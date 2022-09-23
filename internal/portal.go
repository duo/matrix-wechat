package internal

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/duo/matrix-wechat/internal/database"
	"github.com/duo/matrix-wechat/internal/types"
	"github.com/duo/matrix-wechat/internal/wechat"

	"github.com/gabriel-vasile/mimetype"
	"maunium.net/go/mautrix"
	"maunium.net/go/mautrix/appservice"
	"maunium.net/go/mautrix/bridge"
	"maunium.net/go/mautrix/bridge/bridgeconfig"
	"maunium.net/go/mautrix/crypto/attachment"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/format"
	"maunium.net/go/mautrix/id"
	"maunium.net/go/mautrix/util"
	"maunium.net/go/mautrix/util/dbutil"

	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"

	log "maunium.net/go/maulogger/v2"
)

const (
	PrivateChatTopic      = "Wechat private chat"
	recentlyHandledLength = 100
)

var (
	ErrStatusBroadcastDisabled = errors.New("status bridging is disabled")
	errDifferentUser           = errors.New("user is not the recipient of this private chat portal")
	errUserNotLoggedIn         = errors.New("user is not logged in")
	errMediaDownloadFailed     = errors.New("failed to download media")
	errMediaDecryptFailed      = errors.New("failed to decrypt media")

	PortalCreationDummyEvent = event.Type{Type: "me.lxduo.wechat.dummy.portal_created", Class: event.MessageEventType}
)

type PortalMessage struct {
	event  *wechat.WebsocketMessage
	fake   *fakeMessage
	source *User
}

type PortalMatrixMessage struct {
	evt        *event.Event
	user       *User
	receivedAt time.Time
}

type Portal struct {
	*database.Portal

	bridge *WechatBridge
	log    log.Logger

	roomCreateLock sync.Mutex
	encryptLock    sync.Mutex
	avatarLock     sync.Mutex

	recentlyHandled      [recentlyHandledLength]recentlyHandledWrapper
	recentlyHandledLock  sync.Mutex
	recentlyHandledIndex uint8

	messages       chan PortalMessage
	matrixMessages chan PortalMatrixMessage
}

type ReplyInfo struct {
	MessageID string
	Sender    types.UID
}

type ConvertedMessage struct {
	Intent  *appservice.IntentAPI
	Type    event.Type
	Content *event.MessageEventContent
	Extra   map[string]interface{}
	Caption *event.MessageEventContent

	ReplyTo  *ReplyInfo
	Error    database.MessageErrorType
	MediaKey []byte
}

type fakeMessage struct {
	Sender    types.UID
	Text      string
	ID        string
	Time      time.Time
	Important bool
}

type recentlyHandledWrapper struct {
	id  string
	err database.MessageErrorType
}

func (p *Portal) IsEncrypted() bool {
	return p.Encrypted
}

func (p *Portal) MarkEncrypted() {
	p.Encrypted = true
	p.Update(nil)
}

func (p *Portal) ReceiveMatrixEvent(user bridge.User, evt *event.Event) {
	if user.GetPermissionLevel() >= bridgeconfig.PermissionLevelUser {
		p.matrixMessages <- PortalMatrixMessage{user: user.(*User), evt: evt, receivedAt: time.Now()}
	}
}

func (p *Portal) GetUsers() []*User {
	return nil
}

func (p *Portal) handleWechatMessageLoopItem(msg PortalMessage) {
	defer func() {
		panicErr := recover()
		if panicErr != nil {
			p.log.Warnfln("Panic while process %+v: %v\n%s", msg, panicErr, debug.Stack())
		}
	}()

	if len(p.MXID) == 0 {
		p.log.Debugln("Creating Matrix room from incoming message")
		err := p.CreateMatrixRoom(msg.source, nil, false)
		if err != nil {
			p.log.Errorln("Failed to create portal room:", err)

			return
		}
	}

	switch {
	case msg.event != nil:
		p.handleWechatMessage(msg.source, msg.event)
	case msg.fake != nil:
		msg.fake.ID = "FAKE::" + msg.fake.ID
		p.handleFakeMessage(*msg.fake)
	default:
		p.log.Warnln("Unexpected PortalMessage with no message: %+v", msg)
	}
}

func (p *Portal) handleMatrixMessageLoopItem(msg PortalMatrixMessage) {
	defer func() {
		panicErr := recover()
		if panicErr != nil {
			p.log.Warnfln("Panic while process %+v: %v\n%s", msg, panicErr, debug.Stack())
		}
	}()

	switch msg.evt.Type {
	case event.EventMessage, event.EventSticker:
		p.HandleMatrixMessage(msg.user, msg.evt)
	case event.EventRedaction:
		p.HandleMatrixRedaction(msg.user, msg.evt)
	case event.EventReaction:
		p.HandleMatrixReaction(msg.user, msg.evt)
	default:
		p.log.Warnln("Unsupported event type %+v in portal message channel", msg.evt.Type)
	}
}

func (p *Portal) handleMessageLoop() {
	for {
		select {
		case msg := <-p.messages:
			p.handleWechatMessageLoopItem(msg)
		case msg := <-p.matrixMessages:
			p.handleMatrixMessageLoopItem(msg)
		}
	}
}

func (p *Portal) handleFakeMessage(msg fakeMessage) {
	if p.isRecentlyHandled(msg.ID, database.MsgNoError) {
		p.log.Debugfln("Not handling %s (fake): message was recently handled", msg.ID)
		return
	} else if existingMsg := p.bridge.DB.Message.GetByMsgID(p.Key, msg.ID); existingMsg != nil {
		p.log.Debugfln("Not handling %s (fake): message is duplicate", msg.ID)
		return
	}

	intent := p.bridge.GetPuppetByUID(msg.Sender).IntentFor(p)
	if !intent.IsCustomPuppet && p.IsPrivateChat() && msg.Sender.Uin == p.Key.Receiver.Uin {
		p.log.Debugfln("Not handling %s (fake): user doesn't have double puppeting enabled", msg.ID)
		return
	}

	msgType := event.MsgNotice
	if msg.Important {
		msgType = event.MsgText
	}

	resp, err := p.sendMessage(intent, event.EventMessage, &event.MessageEventContent{
		MsgType: msgType,
		Body:    msg.Text,
	}, nil, msg.Time.UnixMilli())
	if err != nil {
		p.log.Errorfln("Failed to send %s to Matrix: %v", msg.ID, err)
	} else {
		p.finishHandling(nil, msg.ID, msg.Time, msg.Sender, resp.EventID, database.MsgFake, database.MsgNoError)
	}
}

