package wechat

import (
	"context"
	"time"

	log "maunium.net/go/maulogger/v2"
)

const requestTimeout = 30 * time.Second

type WechatClient struct {
	mxid    string
	service *WechatService
	hanlder func(*WebsocketMessage)

	log log.Logger
}

func NewWechatClient(mxid string, service *WechatService, handler func(*WebsocketMessage)) *WechatClient {
	return &WechatClient{
		mxid:    mxid,
		service: service,
		hanlder: handler,
		log:     service.log.Sub("Client"),
	}
}

func (c *WechatClient) Login() error {
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()

	return c.service.RequestWebsocket(ctx, &WebsocketRequest{
		MXID:    c.mxid,
		Command: CommandConnect,
	}, nil)
}

func (c *WechatClient) Disconnect() {
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()

	c.service.RequestWebsocket(ctx, &WebsocketRequest{
		MXID:    c.mxid,
		Command: CommandDisconnect,
	}, nil)

	c.service.RemoveClient(c.mxid)
}

func (c *WechatClient) IsLoggedIn() bool {
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()

	var data IsLoginData
	err := c.service.RequestWebsocket(ctx, &WebsocketRequest{
		MXID:    c.mxid,
		Command: CommandIsLogin,
	}, &data)

	if err != nil {
		c.log.Warnln("Failed to get login status:", err)
		return false
	}

	return data.Status
}

func (c *WechatClient) GetSelf() *UserInfo {
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()

	var data UserInfo
	err := c.service.RequestWebsocket(ctx, &WebsocketRequest{
		MXID:    c.mxid,
		Command: CommandGetSelf,
	}, &data)

	if err != nil {
		c.log.Warnln("Failed to get self info:", err)
		return nil
	}

	return &data
}

func (c *WechatClient) GetUserInfo(wxid string) *UserInfo {
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()

	var data UserInfo
	err := c.service.RequestWebsocket(ctx, &WebsocketRequest{
		MXID:    c.mxid,
		Command: CommandGetUserInfo,
		Data:    &QueryData{ID: wxid},
	}, &data)

	if err != nil {
		c.log.Warnln("Failed to get user info:", err)
		return nil
	}

	return &data
}

func (c *WechatClient) GetGroupInfo(wxid string) *GroupInfo {
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()

	var data GroupInfo
	err := c.service.RequestWebsocket(ctx, &WebsocketRequest{
		MXID:    c.mxid,
		Command: CommandGetGroupInfo,
		Data:    &QueryData{ID: wxid},
	}, &data)

	if err != nil {
		c.log.Warnln("Failed to get group info:", err)
		return nil
	}

	return &data
}

func (c *WechatClient) GetGroupMembers(wxid string) []string {
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()

	var data []string
	err := c.service.RequestWebsocket(ctx, &WebsocketRequest{
		MXID:    c.mxid,
		Command: CommandGetGroupMembers,
		Data:    &QueryData{ID: wxid},
	}, &data)

	if err != nil {
		c.log.Warnln("Failed to get group info:", err)
		return nil
	}

	return data
}

func (c *WechatClient) GetFriendList() []*UserInfo {
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()

	var friends []*UserInfo
	err := c.service.RequestWebsocket(ctx, &WebsocketRequest{
		MXID:    c.mxid,
		Command: CommandGetFriendList,
	}, &friends)

	if err != nil {
		c.log.Warnln("Failed to get friend list:", err)
		return []*UserInfo{}
	}

	return friends
}

func (c *WechatClient) GetGroupList() []*GroupInfo {
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()

	var groups []*GroupInfo
	err := c.service.RequestWebsocket(ctx, &WebsocketRequest{
		MXID:    c.mxid,
		Command: CommandGetGroupList,
	}, &groups)

	if err != nil {
		c.log.Warnln("Failed to get group list:", err)
		return []*GroupInfo{}
	}

	return groups
}

func (c *WechatClient) SendMessage(msg *MatrixMessage) error {
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()

	return c.service.RequestWebsocket(ctx, &WebsocketRequest{
		MXID:    c.mxid,
		Command: CommandSendMessage,
		Data:    msg,
	}, nil)
}

func (c *WechatClient) HandleEvent(msg *WebsocketMessage) {
	c.hanlder(msg)
}
