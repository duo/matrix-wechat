package internal

import (
	"github.com/duo/matrix-wechat/internal/types"

	"maunium.net/go/mautrix/format"
	"maunium.net/go/mautrix/id"
)

const mentionedUIDsContextKey = "me.lxduo.wechat.mentioned_uids"

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
				if mxid[0] == '@' {
					puppet := br.GetPuppetByMXID(id.UserID(mxid))
					if puppet != nil {
						uids, ok := ctx.ReturnData[mentionedUIDsContextKey].([]string)
						if !ok {
							ctx.ReturnData[mentionedUIDsContextKey] = []string{puppet.UID.Uin}
						} else {
							ctx.ReturnData[mentionedUIDsContextKey] = append(uids, puppet.UID.Uin)
						}
						return "@" + puppet.UID.Uin
					}
				}
				return mxid
			},
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

func (f *Formatter) ParseMatrix(html string) (string, []string) {
	ctx := format.NewContext()
	result := f.matrixHTMLParser.Parse(html, ctx)
	mentionedUIDs, _ := ctx.ReturnData[mentionedUIDsContextKey].([]string)
	return result, mentionedUIDs
}
