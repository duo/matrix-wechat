package internal

import (
	"maunium.net/go/mautrix/id"
)

func (p *Puppet) SwitchCustomMXID(accessToken string, mxid id.UserID) error {
	p.CustomMXID = mxid
	p.AccessToken = accessToken
	p.EnablePresence = p.bridge.Config.Bridge.DefaultBridgePresence
	p.Update()

	err := p.StartCustomMXID(false)
	if err != nil {
		return err
	}
	// TODO leave rooms with default puppet

	return nil
}

func (p *Puppet) ClearCustomMXID() {
	save := p.CustomMXID != "" || p.AccessToken != ""
	p.CustomMXID = ""
	p.AccessToken = ""
	p.customIntent = nil
	p.customUser = nil
	if save {
		p.Update()
	}
}

func (p *Puppet) StartCustomMXID(reloginOnFail bool) error {
	newIntent, newAccessToken, err := p.bridge.DoublePuppet.Setup(p.CustomMXID, p.AccessToken, reloginOnFail)
	if err != nil {
		p.ClearCustomMXID()
		return err
	}
	if p.AccessToken != newAccessToken {
		p.AccessToken = newAccessToken
		p.Update()
	}
	p.customIntent = newIntent
	p.customUser = p.bridge.GetUserByMXID(p.CustomMXID)
	return nil
}

func (user *User) tryAutomaticDoublePuppeting() {
	if !user.bridge.Config.CanAutoDoublePuppet(user.MXID) {
		return
	}
	user.log.Debug().Msg("Checking if double puppeting needs to be enabled")
	puppet := user.bridge.GetPuppetByUID(user.UID)
	if len(puppet.CustomMXID) > 0 {
		user.log.Debug().Msg("User already has double-puppeting enabled")
		// Custom puppet already enabled
		return
	}
	puppet.CustomMXID = user.MXID
	puppet.EnablePresence = user.bridge.Config.Bridge.DefaultBridgePresence
	error := puppet.StartCustomMXID(true)
	if error != nil {
		user.log.Warn().Err(error).Msg("Failed to login with shared secret")
	} else {
		// TODO leave rooms with default puppet
		user.log.Debug().Msg("Successfully automatically enabled custom puppet")
	}
}