func (p *Portal) handleWechatMessage(source *User, msg *wechat.WebsocketMessage) {
	if len(p.MXID) == 0 {
		p.log.Warnln("handleWechatMessage called even though portal.MXID is empty")
		return
	}

	msgID := fmt.Sprint(msg.ID)
	sender := types.NewUserUID(msg.Sender)
	ts := msg.Timestamp

	existingMsg := p.bridge.DB.Message.GetByMsgID(p.Key, msgID)
	if existingMsg != nil {
		p.log.Debugfln("Not handling %s: message is duplicate", msgID)
		return
	}

	intent := p.getMessageIntent(source, sender)
	if intent == nil {
		return
	} else if !intent.IsCustomPuppet && p.IsPrivateChat() && sender.Uin == p.Key.Receiver.Uin {
		p.log.Debugfln("Not handling %s: user doesn't have double puppeting enabled", msgID)
		return
	}

	var converted = &ConvertedMessage{
		Intent: intent,
		Type:   event.EventMessage,
		Content: &event.MessageEventContent{
			Body:    msg.Content,
			MsgType: event.MsgText,
		},
	}

	switch msg.EventType {
	case wechat.EventText:
		var mentions []string
		err := json.Unmarshal(msg.Extra, &mentions)
		if err == nil && len(mentions) > 0 {
			converted = p.convertWechatMentions(source, msg, intent)
		}
	case wechat.EventImage, wechat.EventVideo, wechat.EventAudio, wechat.EventFile:
		converted = p.convertWechatMedia(source, msg, intent)
	case wechat.EventLocation:
		converted = p.convertWechatLocation(source, msg, intent)
	case wechat.EventNotice:
		p.UpdateTopic(msg.Content, types.EmptyUID, false)
	case wechat.EventApp:
		converted = p.convertWechatApp(source, msg, intent)
	}

	if len(msg.Reply.Sender) > 0 {
		p.SetReply(converted.Content, &ReplyInfo{
			MessageID: strconv.FormatUint(msg.Reply.ID, 10),
			Sender:    types.NewUserUID(msg.Reply.Sender),
		})
	}

	var eventID id.EventID
	resp, err := p.sendMessage(converted.Intent, converted.Type, converted.Content, converted.Extra, ts)
	if err != nil {
		p.log.Errorfln("Failed to send %s to Matrix: %v", msgID, err)
	} else {
		eventID = resp.EventID
	}

	if len(eventID) != 0 {
		p.finishHandling(existingMsg, msgID, time.Unix(ts, 0), sender, eventID, database.MsgNormal, converted.Error)
	}
}

func (p *Portal) convertWechatMentions(source *User, msg *wechat.WebsocketMessage, intent *appservice.IntentAPI) *ConvertedMessage {
	var mentions []string
	json.Unmarshal(msg.Extra, &mentions)

	formattedBody := msg.Content
	var formattedHead string

	// TODO: notify all?
	for _, mention := range mentions {
		mxid, name := p.bridge.Formatter.GetMatrixInfoByUID(p.MXID, types.NewUserUID(mention))
		groupNickname := source.Client.GetGroupMemberNickname(p.Key.UID.Uin, mention)
		original := "@" + groupNickname
		replacement := fmt.Sprintf(`<a href="https://matrix.to/#/%s">%s</a> `, mxid, name)

		if len(groupNickname) > 0 && strings.Contains(msg.Content, original) {
			formattedBody = strings.ReplaceAll(formattedBody, original, replacement)
		} else {
			formattedHead += replacement
		}
	}
	if len(formattedHead) > 0 {
		formattedHead += "<br> "
	}

	content := &event.MessageEventContent{
		MsgType:       event.MsgText,
		Format:        event.FormatHTML,
		Body:          msg.Content,
		FormattedBody: formattedHead + formattedBody,
	}
	converted := &ConvertedMessage{
		Intent:  intent,
		Type:    event.EventMessage,
		Content: content,
	}

	return converted
}

func (p *Portal) convertWechatMedia(source *User, msg *wechat.WebsocketMessage, intent *appservice.IntentAPI) *ConvertedMessage {
	msgID := fmt.Sprint(msg.ID)

	converted := &ConvertedMessage{
		Intent: intent,
	}

	var data wechat.BlobData
	err := json.Unmarshal(msg.Extra, &data)
	if err != nil {
		return p.makeMediaBridgeFailureMessage(msgID, errors.New("failed to decode wechat media"), converted)
	}

	binary := data.Binary
	if msg.EventType == "m.audio" {
		binary, err = convertToOgg(data.Binary)
		if err != nil {
			return p.makeMediaBridgeFailureMessage(msgID, errors.New("failed to convert silk audio to ogg format"), converted)
		}
	}

	mime := mimetype.Detect(binary)

	content := &event.MessageEventContent{
		MsgType: event.MessageType(msg.EventType),
		Info: &event.FileInfo{
			MimeType: mime.String(),
			Size:     len(binary),
		},
		Body: data.Name,
	}
	converted.Type = event.EventMessage
	converted.Content = content

	err = p.uploadMedia(intent, binary, content)
	if err != nil {
		if errors.Is(err, mautrix.MTooLarge) {
			return p.makeMediaBridgeFailureMessage(msgID, errors.New("homeserver rejected too large file"), converted)
		} else if httpErr, ok := err.(mautrix.HTTPError); ok && httpErr.IsStatus(413) {
			return p.makeMediaBridgeFailureMessage(msgID, errors.New("proxy rejected too large file"), converted)
		} else {
			return p.makeMediaBridgeFailureMessage(msgID, fmt.Errorf("failed to upload media: %w", err), converted)
		}
	}

	return converted
}

func (p *Portal) convertWechatLocation(source *User, msg *wechat.WebsocketMessage, intent *appservice.IntentAPI) *ConvertedMessage {
	msgID := fmt.Sprint(msg.ID)

	converted := &ConvertedMessage{
		Intent: intent,
	}

	var data wechat.LocationData
	err := json.Unmarshal(msg.Extra, &data)
	if err != nil {
		return p.makeMediaBridgeFailureMessage(msgID, errors.New("failed to decode wechat location"), converted)
	}

	url := fmt.Sprintf("https://maps.google.com/?q=%.5f,%.5f", data.Latitude, data.Longitude)

	content := &event.MessageEventContent{
		MsgType:       event.MsgLocation,
		Body:          fmt.Sprintf("Location: %s\n%s\n%s", data.Name, data.Address, url),
		Format:        event.FormatHTML,
		FormattedBody: fmt.Sprintf("Location: <a href='%s'>%s</a><br>%s", url, data.Name, data.Address),
		GeoURI:        fmt.Sprintf("geo:%.5f,%.5f", data.Latitude, data.Longitude),
	}

	converted.Type = event.EventMessage
	converted.Content = content

	return converted
}

