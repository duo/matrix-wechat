package internal

import (
	"fmt"
	"net/http"
	"net/url"
	"sync"

	"github.com/duo/matrix-wechat/internal/config"
	"github.com/duo/matrix-wechat/internal/database"
	"github.com/duo/matrix-wechat/internal/types"
	"github.com/duo/matrix-wechat/internal/wechat"

	"maunium.net/go/mautrix"
	"maunium.net/go/mautrix/bridge"
	"maunium.net/go/mautrix/bridge/commands"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/format"
	"maunium.net/go/mautrix/id"
)

type WechatBridge struct {
	bridge.Bridge
	Config        *config.Config
	DB            *database.Database
	Formatter     *Formatter
	WechatService *wechat.WechatService
	ExampleConfig string

	usersByMXID         map[id.UserID]*User
	usersByUsername     map[string]*User
	usersLock           sync.Mutex
	managementRooms     map[id.RoomID]*User
	portalsByMXID       map[id.RoomID]*Portal
	portalsByUID        map[database.PortalKey]*Portal
	portalsLock         sync.Mutex
	puppets             map[types.UID]*Puppet
	puppetsByCustomMXID map[id.UserID]*Puppet
	puppetsLock         sync.Mutex
	checkers            map[id.UserID]chan struct{}
	checkersLock        sync.Mutex
}

func NewWechatBridge(exampleConfig string) *WechatBridge {
	return &WechatBridge{
		ExampleConfig:       exampleConfig,
		usersByMXID:         make(map[id.UserID]*User),
		usersByUsername:     make(map[string]*User),
		managementRooms:     make(map[id.RoomID]*User),
		portalsByMXID:       make(map[id.RoomID]*Portal),
		portalsByUID:        make(map[database.PortalKey]*Portal),
		puppets:             make(map[types.UID]*Puppet),
		puppetsByCustomMXID: make(map[id.UserID]*Puppet),
		checkers:            make(map[id.UserID]chan struct{}),
	}
}

func (br *WechatBridge) GetExampleConfig() string {
	return br.ExampleConfig
}

func (br *WechatBridge) GetConfigPtr() interface{} {
	br.Config = &config.Config{
		BaseConfig: &br.Bridge.Config,
	}
	br.Config.BaseConfig.Bridge = &br.Config.Bridge

	return br.Config
}

func (br *WechatBridge) Init() {
	br.CommandProcessor = commands.NewProcessor(&br.Bridge)
	br.RegisterCommands()

	br.EventProcessor.On(event.EphemeralEventPresence, br.HandlePresence)

	br.DB = database.New(br.Bridge.DB, br.ZLog.With().Str("component", "Database").Logger())

	br.Formatter = NewFormatter(br)
	br.WechatService = wechat.NewWechatService(
		br.Config.Bridge.ListenAddress,
		br.Config.Bridge.ListenSecret,
		*br.ZLog,
	)

	if br.Config.Bridge.HomeserverProxy != "" {
		if proxyUrl, err := url.Parse(br.Config.Bridge.HomeserverProxy); err != nil {
			br.ZLog.Warn().Msgf("Failed to parse bridge.hs_proxy: %v", err)
		} else {
			br.AS.HTTPClient.Transport = &http.Transport{Proxy: http.ProxyURL(proxyUrl)}
		}
	}
}

func (br *WechatBridge) Start() {
	br.WaitWebsocketConnected()
	go br.WechatService.Start()
	go br.StartUsers()
}

func (br *WechatBridge) Stop() {
	br.checkersLock.Lock()
	for _, checker := range br.checkers {
		select {
		case checker <- struct{}{}:
		default:
		}
	}
	br.checkersLock.Unlock()

	br.usersLock.Lock()
	for _, user := range br.usersByUsername {
		if user.Client == nil {
			continue
		}
		br.ZLog.Debug().Msgf("Disconnecting %s", user.MXID)
		user.DeleteConnection()
	}
	br.usersLock.Unlock()

	br.WechatService.Stop()
}

func (br *WechatBridge) StartUsers() {
	br.ZLog.Debug().Msgf("Starting custom puppets")
	for _, loopuppet := range br.GetAllPuppetsWithCustomMXID() {
		go func(puppet *Puppet) {
			puppet.log.Debug().Msgf("Starting custom puppet %s", puppet.CustomMXID)
			err := puppet.StartCustomMXID(true)
			if err != nil {
				puppet.log.Error().Msgf("Failed to start custom puppet: %v", err)
			}
		}(loopuppet)
	}
}

