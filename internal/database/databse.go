package database

import (
	_ "embed"

	_ "github.com/lib/pq"
	_ "github.com/mattn/go-sqlite3"

	"github.com/duo/matrix-wechat/internal/database/upgrades"

	"go.mau.fi/util/dbutil"

	"github.com/rs/zerolog"
)

type Database struct {
	*dbutil.Database

	User    *UserQuery
	Portal  *PortalQuery
	Puppet  *PuppetQuery
	Message *MessageQuery
}

func New(baseDB *dbutil.Database, log zerolog.Logger) *Database {
	db := &Database{Database: baseDB}
	db.UpgradeTable = upgrades.Table
	db.User = &UserQuery{
		db:  db,
		log: log.With().Str("query", "User").Logger(),
	}
	db.Puppet = &PuppetQuery{
		db:  db,
		log: log.With().Str("query", "Puppet").Logger(),
	}
	db.Portal = &PortalQuery{
		db:  db,
		log: log.With().Str("query", "Portal").Logger(),
	}
	db.Message = &MessageQuery{
		db:  db,
		log: log.With().Str("query", "Message").Logger(),
	}
	return db
}

func strPtr(val string) *string {
	if len(val) > 0 {
		return &val
	}
	return nil
}
