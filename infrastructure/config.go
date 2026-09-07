package infrastructure

import (
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

type ModelConfig struct {
	ModelName     string         `yaml:"model_name"`
	BifrostParams map[string]any `yaml:"bifrost_params"`
}

type CacheParams struct {
	Type      string `yaml:"type"`
	Namespace string `yaml:"namespace"`
	Host      string `yaml:"host"`
	Port      string `yaml:"port"`
}

type BifrostSettings struct {
	Callbacks                  []string    `yaml:"callbacks"`
	Cache                      bool        `yaml:"cache"`
	CacheParams                CacheParams `yaml:"cache_params"`
	EnableJSONSchemaValidation bool        `yaml:"enable_json_schema_validation"`
}

type RouterSettings struct {
	RedisHost string `yaml:"redis_host"`
	RedisPort string `yaml:"redis_port"`
}

type GeneralSettings struct {
	MasterKey               string `yaml:"master_key"`
	DatabaseURL             string `yaml:"database_url"`
	StoreModelInDB          bool   `yaml:"store_model_in_db"`
	StorePromptsInSpendLogs bool   `yaml:"store_prompts_in_spend_logs"`
}

type GatewayConfig struct {
	ModelList       []ModelConfig   `yaml:"model_list"`
	GeneralSettings GeneralSettings `yaml:"general_settings"`
	BifrostSettings BifrostSettings `yaml:"bifrost_settings"`
	RouterSettings  RouterSettings  `yaml:"router_settings"`
}

func LoadConfig(path string) (*GatewayConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	data = []byte(expandEnvVars(string(data)))

	var cfg GatewayConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func expandEnvVars(s string) string {
	for {
		start := strings.Index(s, "${")
		if start == -1 {
			break
		}
		end := strings.Index(s[start:], "}")
		if end == -1 {
			break
		}
		end += start
		varName := s[start+2 : end]
		defaultVal := ""
		if idx := strings.Index(varName, ":-"); idx != -1 {
			defaultVal = varName[idx+2:]
			varName = varName[:idx]
		}
		val := os.Getenv(varName)
		if val == "" {
			val = defaultVal
		}
		s = s[:start] + val + s[end+1:]
	}
	return s
}