func (br *WechatBridge) CreatePrivatePortal(roomID id.RoomID, brInviter bridge.User, brGhost bridge.Ghost) {
	inviter := brInviter.(*User)
	puppet := brGhost.(*Puppet)
	key := database.NewPortalKey(puppet.UID, inviter.UID)
	portal := br.GetPortalByUID(key)

	if len(portal.MXID) == 0 {
		br.createPrivatePortalFromInvite(roomID, inviter, puppet, portal)
		return
	}

	ok := portal.ensureUserInvited(inviter)
	if !ok {
		br.ZLog.Warn().Msgf("Failed to invite %s to existing private chat portal %s with %s. Redirecting portal to new room...", inviter.MXID, portal.MXID, puppet.UID)
		br.createPrivatePortalFromInvite(roomID, inviter, puppet, portal)
		return
	}
	intent := puppet.DefaultIntent()
	errorMessage := fmt.Sprintf("You already have a private chat portal with me at [%[1]s](https://matrix.to/#/%[1]s)", portal.MXID)
	errorContent := format.RenderMarkdown(errorMessage, true, false)
	_, _ = intent.SendMessageEvent(roomID, event.EventMessage, errorContent)
	br.ZLog.Debug().Msgf("Leaving private chat room %s as %s after accepting invite from %s as we already have chat with the user", roomID, puppet.MXID, inviter.MXID)
	_, _ = intent.LeaveRoom(roomID)
}

func (br *WechatBridge) createPrivatePortalFromInvite(roomID id.RoomID, inviter *User, puppet *Puppet, portal *Portal) {
	// TODO check if room is already encrypted
	var existingEncryption event.EncryptionEventContent
	var encryptionEnabled bool
	err := portal.MainIntent().StateEvent(roomID, event.StateEncryption, "", &existingEncryption)
	if err != nil {
		portal.log.Warn().Msgf("Failed to check if encryption is enabled in private chat room %s", roomID)
	} else {
		encryptionEnabled = existingEncryption.Algorithm == id.AlgorithmMegolmV1
	}

	portal.MXID = roomID
	portal.Topic = PrivateChatTopic
	portal.Name = puppet.Displayname
	portal.AvatarURL = puppet.AvatarURL
	portal.Avatar = puppet.Avatar
	portal.log.Info().Msgf("Created private chat portal in %s after invite from %s", roomID, inviter.MXID)
	intent := puppet.DefaultIntent()

	if br.Config.Bridge.Encryption.Default || encryptionEnabled {
		_, err := intent.InviteUser(roomID, &mautrix.ReqInviteUser{UserID: br.Bot.UserID})
		if err != nil {
			portal.log.Warn().Msgf("Failed to invite bridge bot to enable e2be: %v", err)
		}
		err = br.Bot.EnsureJoined(roomID)
		if err != nil {
			portal.log.Warn().Msgf("Failed to join as bridge bot to enable e2be: %v", err)
		}
		if !encryptionEnabled {
			_, err = intent.SendStateEvent(roomID, event.StateEncryption, "", portal.GetEncryptionEventContent())
			if err != nil {
				portal.log.Warn().Msgf("Failed to enable e2be: %v", err)
			}
		}
		br.AS.StateStore.SetMembership(roomID, inviter.MXID, event.MembershipJoin)
		br.AS.StateStore.SetMembership(roomID, puppet.MXID, event.MembershipJoin)
		br.AS.StateStore.SetMembership(roomID, br.Bot.UserID, event.MembershipJoin)
		portal.Encrypted = true
	}
	_, _ = portal.MainIntent().SetRoomTopic(portal.MXID, portal.Topic)
	if portal.shouldSetDMRoomMetadata() {
		_, err = portal.MainIntent().SetRoomName(portal.MXID, portal.Name)
		portal.NameSet = err == nil
		_, err = portal.MainIntent().SetRoomAvatar(portal.MXID, portal.AvatarURL)
		portal.AvatarSet = err == nil
	}
	portal.Update(nil)
	portal.UpdateBridgeInfo()
	_, _ = intent.SendNotice(roomID, "Private chat portal created")
}

func (br *WechatBridge) HandlePresence(evt *event.Event) {
	// TODO:
}
