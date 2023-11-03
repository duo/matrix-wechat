package main

import (
	_ "embed"

	"github.com/duo/matrix-wechat/internal"
	"github.com/duo/matrix-wechat/internal/config"

	"go.mau.fi/util/configupgrade"
	"maunium.net/go/mautrix/bridge"
)

// Information to find out exactly which commit the bridge was built from.
// These are filled at build time with the -X linker flag.
var (
	Tag       = "unknown"
	Commit    = "unknown"
	BuildTime = "unknown"
)

//go:embed example-config.yaml
var exampleConfig string

func main() {
	br := internal.NewWechatBridge(exampleConfig)
	br.Bridge = bridge.Bridge{
		Name:         "matrix-wechat",
		URL:          "https://github.com/duo/matrix-wechat",
		Description:  "A Matrix-WeChat puppeting bridge.",
		Version:      "0.2.1",
		ProtocolName: "Wechat",

		CryptoPickleKey: "github.com/duo/matrix-wechat",

		ConfigUpgrader: &configupgrade.StructUpgrader{
			SimpleUpgrader: configupgrade.SimpleUpgrader(config.DoUpgrade),
			Blocks:         config.SpacedBlocks,
			Base:           exampleConfig,
		},

		Child: br,
	}
	br.InitVersion(Tag, Commit, BuildTime)

	br.Main()
}
