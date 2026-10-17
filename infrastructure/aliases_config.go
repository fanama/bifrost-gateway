package infrastructure

import "bridge-gateway/infrastructure/config"

type (
	ModelConfig              = config.ModelConfig
	BifrostSettings          = config.BifrostSettings
	GeneralSettings          = config.GeneralSettings
	CacheSettings            = config.CacheSettings
	LLMResponseCacheSettings = config.LLMResponseCacheSettings
	CacheSettingsGroup       = config.CacheSettingsGroup
	ClassifierSettings       = config.ClassifierSettings
	RoutingSettings          = config.RoutingSettings
	GatewayConfig            = config.GatewayConfig
	Duration                 = config.Duration
)

const DefaultPort = config.DefaultPort

func LoadConfig(path string) (*GatewayConfig, error) { return config.LoadConfig(path) }

func ResolvePort(flagPort int, flagSet bool, lookupEnv func(string) (string, bool), readFile func(string) ([]byte, error)) int {
	return config.ResolvePort(flagPort, flagSet, lookupEnv, readFile)
}

func PortFromValue(raw string) (int, bool) { return config.PortFromValue(raw) }

func PortFromDotEnv(data []byte) (int, bool) { return config.PortFromDotEnv(data) }
