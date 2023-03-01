package wechat

import (
	"sync"

	log "maunium.net/go/maulogger/v2"
)

type WechatClient struct {
	mxid string

	log log.Logger

	processFunc func(*Event)
	requestFunc func(*WechatClient, *Request) (any, error)

	connKey     string
	connKeyLock sync.RWMutex
}

func newWechatClient(mxid string, f func(*WechatClient, *Request) (any, error), log log.Logger) *WechatClient {
	return &WechatClient{
		mxid:        mxid,
		requestFunc: f,
		log:         log.Sub("Client").Sub(mxid),
	}
}

func (wc *WechatClient) SetProcessFunc(f func(*Event)) {
	wc.processFunc = f
}

func (wc *WechatClient) Connect() error {
	_, err := wc.requestFunc(wc, &Request{
		Type: ReqConnect,
	})
	return err
}

func (wc *WechatClient) Disconnect() error {
	_, err := wc.requestFunc(wc, &Request{
		Type: ReqDisconnect,
	})
	return err
}

func (wc *WechatClient) LoginWithQRCode() []byte {
	if data, err := wc.requestFunc(wc, &Request{
		Type: ReqLoginQR,
	}); err != nil {
		wc.log.Warnln("Failed to login with QR code:", err)
		return nil
	} else {
		return data.([]byte)
	}
}

func (wc *WechatClient) IsLoggedIn() bool {
	if data, err := wc.requestFunc(wc, &Request{
		Type: ReqIsLogin,
	}); err != nil {
		wc.log.Warnln("Failed to get login status:", err)
		return false
	} else {
		return data.(bool)
	}
}

func (wc *WechatClient) GetSelf() *UserInfo {
	if data, err := wc.requestFunc(wc, &Request{
		Type: ReqGetSelf,
	}); err != nil {
		wc.log.Warnln("Failed to get self info:", err)
		return nil
	} else {
		return data.(*UserInfo)
	}
}

func (wc *WechatClient) GetUserInfo(wxid string) *UserInfo {
	if data, err := wc.requestFunc(wc, &Request{
		Type: ReqGetUserInfo,
		Data: []string{wxid},
	}); err != nil {
		wc.log.Warnln("Failed to get user info:", err)
		return nil
	} else {
		return data.(*UserInfo)
	}
}

func (wc *WechatClient) GetGroupInfo(wxid string) *GroupInfo {
	if data, err := wc.requestFunc(wc, &Request{
		Type: ReqGetGroupInfo,
		Data: []string{wxid},
	}); err != nil {
		wc.log.Warnln("Failed to get group info:", err)
		return nil
	} else {
		return data.(*GroupInfo)
	}
}

func (wc *WechatClient) GetGroupMembers(wxid string) []string {
	if data, err := wc.requestFunc(wc, &Request{
		Type: ReqGetGroupMembers,
		Data: []string{wxid},
	}); err != nil {
		wc.log.Warnln("Failed to get group members:", err)
		return nil
	} else {
		return data.([]string)
	}
}

func (wc *WechatClient) GetGroupMemberNickname(group, wxid string) string {
	if data, err := wc.requestFunc(wc, &Request{
		Type: ReqGetGroupMemberNickname,
		Data: []string{group, wxid},
	}); err != nil {
		wc.log.Warnln("Failed to get group member nickname:", err)
		return ""
	} else {
		return data.(string)
	}
}

func (wc *WechatClient) GetFriendList() []*UserInfo {
	if data, err := wc.requestFunc(wc, &Request{
		Type: ReqGetFriendList,
	}); err != nil {
		wc.log.Warnln("Failed to get friend list:", err)
		return nil
	} else {
		return data.([]*UserInfo)
	}
}

func (wc *WechatClient) GetGroupList() []*GroupInfo {
	if data, err := wc.requestFunc(wc, &Request{
		Type: ReqGetGroupList,
	}); err != nil {
		wc.log.Warnln("Failed to get group list:", err)
		return nil
	} else {
		return data.([]*GroupInfo)
	}
}

func (wc *WechatClient) SendEvent(event *Event) (*Event, error) {
	if data, err := wc.requestFunc(wc, &Request{
		Type: ReqEvent,
		Data: event,
	}); err != nil {
		wc.log.Warnfln("Failed to send event:", err)
		return nil, err
	} else {
		return data.(*Event), nil
	}
}

func (wc *WechatClient) getConnKey() string {
	wc.connKeyLock.RLock()
	defer wc.connKeyLock.RUnlock()

	return wc.connKey
}

func (wc *WechatClient) setConnKey(key string) {
	wc.connKeyLock.Lock()
	defer wc.connKeyLock.Unlock()

	wc.connKey = key
}
