package database

import (
	"database/sql"
	"time"

	"maunium.net/go/mautrix/id"
	"go.mau.fi/util/dbutil"

	"github.com/rs/zerolog"
)

type Portal struct {
	db  *Database
	log zerolog.Logger

	Key  PortalKey
	MXID id.RoomID

	Name      string
	NameSet   bool
	Topic     string
	TopicSet  bool
	Avatar    string
	AvatarURL id.ContentURI
	AvatarSet bool
	Encrypted bool
	LastSync  time.Time

	FirstEventID id.EventID
	NextBatchID  id.BatchID
}

func (p *Portal) Scan(row dbutil.Scannable) *Portal {
	var mxid, avatarURL, firstEventID, nextBatchID sql.NullString
	var lastSyncTs int64
	err := row.Scan(
		&p.Key.UID, &p.Key.Receiver, &mxid, &p.Name, &p.NameSet,
		&p.Topic, &p.TopicSet, &p.Avatar, &avatarURL, &p.AvatarSet,
		&p.Encrypted, &lastSyncTs, &firstEventID, &nextBatchID,
	)
	if err != nil {
		if err != sql.ErrNoRows {
			p.log.Error().Msgf("Database scan failed: %s", err)
		}

		return nil
	}
	if lastSyncTs > 0 {
		p.LastSync = time.Unix(lastSyncTs, 0)
	}
	p.MXID = id.RoomID(mxid.String)
	p.AvatarURL, _ = id.ParseContentURI(avatarURL.String)
	p.FirstEventID = id.EventID(firstEventID.String)
	p.NextBatchID = id.BatchID(nextBatchID.String)

	return p
}

func (p *Portal) Insert() {
	query := `
		INSERT INTO portal (uid, receiver, mxid, name, name_set, topic, topic_set, avatar, avatar_url,
							avatar_set, encrypted, last_sync, first_event_id, next_batch_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
	`
	args := []interface{}{
		p.Key.UID, p.Key.Receiver, strPtr(p.MXID.String()), p.Name, p.NameSet, p.Topic,
		p.TopicSet, p.Avatar, p.AvatarURL.String(), p.AvatarSet, p.Encrypted,
		p.lastSyncTs(), p.FirstEventID.String(), p.NextBatchID.String(),
	}

	_, err := p.db.Exec(query, args...)
	if err != nil {
		p.log.Warn().Msgf("Failed to insert %s: %v", p.Key, err)
	}
}

func (p *Portal) Update(txn dbutil.Transaction) {
	query := `
		UPDATE portal
		SET mxid=$1, name=$2, name_set=$3, topic=$4, topic_set=$5, avatar=$6, avatar_url=$7,
			avatar_set=$8, encrypted=$9, last_sync=$10, first_event_id=$11, next_batch_id=$12
		WHERE uid=$13 AND receiver=$14`
	args := []interface{}{
		strPtr(p.MXID.String()), p.Name, p.NameSet, p.Topic, p.TopicSet, p.Avatar,
		p.AvatarURL.String(), p.AvatarSet, p.Encrypted, p.lastSyncTs(), p.FirstEventID.String(),
		p.NextBatchID.String(), p.Key.UID, p.Key.Receiver,
	}

	var err error
	if txn != nil {
		_, err = txn.Exec(query, args...)
	} else {
		_, err = p.db.Exec(query, args...)
	}
	if err != nil {
		p.log.Warn().Msgf("Failed to update %s: %v", p.Key, err)
	}
}

func (p *Portal) Delete() {
	query := `
		DELETE FROM portal
		WHERE uid=$1 AND receiver=$2
	`
	args := []interface{}{
		p.Key.UID, p.Key.Receiver,
	}

	_, err := p.db.Exec(query, args...)
	if err != nil {
		p.log.Warn().Msgf("Failed to delete %s: %v", p.Key, err)
	}
}

func (p *Portal) lastSyncTs() int64 {
	if p.LastSync.IsZero() {
		return 0
	}

	return p.LastSync.Unix()
}
