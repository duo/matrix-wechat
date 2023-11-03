package database

import (
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/duo/matrix-wechat/internal/types"

	"go.mau.fi/util/dbutil"
	"maunium.net/go/mautrix/id"

	"github.com/rs/zerolog"
)

type MessageErrorType string

const (
	MsgNoError             MessageErrorType = ""
	MsgErrDecryptionFailed MessageErrorType = "decryption_failed"
	MsgErrMediaNotFound    MessageErrorType = "media_not_found"
)

type MessageType string

const (
	MsgUnknown MessageType = ""
	MsgFake    MessageType = "fake"
	MsgNormal  MessageType = "message"
)

type Message struct {
	db  *Database
	log zerolog.Logger

	Chat      PortalKey
	MsgID     string
	MXID      id.EventID
	Sender    types.UID
	Timestamp time.Time
	Sent      bool
	Type      MessageType
	Error     MessageErrorType
}

func (m *Message) IsFakeMXID() bool {
	return strings.HasPrefix(m.MXID.String(), "me.lxduo.wechat.fake::")
}

func (m *Message) IsFakeMsgID() bool {
	return strings.HasPrefix(m.MsgID, "FAKE::") || m.MsgID == string(m.MXID)
}

func (m *Message) Scan(row dbutil.Scannable) *Message {
	var ts int64
	err := row.Scan(
		&m.Chat.UID, &m.Chat.Receiver, &m.MsgID, &m.MXID,
		&m.Sender, &ts, &m.Sent, &m.Type, &m.Error,
	)
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			m.log.Error().Msgf("Database scan failed: %s", err)
		}

		return nil
	}
	if ts != 0 {
		m.Timestamp = time.Unix(ts, 0)
	}

	return m
}

func (m *Message) Insert(txn dbutil.Transaction) {
	query := `
		INSERT INTO message
			(chat_uid, chat_receiver, msg_id, mxid, sender, timestamp, sent, type, error)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`
	args := []interface{}{
		m.Chat.UID, m.Chat.Receiver, m.MsgID, m.MXID, m.Sender,
		m.Timestamp.Unix(), m.Sent, m.Type, m.Error,
	}

	var err error
	if txn != nil {
		_, err = txn.Exec(query, args...)
	} else {
		_, err = m.db.Exec(query, args...)
	}
	if err != nil {
		m.log.Warn().Msgf("Failed to insert %s %s: %v", m.Chat, m.MsgID, err)
	}
}

func (m *Message) UpdateMXID(txn dbutil.Transaction, mxid id.EventID, newType MessageType, newError MessageErrorType) {
	m.MXID = mxid
	m.Type = newType
	m.Error = newError

	query := `
		UPDATE message
		SET mxid=$1, type=$2, error=$3 WHERE chat_uid=$4 AND chat_receiver=$5 AND msg_id=$6
	`
	args := []interface{}{
		mxid, newType, newError, m.Chat.UID, m.Chat.Receiver, m.MsgID,
	}

	var err error
	if txn != nil {
		_, err = txn.Exec(query, args...)
	} else {
		_, err = m.db.Exec(query, args...)
	}
	if err != nil {
		m.log.Warn().Msgf("Failed to update %s %s: %v", m.Chat, m.MsgID, err)
	}
}

func (m *Message) Delete() {
	query := `
		DELETE FROM message
		WHERE chat_uid=$1 AND chat_receiver=$2 AND msg_id=$3
	`
	args := []interface{}{
		m.Chat.UID, m.Chat.Receiver, m.MsgID,
	}
	_, err := m.db.Exec(query, args...)
	if err != nil {
		m.log.Warn().Msgf("Failed to delete %s %s: %v", m.Chat, m.MsgID, err)
	}
}