func (p *Portal) convertWechatApp(source *User, msg *wechat.WebsocketMessage, intent *appservice.IntentAPI) *ConvertedMessage {
	msgID := fmt.Sprint(msg.ID)

	converted := &ConvertedMessage{
		Intent: intent,
	}

	var data wechat.LinkData
	err := json.Unmarshal(msg.Extra, &data)
	if err != nil {
		return p.makeMediaBridgeFailureMessage(msgID, errors.New("failed to decode wechat link"), converted)
	}

	var body string
	if len(data.URL) > 0 {
		body = fmt.Sprintf("[%s](%s)\n%s", data.Title, data.URL, data.Description)
	} else {
		body = fmt.Sprintf("**%s**\n%s", data.Title, data.Description)
	}

	content := &event.MessageEventContent{
		MsgType:       event.MsgText,
		Format:        event.FormatHTML,
		Body:          body,
		FormattedBody: format.RenderMarkdown(body, true, false).FormattedBody,
	}
	converted.Type = event.EventMessage
	converted.Content = content

	return converted
}

func (p *Portal) isRecentlyHandled(id string, error database.MessageErrorType) bool {
	start := p.recentlyHandledIndex
	lookingForMsg := recentlyHandledWrapper{id, error}
	for i := start; i != start; i = (i - 1) % recentlyHandledLength {
		if p.recentlyHandled[i] == lookingForMsg {
			return true
		}
	}

	return false
}

func (p *Portal) markHandled(txn dbutil.Transaction, msg *database.Message, msgID string, ts time.Time, sender types.UID, mxid id.EventID, isSent, recent bool, msgType database.MessageType, errType database.MessageErrorType) *database.Message {
	if msg == nil {
		msg = p.bridge.DB.Message.New()
		msg.Chat = p.Key
		msg.MsgID = msgID
		msg.MXID = mxid
		msg.Timestamp = ts
		msg.Sender = sender
		msg.Sent = isSent
		msg.Type = msgType
		msg.Error = errType
		msg.Insert(txn)
	} else {
		msg.UpdateMXID(txn, mxid, msgType, errType)
	}

	if recent {
		p.recentlyHandledLock.Lock()
		index := p.recentlyHandledIndex
		p.recentlyHandledIndex = (p.recentlyHandledIndex + 1) % recentlyHandledLength
		p.recentlyHandledLock.Unlock()
		p.recentlyHandled[index] = recentlyHandledWrapper{msg.MsgID, errType}
	}

	return msg
}

func (p *Portal) getMessagePuppet(user *User, sender types.UID) *Puppet {
	puppet := p.bridge.GetPuppetByUID(sender)
	if puppet == nil {
		p.log.Warnfln("Message doesn't seem to have a valid sender (%s): puppet is nil", sender)
		return nil
	}

	user.EnqueuePortalResync(p)
	puppet.SyncContact(user, false, "handling message")

	return puppet
}

func (p *Portal) getMessageIntent(user *User, sender types.UID) *appservice.IntentAPI {
	puppet := p.getMessagePuppet(user, sender)
	if puppet == nil {
		return nil
	}

	return puppet.IntentFor(p)
}

func (p *Portal) finishHandling(existing *database.Message, msgId string, ts time.Time, sender types.UID, mxid id.EventID, msgType database.MessageType, errType database.MessageErrorType) {
	p.markHandled(nil, existing, msgId, ts, sender, mxid, true, true, msgType, errType)
	p.log.Debugfln("Handled message %s (%s) -> %s", msgId, msgType, mxid)
}

func (p *Portal) kickExtraUsers(participantMap map[types.UID]bool) {
	members, err := p.MainIntent().JoinedMembers(p.MXID)
	if err != nil {
		p.log.Warnln("Failed to get member list:", err)
		return
	}
	for member := range members.Joined {
		uid, ok := p.bridge.ParsePuppetMXID(member)
		if ok {
			_, shouldBePresent := participantMap[uid]
			if !shouldBePresent {
				_, err = p.MainIntent().KickUser(p.MXID, &mautrix.ReqKickUser{
					UserID: member,
					Reason: "User had left this Wechat chat",
				})
				if err != nil {
					p.log.Warnfln("Failed to kick user %s who had left: %v", member, err)
				}
			}
		}
	}
}

func (p *Portal) SyncParticipants(source *User, metadata *wechat.GroupInfo, forceAvatarSync bool) {
	changed := false
	levels, err := p.MainIntent().PowerLevels(p.MXID)
	if err != nil {
		levels = p.GetBasePowerLevels()
		changed = true
	}

	if len(metadata.Members) == 0 {
		m := source.Client.GetGroupMembers(metadata.ID)
		if m == nil {
			p.log.Warnfln("Failed to get group members through %s", source.UID)
		} else {
			metadata.Members = m
		}
	}

	changed = p.applyPowerLevelFixes(levels) || changed
	participantMap := make(map[types.UID]bool)
	for _, wxid := range metadata.Members {
		uid := types.NewUserUID(wxid)
		participantMap[uid] = true
		puppet := p.bridge.GetPuppetByUID(uid)
		puppet.SyncContact(source, forceAvatarSync, "group participant")
		user := p.bridge.GetUserByUID(uid)
		if user != nil && user != source {
			p.ensureUserInvited(user)
		}
		if user == nil || !puppet.IntentFor(p).IsCustomPuppet {
			err = puppet.IntentFor(p).EnsureJoined(p.MXID)
			if err != nil {
				p.log.Warnfln("Failed to make puppet of %s join %s: %v", uid, p.MXID, err)
			}
		}

		expectedLevel := 0
		// TODO: permission
		changed = levels.EnsureUserLevel(puppet.MXID, expectedLevel) || changed
		if user != nil {
			changed = levels.EnsureUserLevel(user.MXID, expectedLevel) || changed
		}
	}

	if changed {
		_, err = p.MainIntent().SetPowerLevels(p.MXID, levels)
		if err != nil {
			p.log.Errorln("Failed to change power levels:", err)
		}
	}

	p.kickExtraUsers(participantMap)
}

