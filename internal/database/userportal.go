package database

import (
	"database/sql"
	"errors"
	"time"
)

func (u *User) GetLastReadTS(portal PortalKey) time.Time {
	u.lastReadCacheLock.Lock()
	defer u.lastReadCacheLock.Unlock()

	if cached, ok := u.lastReadCache[portal]; ok {
		return cached
	}

	query := `
		SELECT last_read_ts
		FROM user_portal
		WHERE user_mxid=$1 AND portal_uid=$2 AND portal_receiver=$3
	`
	args := []interface{}{
		u.MXID, portal.UID, portal.Receiver,
	}

	var ts int64
	err := u.db.QueryRow(query, args...).Scan(&ts)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		u.log.Warnfln("Failed to scan last read timestamp from user portal table: %v", err)
	}
	if ts == 0 {
		u.lastReadCache[portal] = time.Time{}
	} else {
		u.lastReadCache[portal] = time.Unix(ts, 0)
	}

	return u.lastReadCache[portal]
}

func (u *User) SetLastReadTS(portal PortalKey, ts time.Time) {
	u.lastReadCacheLock.Lock()
	defer u.lastReadCacheLock.Unlock()

	query := `
		INSERT INTO user_portal
			(user_mxid, portal_uid, portal_receiver, last_read_ts)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (user_mxid, portal_uid, portal_receiver)
		DO UPDATE SET
			last_read_ts=excluded.last_read_ts
		WHERE user_portal.last_read_ts<excluded.last_read_ts
	`
	args := []interface{}{
		u.MXID, portal.UID, portal.Receiver, ts.Unix(),
	}

	_, err := u.db.Exec(query, args...)
	if err != nil {
		u.log.Warnfln("Failed to update last read timestamp: %v", err)
	} else {
		u.log.Debugfln("Set last read timestamp of %s in %s to %d", u.MXID, portal, ts.Unix())
		u.lastReadCache[portal] = ts
	}
}

func (u *User) IsInSpace(portal PortalKey) bool {
	u.inSpaceCacheLock.Lock()
	defer u.inSpaceCacheLock.Unlock()

	if cached, ok := u.inSpaceCache[portal]; ok {
		return cached
	}

	query := `
		SELECT in_space
		FROM user_portal
		WHERE user_mxid=$1 AND portal_uid=$2 AND portal_receiver=$3
	`
	args := []interface{}{
		u.MXID, portal.UID, portal.Receiver,
	}

	var inSpace bool
	err := u.db.QueryRow(query, args...).Scan(&inSpace)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		u.log.Warnfln("Failed to scan in space status from user portal table: %v", err)
	}
	u.inSpaceCache[portal] = inSpace

	return inSpace
}

func (u *User) MarkInSpace(portal PortalKey) {
	u.inSpaceCacheLock.Lock()
	defer u.inSpaceCacheLock.Unlock()

	query := `
		INSERT INTO user_portal
			(user_mxid, portal_uid, portal_receiver, in_space)
		VALUES ($1, $2, $3, true)
		ON CONFLICT (user_mxid, portal_uid, portal_receiver)
		DO UPDATE SET
			in_space=true
	`
	args := []interface{}{
		u.MXID, portal.UID, portal.Receiver,
	}

	_, err := u.db.Exec(query, args...)
	if err != nil {
		u.log.Warnfln("Failed to update in space status: %v", err)
	} else {
		u.inSpaceCache[portal] = true
	}
}
