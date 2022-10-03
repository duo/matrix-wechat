package wechat

import (
	"encoding/json"
	"time"
)

const (
	CommandConnect                = "connect"
	CommandDisconnect             = "disconnect"
	CommandLoginWithQRCode        = "login_qr"
	CommandIsLogin                = "is_login"
	CommandGetSelf                = "get_self"
	CommandGetUserInfo            = "get_user_info"
	CommandGetGroupInfo           = "get_group_info"
	CommandGetGroupMembers        = "get_group_members"
	CommandGetGroupMemberNickname = "get_group_member_nickname"
	CommandGetFriendList          = "get_friend_list"
	CommandGetGroupList           = "get_group_list"
	CommandSendMessage            = "send_message"

	CommandResponse = "response"
	CommandError    = "error"
	CommandPing     = "ping"

	CommandClosed = "__websocket_closed"

	EventText     = "m.text"
	EventImage    = "m.image"
	EventAudio    = "m.audio"
	EventVideo    = "m.video"
	EventFile     = "m.file"
	EventLocation = "m.location"
	EventNotice   = "m.notice"
	EventApp      = "m.app"
	EventRevoke   = "m.revoke"
)

type UserInfo struct {
	ID        string `json:"wxId"`
	Nickname  string `json:"wxNickName"`
	BigAvatar string `json:"wxBigAvatar"`
}

type GroupInfo struct {
	ID        string   `json:"wxId"`
	Name      string   `json:"wxNickName"`
	BigAvatar string   `json:"wxBigAvatar"`
	Notice    string   `json:"notice"`
	Members   []string `json:"members"`
}

type IsLoginData struct {
	Status bool `json:"status"`
}

type QueryData struct {
	ID    string `json:"wxId"`
	Group string `json:"groupId"`
}

type MatrixMessage struct {
	Target      string      `json:"target"`
	MessageType string      `json:"type"`
	Content     string      `json:"content"`
	Data        interface{} `json:"data,omitempty"`
}

type WebsocketRequest struct {
	MXID    string      `json:"mxid"`
	ReqID   int         `json:"req,omitempty"`
	Command string      `json:"command,omitempty"`
	Data    interface{} `json:"data,omitempty"`

	Deadline time.Duration `json:"-,omitempty"`
}

type WebsocketCommand struct {
	ReqID   int             `json:"req,omitempty"`
	Command string          `json:"command,omitempty"`
	Data    json.RawMessage `json:"data,omitempty"`
}

type ReplyInfo struct {
	ID     uint64 `json:"id"`
	Sender string `json:"sender"`
}

type WebsocketEvent struct {
	ID        uint64          `json:"id,omitempty"`
	Timestamp int64           `json:"ts,omitempty"`
	Sender    string          `json:"sender,omitempty"`
	Target    string          `json:"target,omitempty"`
	EventType string          `json:"type,omitempty"`
	Content   string          `json:"content,omitempty"`
	Reply     ReplyInfo       `json:"reply,omitempty"`
	Extra     json.RawMessage `json:"extra,omitempty"`
}

type BlobData struct {
	Name   string `json:"name,omitempty"`
	Binary []byte `json:"binary,omitempty"`
}

type LocationData struct {
	Name      string  `json:"name,omitempty"`
	Address   string  `json:"address,omitempty"`
	Longitude float64 `json:"longitude,omitempty"`
	Latitude  float64 `json:"latitude,omitempty"`
}

type LinkData struct {
	Title       string `json:"title,omitempty"`
	Description string `json:"desc,omitempty"`
	URL         string `json:"url,omitempty"`
}

type WebsocketMessage struct {
	WebsocketEvent
	WebsocketCommand
	MXID string `json:"mxid"`
}