func (p *Portal) UpdateAvatar(user *User, setBy types.UID, updateInfo bool) bool {
	p.avatarLock.Lock()
	defer p.avatarLock.Unlock()

	changed := user.updateAvatar(p.Key.UID, &p.Avatar, &p.AvatarURL, &p.AvatarSet, p.log, p.MainIntent())
	if !changed || p.Avatar == "unauthorized" {
		if changed || updateInfo {
			p.Update(nil)
		}
		return changed
	}

	if len(p.MXID) > 0 {
		intent := p.MainIntent()
		if !setBy.IsEmpty() {
			intent = p.bridge.GetPuppetByUID(setBy).IntentFor(p)
		}
		_, err := intent.SetRoomAvatar(p.MXID, p.AvatarURL)
		if errors.Is(err, mautrix.MForbidden) && intent != p.MainIntent() {
			_, err = p.MainIntent().SetRoomAvatar(p.MXID, p.AvatarURL)
		}
		if err != nil {
			p.log.Warnln("Failed to set room avatar:", err)
			return true
		} else {
			p.AvatarSet = true
		}
	}

	if updateInfo {
		p.UpdateBridgeInfo()
		p.Update(nil)
	}

	return true
}

func (p *Portal) UpdateName(name string, setBy types.UID, updateInfo bool) bool {
	if p.Name != name || (!p.NameSet && len(p.MXID) > 0) {
		p.log.Debugfln("Updating name %q -> %q", p.Name, name)
		p.Name = name
		p.NameSet = false
		if updateInfo {
			defer p.Update(nil)
		}

		if len(p.MXID) > 0 {
			intent := p.MainIntent()
			if !setBy.IsEmpty() {
				intent = p.bridge.GetPuppetByUID(setBy).IntentFor(p)
			}
			_, err := intent.SetRoomName(p.MXID, name)
			if errors.Is(err, mautrix.MForbidden) && intent != p.MainIntent() {
				_, err = p.MainIntent().SetRoomName(p.MXID, name)
			}
			if err == nil {
				p.NameSet = true
				if updateInfo {
					p.UpdateBridgeInfo()
				}

				return true
			} else {
				p.log.Warnln("Failed to set room name:", err)
			}
		}
	}

	return false
}

func (p *Portal) UpdateTopic(topic string, setBy types.UID, updateInfo bool) bool {
	if p.Topic != topic || !p.TopicSet {
		p.log.Debugfln("Updating topic %q -> %q", p.Topic, topic)
		p.Topic = topic
		p.TopicSet = false

		intent := p.MainIntent()
		if !setBy.IsEmpty() {
			intent = p.bridge.GetPuppetByUID(setBy).IntentFor(p)
		}
		_, err := intent.SetRoomTopic(p.MXID, topic)
		if errors.Is(err, mautrix.MForbidden) && intent != p.MainIntent() {
			_, err = p.MainIntent().SetRoomTopic(p.MXID, topic)
		}
		if err == nil {
			p.TopicSet = true
			if updateInfo {
				p.UpdateBridgeInfo()
				p.Update(nil)
			}

			return true
		} else {
			p.log.Warnln("Failed to set room topic:", err)
		}
	}

	return false
}

func (p *Portal) UpdateMetadata(user *User, groupInfo *wechat.GroupInfo, forceAvatarSync bool) bool {
	if p.IsPrivateChat() {
		return false
	}

	if groupInfo == nil {
		p.log.Errorln("Failed to get group info")
		return false
	}

	p.SyncParticipants(user, groupInfo, forceAvatarSync)
	update := false
	update = p.UpdateName(groupInfo.Name, types.EmptyUID, false) || update

	if info := user.Client.GetGroupInfo(groupInfo.ID); info != nil {
		update = p.UpdateTopic(info.Notice, types.EmptyUID, false) || update
	}

	// TODO: restrict message sending and changes

	return update
}

func (p *Portal) ensureUserInvited(user *User) bool {
	return user.ensureInvited(p.MainIntent(), p.MXID, p.IsPrivateChat())
}

func (p *Portal) UpdateMatrixRoom(user *User, groupInfo *wechat.GroupInfo, forceAvatarSync bool) bool {
	if len(p.MXID) == 0 {
		return false
	}
	p.log.Infoln("Syncing portal for", user.MXID)

	p.ensureUserInvited(user)
	go p.addToSpace(user)

	update := false
	update = p.UpdateMetadata(user, groupInfo, forceAvatarSync) || update
	if !p.IsPrivateChat() {
		update = p.UpdateAvatar(user, types.EmptyUID, false) || update
	}
	if update || p.LastSync.Add(24*time.Hour).Before(time.Now()) {
		p.LastSync = time.Now()
		p.Update(nil)
		p.UpdateBridgeInfo()
	}

	return true
}

func (p *Portal) GetBasePowerLevels() *event.PowerLevelsEventContent {
	anyone := 0
	nope := 99
	invite := 50
	if p.bridge.Config.Bridge.AllowUserInvite {
		invite = 0
	}
	return &event.PowerLevelsEventContent{
		UsersDefault:    anyone,
		EventsDefault:   anyone,
		RedactPtr:       &anyone,
		StateDefaultPtr: &nope,
		BanPtr:          &nope,
		InvitePtr:       &invite,
		Users: map[id.UserID]int{
			p.MainIntent().UserID: 100,
		},
		Events: map[string]int{
			event.StateRoomName.Type:   anyone,
			event.StateRoomAvatar.Type: anyone,
			event.StateTopic.Type:      anyone,
			event.EventReaction.Type:   anyone,
			event.EventRedaction.Type:  anyone,
		},
	}
}

func (p *Portal) applyPowerLevelFixes(levels *event.PowerLevelsEventContent) bool {
	changed := false
	changed = levels.EnsureEventLevel(event.EventReaction, 0) || changed
	changed = levels.EnsureEventLevel(event.EventRedaction, 0) || changed

	return changed
}

