package internal

import (
	"maunium.net/go/mautrix/bridge/status"
)

const (
	WechatLoggedOut        status.BridgeStateErrorCode = "wechat-logged-out"
	WechatNotConnected     status.BridgeStateErrorCode = "wechat-not-connected"
	WechatConnecting       status.BridgeStateErrorCode = "wechat-connecting"
	WechatConnectionFailed status.BridgeStateErrorCode = "wechat-connection-failed"
)

func init() {
	status.BridgeStateHumanErrors.Update(status.BridgeStateErrorMap{
		WechatLoggedOut:        "You were logged out from another device. Relogin to continue using the bridge.",
		WechatNotConnected:     "You're not connected to Wechat.",
		WechatConnecting:       "Reconnecting to Wechat...",
		WechatConnectionFailed: "Connect to the Wechat servers failed.",
	})
}

func (user *User) GetRemoteID() string {
	if user == nil || user.UID.IsEmpty() {
		return ""
	}

	return user.UID.String()
}

func (user *User) GetRemoteName() string {
	if user == nil || user.UID.IsEmpty() {
		return ""
	}

	return user.UID.Uin
}
