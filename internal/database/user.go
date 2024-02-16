package database

import (
	"database/sql"
	"sync"
	"time"

	"github.com/duo/matrix-wechat/internal/types"

	"github.com/rs/zerolog"
	"go.mau.fi/util/dbutil"
	"maunium.net/go/mautrix/id"
)

type User struct {
	db  *Database
	log zerolog.Logger

	MXID                     id.UserID
	UID                      types.UID
	ManagementRoom           id.RoomID
	SpaceRoom                id.RoomID
	OfficialAccountSpaceRoom id.RoomID

	lastReadCache     map[PortalKey]time.Time
	lastReadCacheLock sync.Mutex
	inSpaceCache      map[PortalKey]bool
	inSpaceCacheLock  sync.Mutex
}

func (u *User) Scan(row dbutil.Scannable) *User {
	var uin sql.NullString
	err := row.Scan(&u.MXID, &uin, &u.ManagementRoom, &u.SpaceRoom, &u.OfficialAccountSpaceRoom)
	if err != nil {
		if err != sql.ErrNoRows {
			u.log.Error().Msgf("Database scan failed: %v", err)
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
		INSERT INTO "user" (mxid, uin, management_room, space_room, official_account_space_room)
		VALUES ($1, $2, $3, $4, $5)
	`
	args := []interface{}{
		u.MXID, u.UID.Uin, u.ManagementRoom, u.SpaceRoom, u.OfficialAccountSpaceRoom,
	}

	_, err := u.db.Exec(query, args...)
	if err != nil {
		u.log.Warn().Msgf("Failed to insert %s: %v", u.MXID, err)
	}
}

func (u *User) Update() {
	query := `
		UPDATE "user"
		SET uin=$1, management_room=$2, space_room=$3, official_account_space_room=$4
		WHERE mxid=$5
	`
	args := []interface{}{
		u.UID.Uin, u.ManagementRoom, u.SpaceRoom, u.OfficialAccountSpaceRoom, u.MXID,
	}
	_, err := u.db.Exec(query, args...)
	if err != nil {
		u.log.Warn().Msgf("Failed to update %s: %v", u.MXID, err)
	}
}
