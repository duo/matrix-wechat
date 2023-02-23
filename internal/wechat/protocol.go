package wechat

import (
	"encoding/json"
	"fmt"
	"net/http"
)

type Message struct {
	ID   int64       `json:"id"`
	MXID string      `json:"mxid"`
	Type MessageType `json:"type"`
	Data any         `json:"data,omitempty"`
}

type Request struct {
	Type RequestType `json:"type"`
	Data any         `json:"data,omitempty"`
}

type Response struct {
	Type  ResponseType   `json:"type"`
	Error *ErrorResponse `json:"error,omitempty"`
	Data  any            `json:"data,omitempty"`
}

type ErrorResponse struct {
	HTTPStatus int    `json:"-"`
	Code       string `json:"code"`
	Message    string `json:"message"`
}

type Event struct {
	ID        string     `json:"id"`
	ThreadID  string     `json:"thread_id,omitempty"`
	Timestamp int64      `json:"timestamp"`
	From      User       `json:"from"`
	Chat      Chat       `json:"chat"`
	Type      EventType  `json:"type"`
	Content   string     `json:"content,omitempty"`
	Mentions  []string   `json:"mentions,omitempty"`
	Reply     *ReplyInfo `json:"reply,omitempty"`
	Data      any        `json:"data,omitempty"`
}

type User struct {
	ID       string `json:"id"`
	Username string `json:"username"`
	Remark   string `json:"remark,omitempty"`
}

type Chat struct {
	ID    string   `json:"id"`
	Type  ChatType `json:"type"`
	Title string   `json:"title,omitempty"`
}

type ReplyInfo struct {
	ID        string `json:"id"`
	Timestamp int64  `json:"ts"`
	Sender    string `json:"sender"`
	Content   string `json:"content"`
}

type AppData struct {
	Title       string `json:"title,omitempty"`
	Description string `json:"desc,omitempty"`
	Source      string `json:"source,omitempty"`
	URL         string `json:"url,omitempty"`

	Content string               `json:"raw,omitempty"`
	Blobs   map[string]*BlobData `json:"blobs,omitempty"`
}

type LocationData struct {
	Name      string  `json:"name,omitempty"`
	Address   string  `json:"address,omitempty"`
	Longitude float64 `json:"longitude"`
	Latitude  float64 `json:"latitude"`
}

type BlobData struct {
	Name   string `json:"name,omitempty"`
	Mime   string `json:"mime,omitempty"`
	Binary []byte `json:"binary"`
}

func (o *Message) UnmarshalJSON(data []byte) error {
	type cloneType Message

	rawMsg := json.RawMessage{}
	o.Data = &rawMsg

	if err := json.Unmarshal(data, (*cloneType)(o)); err != nil {
		return err
	}

	switch o.Type {
	case MsgRequest:
		var request *Request
		if err := json.Unmarshal(rawMsg, &request); err != nil {
			return err
		}
		o.Data = request
	case MsgResponse:
		var response *Response
		if err := json.Unmarshal(rawMsg, &response); err != nil {
			return err
		}
		o.Data = response
	}

	return nil
}

func (o *Request) UnmarshalJSON(data []byte) error {
	type cloneType Request

	rawMsg := json.RawMessage{}
	o.Data = &rawMsg

	if err := json.Unmarshal(data, (*cloneType)(o)); err != nil {
		return err
	}

	switch o.Type {
	case ReqEvent:
		var event *Event
		if err := json.Unmarshal(rawMsg, &event); err != nil {
			return err
		}
		o.Data = event
	case ReqGetUserInfo, ReqGetGroupInfo, ReqGetGroupMembers, ReqGetGroupMemberNickname:
		var params []string
		if err := json.Unmarshal(rawMsg, &params); err != nil {
			return err
		}
		o.Data = params
	}

	return nil
}

func (o *Response) UnmarshalJSON(data []byte) error {
	type cloneType Response

	rawMsg := json.RawMessage{}
	o.Data = &rawMsg

	if err := json.Unmarshal(data, (*cloneType)(o)); err != nil {
		return err
	}

	if o.Error != nil {
		return nil
	}

	switch o.Type {
	case RespEvent:
		var event *Event
		if err := json.Unmarshal(rawMsg, &event); err != nil {
			return err
		}
		o.Data = event
	case RespLoginQR:
		var code []byte
		if err := json.Unmarshal(rawMsg, &code); err != nil {
			return err
		}
		o.Data = code
	case RespIsLogin:
		var status bool
		if err := json.Unmarshal(rawMsg, &status); err != nil {
			return err
		}
		o.Data = status
	case RespGetSelf, RespGetUserInfo:
		var info *UserInfo
		if err := json.Unmarshal(rawMsg, &info); err != nil {
			return err
		}
		o.Data = info
	case RespGetGroupInfo:
		var info *GroupInfo
		if err := json.Unmarshal(rawMsg, &info); err != nil {
			return err
		}
		o.Data = info
	case RespGetGroupMembers:
		var members []string
		if err := json.Unmarshal(rawMsg, &members); err != nil {
			return err
		}
		o.Data = members
	case RespGetGroupMemberNickname:
		var nickname string
		if err := json.Unmarshal(rawMsg, &nickname); err != nil {
			return err
		}
		o.Data = nickname
	case RespGetFriendList:
		var friends []*UserInfo
		if err := json.Unmarshal(rawMsg, &friends); err != nil {
			return err
		}
		o.Data = friends
	case RespGetGroupList:
		var groups []*GroupInfo
		if err := json.Unmarshal(rawMsg, &groups); err != nil {
			return err
		}
		o.Data = groups
	default:
	}

	return nil
}

