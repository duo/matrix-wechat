package database

import (
	"github.com/rs/zerolog"
	"maunium.net/go/mautrix/id"
)

type MessageQuery struct {
	db  *Database
	log zerolog.Logger
}

func (mq *MessageQuery) New() *Message {
	return &Message{
		db:  mq.db,
		log: mq.log,
	}
}

const (
	getAllMessagesQuery = `
		SELECT chat_uid, chat_receiver, msg_id, mxid, sender, timestamp, sent, type, error
		FROM message
		WHERE chat_uid=$1 AND chat_receiver=$2
	`
	getMessageByMsgIDQuery = `
		SELECT chat_uid, chat_receiver, msg_id, mxid, sender, timestamp, sent, type, error
		FROM message
		WHERE chat_uid=$1 AND chat_receiver=$2 AND msg_id=$3
	`
	getMessageByMXIDQuery = `
		SELECT chat_uid, chat_receiver, msg_id, mxid, sender, timestamp, sent, type, error
		FROM message
		WHERE mxid=$1
	`
)

func (mq *MessageQuery) GetAll(chat PortalKey) []*Message {
	messages := []*Message{}

	rows, err := mq.db.Query(getAllMessagesQuery, chat.UID, chat.Receiver)
	if err != nil || rows == nil {
		return messages
	}
	for rows.Next() {
		messages = append(messages, mq.New().Scan(rows))
	}

	return messages
}

func (mq *MessageQuery) GetByMsgID(chat PortalKey, msgID string) *Message {
	row := mq.db.QueryRow(getMessageByMsgIDQuery, chat.UID, chat.Receiver, msgID)
	if row == nil {
		return nil
	}

	return mq.New().Scan(row)
}

func (mq *MessageQuery) GetByMXID(mxid id.EventID) *Message {
	row := mq.db.QueryRow(getMessageByMXIDQuery, mxid)
	if row == nil {
		return nil
	}

	return mq.New().Scan(row)
}
