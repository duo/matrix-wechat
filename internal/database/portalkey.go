package database

import (
	"fmt"

	"github.com/duo/matrix-wechat/internal/types"
)

const SEP_PORTAL = "\u0002"

type PortalKey struct {
	UID      types.UID
	Receiver types.UID
}

func NewPortalKey(uid, receiver types.UID) PortalKey {
	if uid.IsGroup() {
		receiver = uid
	}

	return PortalKey{
		UID:      uid,
		Receiver: receiver,
	}
}

func (pk PortalKey) String() string {
	if pk.Receiver == pk.UID {
		return pk.UID.String()
	}

	return fmt.Sprintf("%s%s%s", pk.UID, SEP_PORTAL, pk.Receiver)
}
