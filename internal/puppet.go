package internal

import (
	"fmt"
	"net/http"
	"regexp"
	"sync"
	"time"

	"github.com/duo/matrix-wechat/internal/database"
	"github.com/duo/matrix-wechat/internal/types"

	"maunium.net/go/mautrix/appservice"
	"maunium.net/go/mautrix/bridge"
	"maunium.net/go/mautrix/id"

	"github.com/rs/zerolog"
)

var userIDRegex *regexp.Regexp

var _ bridge.GhostWithProfile = (*Puppet)(nil)

type Puppet struct {
	*database.Puppet

	bridge *WechatBridge
	log    zerolog.Logger

	MXID id.UserID

	customIntent *appservice.IntentAPI
	customUser   *User

	syncLock sync.Mutex
}

func (p *Puppet) GetMXID() id.UserID {
	return p.MXID
}

func (p *Puppet) GetDisplayname() string {
	return p.Displayname
}

func (p *Puppet) GetAvatarURL() id.ContentURI {
	return p.AvatarURL
}

func (p *Puppet) IntentFor(portal *Portal) *appservice.IntentAPI {
	if p.customIntent == nil || portal.Key.UID == p.UID {
		return p.DefaultIntent()
	}
	return p.customIntent
}

func (p *Puppet) CustomIntent() *appservice.IntentAPI {
	return p.customIntent
}

func (p *Puppet) DefaultIntent() *appservice.IntentAPI {
	return p.bridge.AS.Intent(p.MXID)
}

func (p *Puppet) UpdateAvatar(source *User, forceAvatarSync bool, forcePortalSync bool) bool {
	changed := false
	if forceAvatarSync {
		changed = source.updateAvatar(p.UID, &p.Avatar, &p.AvatarURL, &p.AvatarSet, p.log, p.DefaultIntent())
	}
	if !changed || p.Avatar == "unauthorized" {
		if forcePortalSync {
			go p.updatePortalAvatar()
		}

		return changed
	}
	err := p.DefaultIntent().SetAvatarURL(p.AvatarURL)
	if err != nil {
		p.log.Warn().Msgf("Failed to set avatar: %s", err)
	} else {
		p.AvatarSet = true
	}
	go p.updatePortalAvatar()

	return true
}

func (p *Puppet) UpdateName(contact types.ContactInfo, forcePortalSync bool) bool {
	newName, quality := p.bridge.Config.Bridge.FormatDisplayname(contact)
	if (p.Displayname != newName || !p.NameSet) && quality >= p.NameQuality {
		p.Displayname = newName
		p.NameQuality = quality
		p.NameSet = false
		err := p.DefaultIntent().SetDisplayName(newName)
		if err == nil {
			p.log.Debug().Msgf("Updated name %s -> %s", p.Displayname, newName)
			p.NameSet = true
			go p.updatePortalName()
		} else {
			p.log.Warn().Msgf("Failed to set display name: %s", err)
		}
		return true
	} else if forcePortalSync {
		go p.updatePortalName()
	}

	return false
}

func (p *Puppet) updatePortalMeta(meta func(portal *Portal)) {
	if p.bridge.Config.Bridge.PrivateChatPortalMeta {
		for _, portal := range p.bridge.GetAllPortalsByUID(p.UID) {
			// Get room create lock to prevent races between receiving contact info and room creation.
			portal.roomCreateLock.Lock()
			meta(portal)
			portal.roomCreateLock.Unlock()
		}
	}
}

func (p *Puppet) updatePortalAvatar() {
	p.updatePortalMeta(func(portal *Portal) {
		if portal.Avatar == p.Avatar && portal.AvatarURL == p.AvatarURL && portal.AvatarSet {
			return
		}
		portal.AvatarURL = p.AvatarURL
		portal.Avatar = p.Avatar
		portal.AvatarSet = false
		defer portal.Update(nil)
		if len(portal.MXID) > 0 {
			_, err := portal.MainIntent().SetRoomAvatar(portal.MXID, p.AvatarURL)
			if err != nil {
				portal.log.Warn().Msgf("Failed to set avatar: %s", err)
			} else {
				portal.AvatarSet = true
				portal.UpdateBridgeInfo()
			}
		}
	})
}

func (p *Puppet) updatePortalName() {
	p.updatePortalMeta(func(portal *Portal) {
		portal.UpdateName(p.Displayname, types.EmptyUID, true)
	})
}

func (p *Puppet) SyncContact(source *User, forceAvatarSync bool, reason string) {
	info := source.Client.GetUserInfo(p.UID.Uin)
	if info != nil {
		p.Sync(source, types.NewContact(info.ID, info.Name, info.Remark), forceAvatarSync, false)
	} else {
		p.log.Warn().Msgf("No contact info found through %s in SyncContact (sync reason: %s)", source.MXID, reason)
	}
}

func (p *Puppet) Sync(source *User, contact *types.ContactInfo, forceAvatarSync, forcePortalSync bool) {
	p.syncLock.Lock()
	defer p.syncLock.Unlock()

	err := p.DefaultIntent().EnsureRegistered()
	if err != nil {
		p.log.Error().Msgf("Failed to ensure registered: %s", err)
	}

	p.log.Debug().Msgf("Syncing info through %s", source.UID)

	update := false
	if contact != nil {
		update = p.UpdateName(*contact, forcePortalSync) || update
	}
	if len(p.Avatar) == 0 || forceAvatarSync || p.bridge.Config.Bridge.UserAvatarSync {
		update = p.UpdateAvatar(source, forceAvatarSync, forcePortalSync) || update
	}
	if update || p.LastSync.Add(24*time.Hour).Before(time.Now()) {
		p.LastSync = time.Now()
		p.Update()
	}
}