func (p *Portal) ChangeAdminStatus(uids []types.UID, setAdmin bool) id.EventID {
	levels, err := p.MainIntent().PowerLevels(p.MXID)
	if err != nil {
		levels = p.GetBasePowerLevels()
	}
	newLevel := 0
	if setAdmin {
		newLevel = 50
	}

	changed := p.applyPowerLevelFixes(levels)
	for _, uid := range uids {
		puppet := p.bridge.GetPuppetByUID(uid)
		changed = levels.EnsureUserLevel(puppet.MXID, newLevel) || changed

		user := p.bridge.GetUserByUID(uid)
		if user != nil {
			changed = levels.EnsureUserLevel(user.MXID, newLevel) || changed
		}
	}

	if changed {
		resp, err := p.MainIntent().SetPowerLevels(p.MXID, levels)
		if err != nil {
			p.log.Errorln("Failed to change power levels:", err)
		} else {
			return resp.EventID
		}
	}

	return ""
}

func (p *Portal) RestrictMessageSending(restrict bool) id.EventID {
	levels, err := p.MainIntent().PowerLevels(p.MXID)
	if err != nil {
		levels = p.GetBasePowerLevels()
	}

	newLevel := 0
	if restrict {
		newLevel = 50
	}

	changed := p.applyPowerLevelFixes(levels)
	if levels.EventsDefault == newLevel && !changed {
		return ""
	}

	levels.EventsDefault = newLevel
	resp, err := p.MainIntent().SetPowerLevels(p.MXID, levels)
	if err != nil {
		p.log.Errorln("Failed to change power levels:", err)
		return ""
	} else {
		return resp.EventID
	}
}

func (p *Portal) RestrictMetadataChanges(restrict bool) id.EventID {
	levels, err := p.MainIntent().PowerLevels(p.MXID)
	if err != nil {
		levels = p.GetBasePowerLevels()
	}
	newLevel := 0
	if restrict {
		newLevel = 50
	}

	changed := p.applyPowerLevelFixes(levels)
	changed = levels.EnsureEventLevel(event.StateRoomName, newLevel) || changed
	changed = levels.EnsureEventLevel(event.StateRoomAvatar, newLevel) || changed
	changed = levels.EnsureEventLevel(event.StateTopic, newLevel) || changed
	if changed {
		resp, err := p.MainIntent().SetPowerLevels(p.MXID, levels)
		if err != nil {
			p.log.Errorln("Failed to change power levels:", err)
		} else {
			return resp.EventID
		}
	}

	return ""
}

func (p *Portal) getBridgeInfoStateKey() string {
	return fmt.Sprintf("me.lxduo.wechat://wechat/%s", p.Key.UID)
}

func (p *Portal) getBridgeInfo() (string, event.BridgeEventContent) {
	bridgeInfo := event.BridgeEventContent{
		BridgeBot: p.bridge.Bot.UserID,
		Creator:   p.MainIntent().UserID,
		Protocol: event.BridgeInfoSection{
			ID:          "wechat",
			DisplayName: "Wechat",
			AvatarURL:   p.bridge.Config.AppService.Bot.ParsedAvatar.CUString(),
			ExternalURL: "https://www.wechat.com/",
		},
		Channel: event.BridgeInfoSection{
			ID:          p.Key.UID.String(),
			DisplayName: p.Name,
			AvatarURL:   p.AvatarURL.CUString(),
		},
	}

	return p.getBridgeInfoStateKey(), bridgeInfo
}

func (p *Portal) UpdateBridgeInfo() {
	if len(p.MXID) == 0 {
		p.log.Debugln("Not updating bridge info: no Matrix room created")
		return
	}
	p.log.Debugln("Updating bridge info...")
	stateKey, content := p.getBridgeInfo()
	_, err := p.MainIntent().SendStateEvent(p.MXID, event.StateBridge, stateKey, content)
	if err != nil {
		p.log.Warnln("Failed to update m.bridge:", err)
	}
	// TODO remove this once https://github.com/matrix-org/matrix-doc/pull/2346 is in spec
	_, err = p.MainIntent().SendStateEvent(p.MXID, event.StateHalfShotBridge, stateKey, content)
	if err != nil {
		p.log.Warnln("Failed to update uk.half-shot.bridge:", err)
	}
}

func (p *Portal) GetEncryptionEventContent() (evt *event.EncryptionEventContent) {
	evt = &event.EncryptionEventContent{Algorithm: id.AlgorithmMegolmV1}
	if rot := p.bridge.Config.Bridge.Encryption.Rotation; rot.EnableCustom {
		evt.RotationPeriodMillis = rot.Milliseconds
		evt.RotationPeriodMessages = rot.Messages
	}
	return
}