func (o *Event) UnmarshalJSON(data []byte) error {
	type cloneType Event

	rawMsg := json.RawMessage{}
	o.Data = &rawMsg

	if err := json.Unmarshal(data, (*cloneType)(o)); err != nil {
		return err
	}

	switch o.Type {
	case EventPhoto:
		var photos []*BlobData
		if err := json.Unmarshal(rawMsg, &photos); err != nil {
			return err
		}
		o.Data = photos
	case EventAudio, EventVideo, EventFile:
		var blob *BlobData
		if err := json.Unmarshal(rawMsg, &blob); err != nil {
			return err
		}
		o.Data = blob
	case EventLocation:
		var location *LocationData
		if err := json.Unmarshal(rawMsg, &location); err != nil {
			return err
		}
		o.Data = location
	case EventApp:
		var app *AppData
		if err := json.Unmarshal(rawMsg, &app); err != nil {
			return err
		}
		o.Data = app
	}

	return nil
}

const (
	MsgRequest MessageType = iota
	MsgResponse
)

const (
	ReqEvent RequestType = iota
	ReqConnect
	ReqDisconnect
	ReqLoginQR
	ReqIsLogin
	ReqGetSelf
	ReqGetUserInfo
	ReqGetGroupInfo
	ReqGetGroupMembers
	ReqGetGroupMemberNickname
	ReqGetFriendList
	ReqGetGroupList
)

const (
	RespEvent ResponseType = iota
	RespConnect
	RespDisconnect
	RespLoginQR
	RespIsLogin
	RespGetSelf
	RespGetUserInfo
	RespGetGroupInfo
	RespGetGroupMembers
	RespGetGroupMemberNickname
	RespGetFriendList
	RespGetGroupList
)

const (
	ChatPrivate ChatType = iota
	ChatGroup
)

const (
	EventText EventType = iota
	EventPhoto
	EventAudio
	EventVideo
	EventFile
	EventLocation
	EventNotice
	EventApp
	EventRevoke
	EventVoIP
	EventSystem
)

type MessageType int

func (t MessageType) String() string {
	switch t {
	case MsgRequest:
		return "request"
	case MsgResponse:
		return "response"
	default:
		return "unknown"
	}
}

type RequestType int

func (t RequestType) String() string {
	switch t {
	case ReqEvent:
		return "event"
	case ReqConnect:
		return "connect"
	case ReqDisconnect:
		return "disconnect"
	case ReqLoginQR:
		return "login_qr"
	case ReqIsLogin:
		return "is_login"
	case ReqGetSelf:
		return "get_self"
	case ReqGetUserInfo:
		return "get_user_info"
	case ReqGetGroupInfo:
		return "get_group_info"
	case ReqGetGroupMembers:
		return "get_group_members"
	case ReqGetGroupMemberNickname:
		return "get_group_member_nickname"
	case ReqGetFriendList:
		return "get_friend_list"
	case ReqGetGroupList:
		return "get_group_list"
	default:
		return "unknown"
	}
}

type ResponseType int

func (t ResponseType) String() string {
	switch t {
	case RespEvent:
		return "event"
	case RespConnect:
		return "connect"
	case RespDisconnect:
		return "disconnect"
	case RespLoginQR:
		return "login_qr"
	case RespIsLogin:
		return "is_login"
	case RespGetSelf:
		return "get_self"
	case RespGetUserInfo:
		return "get_user_info"
	case RespGetGroupInfo:
		return "get_group_info"
	case RespGetGroupMembers:
		return "get_group_members"
	case RespGetGroupMemberNickname:
		return "get_group_member_nickname"
	case RespGetFriendList:
		return "get_friend_list"
	case RespGetGroupList:
		return "get_group_list"
	default:
		return "unknown"
	}
}

type ChatType int

func (t ChatType) String() string {
	switch t {
	case ChatPrivate:
		return "private"
	case ChatGroup:
		return "group"
	default:
		return "unknown"
	}
}

type EventType int

func (t EventType) String() string {
	switch t {
	case EventText:
		return "text"
	case EventPhoto:
		return "photo"
	case EventAudio:
		return "audio"
	case EventVideo:
		return "video"
	case EventFile:
		return "file"
	case EventLocation:
		return "location"
	case EventNotice:
		return "notice"
	case EventApp:
		return "app"
	case EventRevoke:
		return "revoke"
	case EventVoIP:
		return "voip"
	case EventSystem:
		return "system"
	default:
		return "unknown"
	}
}

type UserInfo struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Avatar string `json:"avatar,omitempty"`
	Remark string `json:"remark,omitempty"`
}

type GroupInfo struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Avatar  string   `json:"avatar,omitempty"`
	Notice  string   `json:"notice,omitempty"`
	Members []string `json:"members"`
}

func (er *ErrorResponse) Error() string {
	return fmt.Sprintf("%s: %s", er.Code, er.Message)
}

func (er ErrorResponse) Write(w http.ResponseWriter) {
	w.Header().Add("Content-Type", "application/json")
	w.WriteHeader(er.HTTPStatus)
	_ = Respond(w, &er)
}

func Respond(w http.ResponseWriter, data any) error {
	w.Header().Add("Content-Type", "application/json")
	dataStr, err := json.Marshal(data)
	if err != nil {
		return err
	}
	_, err = w.Write(dataStr)
	return err
}
