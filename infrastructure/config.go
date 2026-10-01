package infrastructure

import (
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type ModelConfig struct {
	ModelName     string         `yaml:"model_name"`
	BifrostParams map[string]any `yaml:"bifrost_params"`
}

type BifrostSettings struct {
	Callbacks                  []string `yaml:"callbacks"`
	EnableJSONSchemaValidation bool     `yaml:"enable_json_schema_validation"`
}

type GeneralSettings struct {
	MasterKey               string `yaml:"master_key"`
	StorePromptsInSpendLogs bool   `yaml:"store_prompts_in_spend_logs"`
}

// CacheSettings pilote le cache local en memoire qui remplace Redis.
// Une duree nulle desactive le cache correspondant.
type CacheSettings struct {
	// Enabled active le cache des lectures de repositories.
	Enabled bool `yaml:"enabled"`
	// TTL est la duree de vie d'une entree (format duration Go, ex "15s").
	TTL string `yaml:"ttl"`
	// MaxEntries borne la memoire du cache. Zero signifie illimite.
	MaxEntries int `yaml:"max_entries"`
}

// LLMResponseCacheSettings pilote le cache des reponses provider.
type LLMResponseCacheSettings struct {
	// Enabled active la memorisation des reponses LLM identiques.
	Enabled bool `yaml:"enabled"`
	// TTL est la duree de vie d'une reponse mise en cache.
	TTL string `yaml:"ttl"`
	// MaxEntries borne le nombre de reponses memorisees. Zero signifie illimite.
	MaxEntries int `yaml:"max_entries"`
}

// CacheSettings regroupe les deux caches pour eviter la repetition dans le YAML.
type CacheSettingsGroup struct {
	Stores      CacheSettings            `yaml:"stores"`
	LLMResponse LLMResponseCacheSettings `yaml:"llm_responses"`
}

type GatewayConfig struct {
	ModelList       []ModelConfig      `yaml:"model_list"`
	GeneralSettings GeneralSettings    `yaml:"general_settings"`
	BifrostSettings BifrostSettings    `yaml:"bifrost_settings"`
	Cache           CacheSettingsGroup `yaml:"cache"`
}

// CacheTTL parse la duree configuree et retourne defaut si la valeur est absente
// ou invalide, pour qu'une erreur de saisie n'empeche pas le demarrage.
func (c CacheSettings) CacheTTL(def time.Duration) time.Duration {
	return parseDuration(c.TTL, def)
}

func (c LLMResponseCacheSettings) CacheTTL(def time.Duration) time.Duration {
	return parseDuration(c.TTL, def)
}

func parseDuration(value string, def time.Duration) time.Duration {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return def
	}
	parsed, err := time.ParseDuration(trimmed)
	if err != nil || parsed < 0 {
		return def
	}
	return parsed
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
