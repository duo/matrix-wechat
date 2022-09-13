package database

import (
	"database/sql"
	"time"

	"github.com/duo/matrix-wechat/internal/types"

	"maunium.net/go/mautrix/id"
	"maunium.net/go/mautrix/util/dbutil"

	log "maunium.net/go/maulogger/v2"
)

type Puppet struct {
	db          *Database
	log         log.Logger
	UID         types.UID
	Avatar      string
	AvatarURL   id.ContentURI
	AvatarSet   bool
	Displayname string
	NameQuality int8
	NameSet     bool
	LastSync    time.Time

	CustomMXID     id.UserID
	AccessToken    string
	NextBatch      string
	EnablePresence bool
}

func (p *Puppet) Scan(row dbutil.Scannable) *Puppet {
	var displayname, avatar, avatarURL, customMXID, accessToken, nextBatch sql.NullString
	var quality, lastSync sql.NullInt64
	var enablePresence, nameSet, avatarSet sql.NullBool
	var uin string
	err := row.Scan(
		&uin, &avatar, &avatarURL, &displayname, &quality, &nameSet, &avatarSet,
		&lastSync, &customMXID, &accessToken, &nextBatch, &enablePresence,
	)
	if err != nil {
		if err != sql.ErrNoRows {
			p.log.Errorln("Database scan failed:", err)
		}

		return nil
	}
	p.UID = types.NewUserUID(uin)
	p.Displayname = displayname.String
	p.Avatar = avatar.String
	p.AvatarURL, _ = id.ParseContentURI(avatarURL.String)
	p.NameQuality = int8(quality.Int64)
	p.NameSet = nameSet.Bool
	p.AvatarSet = avatarSet.Bool
	if lastSync.Int64 > 0 {
		p.LastSync = time.Unix(lastSync.Int64, 0)
	}
	p.CustomMXID = id.UserID(customMXID.String)
	p.AccessToken = accessToken.String
	p.NextBatch = nextBatch.String
	p.EnablePresence = enablePresence.Bool

	return p
}

func (p *Puppet) Insert() {
	var lastSyncTs int64
	if !p.LastSync.IsZero() {
		lastSyncTs = p.LastSync.Unix()
	}

	query := `
		INSERT INTO puppet (uin, avatar, avatar_url, avatar_set, displayname, name_quality, name_set,
							last_sync, custom_mxid, access_token, next_batch, enable_presence)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
	`
	args := []interface{}{
		p.UID.Uin, p.Avatar, p.AvatarURL.String(), p.AvatarSet, p.Displayname, p.NameQuality,
		p.NameSet, lastSyncTs, p.CustomMXID, p.AccessToken, p.NextBatch, p.EnablePresence,
	}

	_, err := p.db.Exec(query, args...)
	if err != nil {
		p.log.Warnfln("Failed to insert %s: %v", p.UID, err)
	}
}

func (p *Puppet) Update() {
	var lastSyncTs int64
	if !p.LastSync.IsZero() {
		lastSyncTs = p.LastSync.Unix()
	}

	query := `
		UPDATE puppet
		SET displayname=$1, name_quality=$2, name_set=$3, avatar=$4, avatar_url=$5, avatar_set=$6,
			last_sync=$7, custom_mxid=$8, access_token=$9, next_batch=$10, enable_presence=$11
		WHERE uin=$12
	`
	args := []interface{}{
		p.Displayname, p.NameQuality, p.NameSet, p.Avatar, p.AvatarURL.String(), p.AvatarSet,
		lastSyncTs, p.CustomMXID, p.AccessToken, p.NextBatch, p.EnablePresence, p.UID.Uin,
	}

	_, err := p.db.Exec(query, args...)
	if err != nil {
		p.log.Warnfln("Failed to update %s: %v", p.UID, err)
	}
}
