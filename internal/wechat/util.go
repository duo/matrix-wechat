package wechat

import "maunium.net/go/mautrix/event"

func ToEventType(t event.MessageType) EventType {
	switch t {
	case event.MsgText:
		return EventText
	case event.MsgImage:
		return EventPhoto
	case event.MsgAudio:
		return EventAudio
	case event.MsgVideo:
		return EventVideo
	case event.MsgFile:
		return EventFile
	case event.MsgLocation:
		return EventLocation
	default:
		return EventText
	}
}

func ToMessageType(t EventType) event.MessageType {
	switch t {
	case EventText:
		return event.MsgText
	case EventPhoto:
		return event.MsgImage
	case EventSticker:
		return event.MsgImage
	case EventAudio:
		return event.MsgAudio
	case EventVideo:
		return event.MsgVideo
	case EventFile:
		return event.MsgFile
	case EventLocation:
		return event.MsgLocation
	default:
		return event.MsgText
	}
}
