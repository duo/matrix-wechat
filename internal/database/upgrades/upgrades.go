package upgrades

import (
	"embed"

	"maunium.net/go/mautrix/util/dbutil"
)

var Table dbutil.UpgradeTable

//go:embed *.sql
var rawUpgrades embed.FS

func init() {
	Table.RegisterFS(rawUpgrades)
}
