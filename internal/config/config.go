package config

import (
	"maunium.net/go/mautrix/bridge/bridgeconfig"
	"maunium.net/go/mautrix/id"
)

type Config struct {
	*bridgeconfig.BaseConfig `yaml:",inline"`

	Bridge BridgeConfig `yaml:"bridge"`
}

func (c *Config) CanAutoDoublePuppet(userID id.UserID) bool {
	_, homeserver, _ := userID.Parse()
	_, hasSecret := c.Bridge.DoublePuppetConfig.SharedSecretMap[homeserver]

	return hasSecret
}
