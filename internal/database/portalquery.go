package database

import (
	"fmt"

	"github.com/duo/matrix-wechat/internal/types"

	"github.com/rs/zerolog"
	"maunium.net/go/mautrix/id"
)

const portalColumns = `
	uid, receiver, mxid, name, name_set, topic, topic_set, avatar, avatar_url,
	avatar_set, encrypted, last_sync, first_event_id, next_batch_id
`

type PortalQuery struct {
	db  *Database
	log zerolog.Logger
}

func (pq *PortalQuery) New() *Portal {
	return &Portal{
		db:  pq.db,
		log: pq.log,
	}
}

func (pq *PortalQuery) GetAll() []*Portal {
	query := fmt.Sprintf("SELECT %s FROM portal", portalColumns)

	return pq.getAll(query)
}

func (pq *PortalQuery) GetByUID(key PortalKey) *Portal {
	query := fmt.Sprintf("SELECT %s FROM portal WHERE uid=$1 AND receiver=$2", portalColumns)
	args := []interface{}{
		key.UID, key.Receiver,
	}

	return pq.get(query, args...)
}

func (pq *PortalQuery) GetByMXID(mxid id.RoomID) *Portal {
	query := fmt.Sprintf("SELECT %s FROM portal WHERE mxid=$1", portalColumns)
	args := []interface{}{mxid}

	return pq.get(query, args...)
}

func (pq *PortalQuery) GetAllByUID(uid types.UID) []*Portal {
	query := fmt.Sprintf("SELECT %s FROM portal WHERE uid=$1", portalColumns)
	args := []interface{}{uid}

	return pq.getAll(query, args...)
}

func (pq *PortalQuery) FindPrivateChats(receiver types.UID) []*Portal {
	query := fmt.Sprintf(
		"SELECT %s FROM portal WHERE receiver=$1 AND uid LIKE '%%%s%s'",
		portalColumns, types.SEP_UID, types.User,
	)
	args := []interface{}{receiver}

	return pq.getAll(query, args...)
}

func (pq *PortalQuery) FindPrivateChatsNotInSpace(receiver types.UID) []PortalKey {
	keys := []PortalKey{}

	query := `
		SELECT uid FROM portal
		    LEFT JOIN user_portal ON portal.uid=user_portal.portal_uid AND portal.receiver=user_portal.portal_receiver
		WHERE mxid<>'' AND receiver=$1 AND (in_space=false OR in_space IS NULL)
	`
	args := []interface{}{receiver}

	rows, err := pq.db.Query(query, args...)
	if err != nil || rows == nil {
		return keys
	}

	defer rows.Close()
	for rows.Next() {
		var key PortalKey
		key.Receiver = receiver
		err = rows.Scan(&key.UID)
		if err == nil {
			keys = append(keys, key)
		}
	}

	return keys
}

func (pq *PortalQuery) getAll(query string, args ...interface{}) []*Portal {
	portals := []*Portal{}

	rows, err := pq.db.Query(query, args...)
	if err != nil || rows == nil {
		return portals
	}

	defer rows.Close()
	for rows.Next() {
		portals = append(portals, pq.New().Scan(rows))
	}

	return portals
}

func (pq *PortalQuery) get(query string, args ...interface{}) *Portal {
	row := pq.db.QueryRow(query, args...)
	if row == nil {
		return nil
	}

	return pq.New().Scan(row)
}
