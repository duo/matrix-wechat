package database

import (
	"fmt"

	"github.com/duo/matrix-wechat/internal/types"

	"maunium.net/go/mautrix/id"

	log "maunium.net/go/maulogger/v2"
)

const puppetColumns = `
	uin, avatar, avatar_url, displayname, name_quality, name_set, avatar_set,
	last_sync, custom_mxid, access_token, next_batch, enable_presence
`

type PuppetQuery struct {
	db  *Database
	log log.Logger
}

func (pq *PuppetQuery) New() *Puppet {
	return &Puppet{
		db:  pq.db,
		log: pq.log,

		EnablePresence: true,
	}
}

func (pq *PuppetQuery) GetAll() []*Puppet {
	puppets := []*Puppet{}

	query := fmt.Sprintf("SELECT %s FROM puppet", puppetColumns)

	rows, err := pq.db.Query(query)
	if err != nil || rows == nil {
		return puppets
	}

	defer rows.Close()
	for rows.Next() {
		puppets = append(puppets, pq.New().Scan(rows))
	}

	return puppets
}

func (pq *PuppetQuery) Get(uid types.UID) *Puppet {
	query := fmt.Sprintf("SELECT %s FROM puppet WHERE uin=$1", puppetColumns)
	args := []interface{}{uid.Uin}

	row := pq.db.QueryRow(query, args...)
	if row == nil {
		return nil
	}

	return pq.New().Scan(row)
}

func (pq *PuppetQuery) GetByCustomMXID(mxid id.UserID) *Puppet {
	query := fmt.Sprintf("SELECT %s FROM puppet WHERE custom_mxid=$1", puppetColumns)
	args := []interface{}{mxid}

	row := pq.db.QueryRow(query, args...)
	if row == nil {
		return nil
	}

	return pq.New().Scan(row)
}

func (pq *PuppetQuery) GetAllWithCustomMXID() []*Puppet {
	puppets := []*Puppet{}

	query := fmt.Sprintf("SELECT %s FROM puppet WHERE custom_mxid<>''", puppetColumns)

	rows, err := pq.db.Query(query)
	if err != nil || rows == nil {
		return puppets
	}

	defer rows.Close()
	for rows.Next() {
		puppets = append(puppets, pq.New().Scan(rows))
	}

	return puppets
}