func (p *Portal) CreateMatrixRoom(user *User, groupInfo *wechat.GroupInfo, isFullInfo bool) error {
	if len(p.MXID) > 0 {
		return nil
	}

	p.roomCreateLock.Lock()
	defer p.roomCreateLock.Unlock()

	intent := p.MainIntent()
	if err := intent.EnsureRegistered(); err != nil {
		return err
	}

	p.log.Infoln("Creating Matrix room. Info source:", user.MXID)

	if p.IsPrivateChat() {
		puppet := p.bridge.GetPuppetByUID(p.Key.UID)
		puppet.SyncContact(user, true, "creating private chat portal")
		if p.bridge.Config.Bridge.PrivateChatPortalMeta {
			p.Name = puppet.Displayname
			p.AvatarURL = puppet.AvatarURL
			p.Avatar = puppet.Avatar
		} else {
			p.Name = ""
		}
		p.Topic = PrivateChatTopic
	} else {
		if groupInfo == nil || !isFullInfo {
			foundInfo := user.Client.GetGroupInfo(p.Key.UID.Uin)
			if foundInfo == nil {
				p.log.Warnfln("Failed to get group info through %s", user.UID)
			} else {
				m := user.Client.GetGroupMembers(p.Key.UID.Uin)
				if m == nil {
					p.log.Warnfln("Failed to get group members through %s: %v", user.UID)
				} else {
					foundInfo.Members = m
					groupInfo = foundInfo
					isFullInfo = true
				}
			}
		}
		if groupInfo != nil {
			p.Name = groupInfo.Name
			//p.Topic = groupInfo.Topic
		}
		p.UpdateAvatar(user, types.EmptyUID, false)
	}

	bridgeInfoStateKey, bridgeInfo := p.getBridgeInfo()

	initialState := []*event.Event{{
		Type: event.StatePowerLevels,
		Content: event.Content{
			Parsed: p.GetBasePowerLevels(),
		},
	}, {
		Type:     event.StateBridge,
		Content:  event.Content{Parsed: bridgeInfo},
		StateKey: &bridgeInfoStateKey,
	}, {
		// TODO remove this once https://github.com/matrix-org/matrix-doc/pull/2346 is in spec
		Type:     event.StateHalfShotBridge,
		Content:  event.Content{Parsed: bridgeInfo},
		StateKey: &bridgeInfoStateKey,
	}}
	if !p.AvatarURL.IsEmpty() {
		initialState = append(initialState, &event.Event{
			Type: event.StateRoomAvatar,
			Content: event.Content{
				Parsed: event.RoomAvatarEventContent{URL: p.AvatarURL},
			},
		})
		p.AvatarSet = true
	}

	var invite []id.UserID

	if p.bridge.Config.Bridge.Encryption.Default {
		initialState = append(initialState, &event.Event{
			Type: event.StateEncryption,
			Content: event.Content{
				Parsed: p.GetEncryptionEventContent(),
			},
		})
		p.Encrypted = true
		if p.IsPrivateChat() {
			invite = append(invite, p.bridge.Bot.UserID)
		}
	}

	creationContent := make(map[string]interface{})
	if !p.bridge.Config.Bridge.FederateRooms {
		creationContent["m.federate"] = false
	}
	resp, err := intent.CreateRoom(&mautrix.ReqCreateRoom{
		Visibility:      "private",
		Name:            p.Name,
		Topic:           p.Topic,
		Invite:          invite,
		Preset:          "private_chat",
		IsDirect:        p.IsPrivateChat(),
		InitialState:    initialState,
		CreationContent: creationContent,
	})
	if err != nil {
		return err
	}
	p.NameSet = len(p.Name) > 0
	p.TopicSet = len(p.Topic) > 0
	p.MXID = resp.RoomID
	p.bridge.portalsLock.Lock()
	p.bridge.portalsByMXID[p.MXID] = p
	p.bridge.portalsLock.Unlock()
	p.Update(nil)
	p.log.Infoln("Matrix room created:", p.MXID)

	for _, userID := range invite {
		p.bridge.StateStore.SetMembership(p.MXID, userID, event.MembershipInvite)
	}

	p.ensureUserInvited(user)
	// TODO: sync chat double puppet detail

	go p.addToSpace(user)

	if groupInfo != nil {
		p.SyncParticipants(user, groupInfo, true)
		// TODO: restrict message sending and changes
	}
	if p.IsPrivateChat() {
		puppet := user.bridge.GetPuppetByUID(p.Key.UID)

		if p.bridge.Config.Bridge.Encryption.Default {
			err = p.bridge.Bot.EnsureJoined(p.MXID)
			if err != nil {
				p.log.Errorln("Failed to join created portal with bridge bot for e2be:", err)
			}
		}

		user.UpdateDirectChats(map[id.UserID][]id.RoomID{puppet.MXID: {p.MXID}})
	}

	firstEventResp, err := p.MainIntent().SendMessageEvent(p.MXID, PortalCreationDummyEvent, struct{}{})
	if err != nil {
		p.log.Errorln("Failed to send dummy event to mark portal creation:", err)
	} else {
		p.FirstEventID = firstEventResp.EventID
		p.Update(nil)
	}

	return nil
}

func (p *Portal) addToSpace(user *User) {
	spaceID := user.GetSpaceRoom()
	if len(spaceID) == 0 || user.IsInSpace(p.Key) {
		return
	}
	_, err := p.bridge.Bot.SendStateEvent(spaceID, event.StateSpaceChild, p.MXID.String(), &event.SpaceChildEventContent{
		Via: []string{p.bridge.Config.Homeserver.Domain},
	})
	if err != nil {
		p.log.Errorfln("Failed to add room to %s's personal filtering space (%s): %v", user.MXID, spaceID, err)
	} else {
		p.log.Debugfln("Added room to %s's personal filtering space (%s)", user.MXID, spaceID)
		user.MarkInSpace(p.Key)
	}
}

func (p *Portal) IsPrivateChat() bool {
	return p.Key.UID.Type == types.User
}

func (p *Portal) IsGroupChat() bool {
	return p.Key.UID.Type == types.Group
}

func (p *Portal) MainIntent() *appservice.IntentAPI {
	if p.IsPrivateChat() {
		return p.bridge.GetPuppetByUID(p.Key.UID).DefaultIntent()
	}

	return p.bridge.Bot
}

func (p *Portal) SetReply(content *event.MessageEventContent, replyTo *ReplyInfo) bool {
	if replyTo == nil {
		return false
	}
	message := p.bridge.DB.Message.GetByMsgID(p.Key, replyTo.MessageID)
	if message == nil || message.IsFakeMXID() {
		return false
	}
	evt, err := p.MainIntent().GetEvent(p.MXID, message.MXID)
	if err != nil {
		p.log.Warnln("Failed to get reply target:", err)
		content.RelatesTo = (&event.RelatesTo{}).SetReplyTo(message.MXID)
		return true
	}
	_ = evt.Content.ParseRaw(evt.Type)
	if evt.Type == event.EventEncrypted {
		decryptedEvt, err := p.bridge.Crypto.Decrypt(evt)
		if err != nil {
			p.log.Warnln("Failed to decrypt reply target:", err)
		} else {
			evt = decryptedEvt
		}
	}
	content.SetReply(evt)

	return true
}

func (p *Portal) encrypt(intent *appservice.IntentAPI, content *event.Content, eventType event.Type) (event.Type, error) {
	if !p.Encrypted || p.bridge.Crypto == nil {
		return eventType, nil
	}
	intent.AddDoublePuppetValue(content)

	// TODO maybe the locking should be inside mautrix-go?
	p.encryptLock.Lock()
	defer p.encryptLock.Unlock()

	err := p.bridge.Crypto.Encrypt(p.MXID, eventType, content)
	if err != nil {
		return eventType, fmt.Errorf("failed to encrypt event: %w", err)
	}

	return event.EventEncrypted, nil
}

func (p *Portal) sendMessage(intent *appservice.IntentAPI, eventType event.Type, content *event.MessageEventContent, extraContent map[string]interface{}, timestamp int64) (*mautrix.RespSendEvent, error) {
	wrappedContent := event.Content{Parsed: content, Raw: extraContent}
	var err error
	eventType, err = p.encrypt(intent, &wrappedContent, eventType)
	if err != nil {
		return nil, err
	}

	_, _ = intent.UserTyping(p.MXID, false, 0)
	if timestamp == 0 {
		return intent.SendMessageEvent(p.MXID, eventType, &wrappedContent)
	} else {
		return intent.SendMassagedMessageEvent(p.MXID, eventType, &wrappedContent, timestamp)
	}
}

