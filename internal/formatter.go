package internal

import (
	"fmt"
	"slices"
	"sort"

	"github.com/duo/matrix-wechat/internal/types"

	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/format"
	"maunium.net/go/mautrix/id"
)

const mentionedUIDsContextKey = "me.lxduo.wechat.mentioned_uids"
const allowedMentionsContextKey = "me.lxduo.wechat.allowed_mentions"

type Formatter struct {
	bridge *WechatBridge

	matrixHTMLParser *format.HTMLParser
}

func NewFormatter(br *WechatBridge) *Formatter {
	formatter := &Formatter{
		bridge: br,
		matrixHTMLParser: &format.HTMLParser{
			TabsToSpaces: 4,
			Newline:      "\n",

			PillConverter: func(displayname, mxid, eventID string, ctx format.Context) string {
				allowedMentions, _ := ctx.ReturnData[allowedMentionsContextKey].(map[types.UID]bool)
				if mxid[0] == '@' {
					puppet := br.GetPuppetByMXID(id.UserID(mxid))
					if puppet != nil && (allowedMentions == nil || allowedMentions[puppet.UID]) {
						if allowedMentions == nil {
							uids, ok := ctx.ReturnData[mentionedUIDsContextKey].([]string)
							if !ok {
								ctx.ReturnData[mentionedUIDsContextKey] = []string{puppet.UID.Uin}
							} else {
								ctx.ReturnData[mentionedUIDsContextKey] = append(uids, puppet.UID.Uin)
							}
						}
						return "@" + puppet.UID.Uin
					}
				}
				return displayname
			},
			BoldConverter:           func(text string, _ format.Context) string { return fmt.Sprintf("*%s*", text) },
			ItalicConverter:         func(text string, _ format.Context) string { return fmt.Sprintf("_%s_", text) },
			StrikethroughConverter:  func(text string, _ format.Context) string { return fmt.Sprintf("~%s~", text) },
			MonospaceConverter:      func(text string, _ format.Context) string { return fmt.Sprintf("```%s```", text) },
			MonospaceBlockConverter: func(text, language string, _ format.Context) string { return fmt.Sprintf("```%s```", text) },
		},
	}
	return formatter
}

func (f *Formatter) GetMatrixInfoByUID(roomID id.RoomID, uid types.UID) (id.UserID, string) {
	var mxid id.UserID
	var displayname string
	if puppet := f.bridge.GetPuppetByUID(uid); puppet != nil {
		mxid = puppet.MXID
		displayname = puppet.Displayname
	}
	if user := f.bridge.GetUserByUID(uid); user != nil {
		mxid = user.MXID
		member := f.bridge.StateStore.GetMember(roomID, user.MXID)
		if len(member.Displayname) > 0 {
			displayname = member.Displayname
		}
	}

	return mxid, displayname
}

func (f *Formatter) ParseMatrix(html string, mentions *event.Mentions) (string, []string) {
	ctx := format.NewContext()

	var mentionedUIDs []string
	if mentions != nil {
		var allowedMentions = make(map[types.UID]bool)
		mentionedUIDs = make([]string, 0, len(mentions.UserIDs))
		for _, userID := range mentions.UserIDs {
			var uid types.UID
			if puppet := f.bridge.GetPuppetByMXID(userID); puppet != nil {
				uid = puppet.UID
				mentionedUIDs = append(mentionedUIDs, puppet.UID.Uin)
			} else if user := f.bridge.GetUserByMXIDIfExists(userID); user != nil {
				uid = user.UID
			}
			if !uid.IsEmpty() && !allowedMentions[uid] {
				allowedMentions[uid] = true
				mentionedUIDs = append(mentionedUIDs, uid.Uin)
			}
		}
		ctx.ReturnData[allowedMentionsContextKey] = allowedMentions
	}

	result := f.matrixHTMLParser.Parse(html, ctx)
	if mentions == nil {
		mentionedUIDs, _ = ctx.ReturnData[mentionedUIDsContextKey].([]string)
		sort.Strings(mentionedUIDs)
		mentionedUIDs = slices.Compact(mentionedUIDs)
	}
	f.bridge.ZLog.Error().Msgf("WTF mentions: %+v", mentionedUIDs)
	return result, mentionedUIDs
}
