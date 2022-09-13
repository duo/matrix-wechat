package database

import (
	"database/sql"
	"sync"
	"time"

	"github.com/duo/matrix-wechat/internal/types"

	"maunium.net/go/mautrix/id"
	"maunium.net/go/mautrix/util/dbutil"

	log "maunium.net/go/maulogger/v2"
)

type User struct {
	db  *Database
	log log.Logger

	MXID           id.UserID
	UID            types.UID
	ManagementRoom id.RoomID
	SpaceRoom      id.RoomID

	lastReadCache     map[PortalKey]time.Time
	lastReadCacheLock sync.Mutex
	inSpaceCache      map[PortalKey]bool
	inSpaceCacheLock  sync.Mutex
}

func (u *User) Scan(row dbutil.Scannable) *User {
	var uin sql.NullString
	err := row.Scan(&u.MXID, &uin, &u.ManagementRoom, &u.SpaceRoom)
	if err != nil {
		if err != sql.ErrNoRows {
			u.log.Errorln("Database scan failed:", err)
		}

		return nil
	}
	if len(uin.String) > 0 {
		u.UID = types.NewUserUID(uin.String)
	}

	return u
}

func (u *User) Insert() {
	query := `
		INSERT INTO "user" (mxid, uin, management_room, space_room)
		VALUES ($1, $2, $3, $4)
	`
	args := []interface{}{
		u.MXID, u.UID.Uin, u.ManagementRoom, u.SpaceRoom,
	}

	_, err := u.db.Exec(query, args...)
	if err != nil {
		u.log.Warnfln("Failed to insert %s: %v", u.MXID, err)
	}
}

func (u *User) Update() {
	query := `
		UPDATE "user"
		SET uin=$1, management_room=$2, space_room=$3
		WHERE mxid=$4
	`
	args := []interface{}{
		u.UID.Uin, u.ManagementRoom, u.SpaceRoom, u.MXID,
	}
	_, err := u.db.Exec(query, args...)
	if err != nil {
		u.log.Warnfln("Failed to update %s: %v", u.MXID, err)
	}
}