func (p *Portal) makeMediaBridgeFailureMessage(msgID string, bridgeErr error, converted *ConvertedMessage) *ConvertedMessage {
	p.log.Errorfln("Failed to bridge media for %s: %v", msgID, bridgeErr)
	converted.Type = event.EventMessage
	converted.Content = &event.MessageEventContent{
		MsgType: event.MsgNotice,
		Body:    fmt.Sprintf("Failed to bridge media: %v", bridgeErr),
	}

	return converted
}

func (p *Portal) encryptFileInPlace(data []byte, mimeType string) (string, *event.EncryptedFileInfo) {
	if !p.Encrypted {
		return mimeType, nil
	}

	file := &event.EncryptedFileInfo{
		EncryptedFile: *attachment.NewEncryptedFile(),
		URL:           "",
	}
	file.EncryptInPlace(data)
	return "application/octet-stream", file
}

func (p *Portal) uploadMedia(intent *appservice.IntentAPI, data []byte, content *event.MessageEventContent) error {
	uploadMimeType, file := p.encryptFileInPlace(data, content.Info.MimeType)

	req := mautrix.ReqUploadMedia{
		ContentBytes: data,
		ContentType:  uploadMimeType,
	}
	var mxc id.ContentURI
	if p.bridge.Config.Homeserver.AsyncMedia {
		uploaded, err := intent.UnstableUploadAsync(req)
		if err != nil {
			return err
		}
		mxc = uploaded.ContentURI
	} else {
		uploaded, err := intent.UploadMedia(req)
		if err != nil {
			return err
		}
		mxc = uploaded.ContentURI
	}

	if file != nil {
		file.URL = mxc.CUString()
		content.File = file
	} else {
		content.URL = mxc.CUString()
	}

	content.Info.Size = len(data)
	if content.Info.Width == 0 && content.Info.Height == 0 && strings.HasPrefix(content.Info.MimeType, "image/") {
		cfg, _, _ := image.DecodeConfig(bytes.NewReader(data))
		content.Info.Width, content.Info.Height = cfg.Width, cfg.Height
	}

	return nil
}

func (p *Portal) preprocessMatrixMedia(content *event.MessageEventContent) (string, []byte, error) {
	fileName := content.Body
	if content.FileName != "" && content.Body != content.FileName {
		fileName = content.FileName
	}

	var file *event.EncryptedFileInfo
	rawMXC := content.URL
	if content.File != nil {
		file = content.File
		rawMXC = file.URL
	}
	mxc, err := rawMXC.Parse()
	if err != nil {
		return fileName, nil, err
	}
	data, err := p.MainIntent().DownloadBytesContext(context.Background(), mxc)
	if err != nil {
		return fileName, nil, util.NewDualError(errMediaDownloadFailed, err)
	}
	if file != nil {
		err = file.DecryptInPlace(data)
		if err != nil {
			return fileName, nil, util.NewDualError(errMediaDecryptFailed, err)
		}
	}

	return fileName, data, nil
}

func (p *Portal) HandleMatrixMessage(sender *User, evt *event.Event) {
	if err := p.canBridgeFrom(sender); err != nil {
		return
	}

	content, ok := evt.Content.Parsed.(*event.MessageEventContent)
	if !ok {
		p.log.Warnfln("Failed to parse matrix message content")
		return
	}

	target := p.Key.UID.Uin

	replyToID := content.GetReplyTo()
	// TODO: how to reply
	var replyMention string
	if len(replyToID) > 0 {
		replyToMsg := p.bridge.DB.Message.GetByMXID(replyToID)
		if replyToMsg != nil && !replyToMsg.IsFakeMsgID() && replyToMsg.Type == database.MsgNormal {
			replyMention = replyToMsg.Sender.Uin
		}
	}

	if evt.Type == event.EventSticker {
		content.MsgType = event.MsgImage
	}

	msg := &wechat.MatrixMessage{
		Target: target,
	}
	switch content.MsgType {
	case event.MsgText, event.MsgEmote:
		var mentions []string
		msg.MessageType = string(event.MsgText)
		text := content.Body

		if content.Format == event.FormatHTML {
			formatted, mentionedUIDs := p.bridge.Formatter.ParseMatrix(content.FormattedBody)
			for _, mention := range mentionedUIDs {
				groupNickname := sender.Client.GetGroupMemberNickname(p.Key.UID.Uin, mention)
				if len(groupNickname) > 0 {
					formatted = strings.ReplaceAll(formatted, "@"+mention, "@"+groupNickname)
				} else {
					puppet := p.bridge.GetPuppetByUID(types.NewUserUID(mention))
					if puppet != nil {
						formatted = strings.ReplaceAll(formatted, "@"+mention, "@"+puppet.Displayname)
					}
				}
			}
			mentions = append(mentions, mentionedUIDs...)
			text = formatted
		}

		if len(replyMention) > 0 {
			if info := sender.Client.GetUserInfo(replyMention); info != nil {
				mentions = append([]string{replyMention}, mentions...)
				text = fmt.Sprintf("@%s %s", info.Nickname, text)
			}
		}

		if content.MsgType == event.MsgEmote {
			text = "/me " + text
		}

		msg.Content = text
		if len(mentions) > 0 {
			msg.Data = mentions
		}
	case event.MsgImage, event.MsgVideo, event.MsgFile:
		name, data, err := p.preprocessMatrixMedia(content)
		if data == nil {
			p.log.Warnfln("Failed to process matrix media: %v", err)
			return
		}
		msg.MessageType = string(content.MsgType)
		msg.Data = &wechat.BlobData{
			Name:   name,
			Binary: data,
		}
	default:
		p.log.Warnfln("%s not support", content.MsgType)
		return
	}

	msgID := "FAKE::" + strconv.FormatInt(evt.Timestamp, 10)
	p.log.Debugln("Sending event", evt.ID, "to Wechat")
	if err := sender.Client.SendMessage(msg); err != nil {
		p.log.Warnfln("Sending event", evt.ID, "to Wechat failed")
	} else {
		// TODO: get msgID from Wechat
		p.finishHandling(nil, msgID, time.Unix(evt.Timestamp, 0), sender.UID, evt.ID, database.MsgNormal, database.MsgNoError)
	}
}

func (p *Portal) HandleMatrixRedaction(sender *User, evt *event.Event) {
	// TODO:
}

func (p *Portal) HandleMatrixReaction(sender *User, evt *event.Event) {
	// TODO:
}