func (br *WechatBridge) ParsePuppetMXID(mxid id.UserID) (types.UID, bool) {
	var uid types.UID
	if userIDRegex == nil {
		userIDRegex = regexp.MustCompile(fmt.Sprintf("^@%s:%s$",
			br.Config.Bridge.FormatUsername("(.+)"),
			br.Config.Homeserver.Domain))
	}
	match := userIDRegex.FindStringSubmatch(string(mxid))
	if len(match) == 2 {
		uid = types.NewUserUID(match[1])
		return uid, true
	}
	return uid, false
}

func (br *WechatBridge) GetPuppetByMXID(mxid id.UserID) *Puppet {
	uid, ok := br.ParsePuppetMXID(mxid)
	if !ok {
		return nil
	}

	return br.GetPuppetByUID(uid)
}

func (br *WechatBridge) GetPuppetByUID(uid types.UID) *Puppet {
	if uid.Type != types.User {
		return nil
	}

	br.puppetsLock.Lock()
	defer br.puppetsLock.Unlock()

	puppet, ok := br.puppets[uid]
	if !ok {
		dbPuppet := br.DB.Puppet.Get(uid)
		if dbPuppet == nil {
			dbPuppet = br.DB.Puppet.New()
			dbPuppet.UID = uid
			dbPuppet.Insert()
		}
		puppet = br.NewPuppet(dbPuppet)
		br.puppets[puppet.UID] = puppet
		if len(puppet.CustomMXID) > 0 {
			br.puppetsByCustomMXID[puppet.CustomMXID] = puppet
		}
	}

	return puppet
}

func (br *WechatBridge) GetPuppetByCustomMXID(mxid id.UserID) *Puppet {
	br.puppetsLock.Lock()
	defer br.puppetsLock.Unlock()

	puppet, ok := br.puppetsByCustomMXID[mxid]
	if !ok {
		dbPuppet := br.DB.Puppet.GetByCustomMXID(mxid)
		if dbPuppet == nil {
			return nil
		}
		puppet = br.NewPuppet(dbPuppet)
		br.puppets[puppet.UID] = puppet
		br.puppetsByCustomMXID[puppet.CustomMXID] = puppet
	}

	return puppet
}

func (user *User) GetIDoublePuppet() bridge.DoublePuppet {
	p := user.bridge.GetPuppetByCustomMXID(user.MXID)
	if p == nil || p.CustomIntent() == nil {
		return nil
	}

	return p
}

func (user *User) GetIGhost() bridge.Ghost {
	if user.UID.IsEmpty() {
		return nil
	}
	p := user.bridge.GetPuppetByUID(user.UID)
	if p == nil {
		return nil
	}

	return p
}

func (br *WechatBridge) IsGhost(id id.UserID) bool {
	_, ok := br.ParsePuppetMXID(id)

	return ok
}

func (br *WechatBridge) GetIGhost(id id.UserID) bridge.Ghost {
	p := br.GetPuppetByMXID(id)
	if p == nil {
		return nil
	}

	return p
}

func (br *WechatBridge) GetAllPuppetsWithCustomMXID() []*Puppet {
	return br.dbPuppetsToPuppets(br.DB.Puppet.GetAllWithCustomMXID())
}

func (br *WechatBridge) GetAllPuppets() []*Puppet {
	return br.dbPuppetsToPuppets(br.DB.Puppet.GetAll())
}

func (br *WechatBridge) dbPuppetsToPuppets(dbPuppets []*database.Puppet) []*Puppet {
	br.puppetsLock.Lock()
	defer br.puppetsLock.Unlock()

	output := make([]*Puppet, len(dbPuppets))
	for index, dbPuppet := range dbPuppets {
		if dbPuppet == nil {
			continue
		}
		puppet, ok := br.puppets[dbPuppet.UID]
		if !ok {
			puppet = br.NewPuppet(dbPuppet)
			br.puppets[dbPuppet.UID] = puppet
			if len(dbPuppet.CustomMXID) > 0 {
				br.puppetsByCustomMXID[dbPuppet.CustomMXID] = puppet
			}
		}
		output[index] = puppet
	}

	return output
}

func (br *WechatBridge) FormatPuppetMXID(uid types.UID) id.UserID {
	return id.NewUserID(
		br.Config.Bridge.FormatUsername(uid.Uin),
		br.Config.Homeserver.Domain)
}

func (br *WechatBridge) NewPuppet(dbPuppet *database.Puppet) *Puppet {
	return &Puppet{
		Puppet: dbPuppet,
		bridge: br,
		log:    br.ZLog.With().Str("puppet", fmt.Sprintf("Puppet/%s", dbPuppet.UID)).Logger(),

		MXID: br.FormatPuppetMXID(dbPuppet.UID),
	}
}

func reuploadAvatar(intent *appservice.IntentAPI, url string) (id.ContentURI, error) {
	data, err := GetBytes(url)
	if err != nil {
		return id.ContentURI{}, fmt.Errorf("failed to download avatar: %w", err)
	}

	mime := http.DetectContentType(data)
	resp, err := intent.UploadBytes(data, mime)
	if err != nil {
		return id.ContentURI{}, fmt.Errorf("failed to upload avatar to Matrix: %w", err)
	}

	return resp.ContentURI, nil
}