func (p *Portal) canBridgeFrom(sender *User) error {
	if !sender.IsLoggedIn() {
		return errUserNotLoggedIn
	} else if p.IsPrivateChat() && sender.UID.Uin != p.Key.Receiver.Uin {
		return errDifferentUser
	}

	return nil
}

func (p *Portal) Delete() {
	p.Portal.Delete()
	p.bridge.portalsLock.Lock()
	delete(p.bridge.portalsByUID, p.Key)
	if len(p.MXID) > 0 {
		delete(p.bridge.portalsByMXID, p.MXID)
	}
	p.bridge.portalsLock.Unlock()
}

func (p *Portal) GetMatrixUsers() ([]id.UserID, error) {
	members, err := p.MainIntent().JoinedMembers(p.MXID)
	if err != nil {
		return nil, fmt.Errorf("failed to get member list: %w", err)
	}
	var users []id.UserID
	for userID := range members.Joined {
		_, isPuppet := p.bridge.ParsePuppetMXID(userID)
		if !isPuppet && userID != p.bridge.Bot.UserID {
			users = append(users, userID)
		}
	}

	return users, nil
}

func (p *Portal) CleanupIfEmpty() {
	users, err := p.GetMatrixUsers()
	if err != nil {
		p.log.Errorfln("Failed to get Matrix user list to determine if portal needs to be cleaned up: %v", err)
		return
	}

	if len(users) == 0 {
		p.log.Infoln("Room seems to be empty, cleaning up...")
		p.Delete()
		p.Cleanup(false)
	}
}

func (p *Portal) Cleanup(puppetsOnly bool) {
	if len(p.MXID) == 0 {
		return
	}
	intent := p.MainIntent()
	members, err := intent.JoinedMembers(p.MXID)
	if err != nil {
		p.log.Errorln("Failed to get portal members for cleanup:", err)
		return
	}
	for member := range members.Joined {
		if member == intent.UserID {
			continue
		}
		puppet := p.bridge.GetPuppetByMXID(member)
		if puppet != nil {
			_, err = puppet.DefaultIntent().LeaveRoom(p.MXID)
			if err != nil {
				p.log.Errorln("Error leaving as puppet while cleaning up portal:", err)
			}
		} else if !puppetsOnly {
			_, err = intent.KickUser(p.MXID, &mautrix.ReqKickUser{UserID: member, Reason: "Deleting portal"})
			if err != nil {
				p.log.Errorln("Error kicking user while cleaning up portal:", err)
			}
		}
	}
	_, err = intent.LeaveRoom(p.MXID)
	if err != nil {
		p.log.Errorln("Error leaving with main intent while cleaning up portal:", err)
	}
}

func (p *Portal) HandleMatrixLeave(brSender bridge.User) {
	// TODO:
}

func (p *Portal) HandleMatrixKick(brSender bridge.User, brTarget bridge.Ghost) {
	// TODO:
}

func (p *Portal) HandleMatrixInvite(brSender bridge.User, brTarget bridge.Ghost) {
	// TODO:
}

func (p *Portal) HandleMatrixMeta(brSender bridge.User, evt *event.Event) {
	// TODO:
}

func (br *WechatBridge) GetPortalByMXID(mxid id.RoomID) *Portal {
	br.portalsLock.Lock()
	defer br.portalsLock.Unlock()

	portal, ok := br.portalsByMXID[mxid]
	if !ok {
		return br.loadDBPortal(br.DB.Portal.GetByMXID(mxid), nil)
	}

	return portal
}

func (br *WechatBridge) GetIPortal(mxid id.RoomID) bridge.Portal {
	p := br.GetPortalByMXID(mxid)
	if p == nil {
		return nil
	}

	return p
}

func (br *WechatBridge) GetPortalByUID(key database.PortalKey) *Portal {
	br.portalsLock.Lock()
	defer br.portalsLock.Unlock()

	portal, ok := br.portalsByUID[key]
	if !ok {
		return br.loadDBPortal(br.DB.Portal.GetByUID(key), &key)
	}

	return portal
}

func (br *WechatBridge) GetAllPortals() []*Portal {
	return br.dbPortalsToPortals(br.DB.Portal.GetAll())
}

func (br *WechatBridge) GetAllIPortals() (iportals []bridge.Portal) {
	portals := br.GetAllPortals()
	iportals = make([]bridge.Portal, len(portals))
	for i, portal := range portals {
		iportals[i] = portal
	}

	return iportals
}

func (br *WechatBridge) GetAllPortalsByUID(uid types.UID) []*Portal {
	return br.dbPortalsToPortals(br.DB.Portal.GetAllByUID(uid))
}

func (br *WechatBridge) dbPortalsToPortals(dbPortals []*database.Portal) []*Portal {
	br.portalsLock.Lock()
	defer br.portalsLock.Unlock()

	output := make([]*Portal, len(dbPortals))
	for index, dbPortal := range dbPortals {
		if dbPortal == nil {
			continue
		}
		portal, ok := br.portalsByUID[dbPortal.Key]
		if !ok {
			portal = br.loadDBPortal(dbPortal, nil)
		}
		output[index] = portal
	}

	return output
}

func (br *WechatBridge) loadDBPortal(dbPortal *database.Portal, key *database.PortalKey) *Portal {
	if dbPortal == nil {
		if key == nil {
			return nil
		}
		dbPortal = br.DB.Portal.New()
		dbPortal.Key = *key
		dbPortal.Insert()
	}
	portal := br.NewPortal(dbPortal)
	br.portalsByUID[portal.Key] = portal
	if len(portal.MXID) > 0 {
		br.portalsByMXID[portal.MXID] = portal
	}

	return portal
}

func (br *WechatBridge) newBlankPortal(key database.PortalKey) *Portal {
	portal := &Portal{
		bridge: br,
		log:    br.Log.Sub(fmt.Sprintf("Portal/%s", key)),

		messages:       make(chan PortalMessage, br.Config.Bridge.PortalMessageBuffer),
		matrixMessages: make(chan PortalMatrixMessage, br.Config.Bridge.PortalMessageBuffer),
	}

	go portal.handleMessageLoop()

	return portal
}

func (br *WechatBridge) NewManualPortal(key database.PortalKey) *Portal {
	portal := br.newBlankPortal(key)
	portal.Portal = br.DB.Portal.New()
	portal.Key = key

	return portal
}

func (br *WechatBridge) NewPortal(dbPortal *database.Portal) *Portal {
	portal := br.newBlankPortal(dbPortal.Key)
	portal.Portal = dbPortal

	return portal
}
