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

// ClassifierSettings decrit le petit modele local qui attribue un tier a une
// conversation. Il est volontairement separe de model_list : le classifieur doit
// rester le modele le moins cher du lot, et le melanger aux modeles servis
// rendrait son role opaque.
type ClassifierSettings struct {
	Provider string `yaml:"provider"`
	Model    string `yaml:"model"`
	BaseURL  string `yaml:"base_url"`
	APIKey   string `yaml:"api_key"`
	// MaxInputChars borne l'historique envoye au classifieur, pour qu'une
	// conversation tres longue ne rende pas la classification plus couteuse que
	// la reponse qu'elle cherche a optimiser.
	MaxInputChars int `yaml:"max_input_chars"`
	// MaxTokens borne la sortie du classifieur, qui ne doit produire qu'un
	// petit objet JSON.
	MaxTokens int `yaml:"max_tokens"`
	// Timeout borne l'appel de classification. Au-dela, la precision du
	// routage est sacrifiee et la configuration active est conservee, ce qui
	// vaut mieux que de perdre la requete entiere sur un classifieur lent.
	Timeout Duration `yaml:"timeout"`
}

// Duration est une duree YAML qui accepte la notation Go ("10s", "500ms").
type Duration time.Duration

func (d Duration) Or(def time.Duration) time.Duration {
	if d <= 0 {
		return def
	}
	return time.Duration(d)
}

func (d *Duration) UnmarshalYAML(value *yaml.Node) error {
	var raw string
	if err := value.Decode(&raw); err != nil {
		return err
	}
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil
	}
	parsed, err := time.ParseDuration(trimmed)
	if err != nil || parsed < 0 {
		return nil
	}
	*d = Duration(parsed)
	return nil
}

// RoutingSettings pilote le routage par tier et la bascule sur saturation.
type RoutingSettings struct {
	// Enabled active le decorateur. Desactive, les requetes utilisent toujours
	// la configuration active du projet.
	Enabled bool `yaml:"enabled"`
	// MinConfidence est le seuil en dessous duquel le verdict du classifieur est
	// rejete et la configuration active conservee. Une confiance basse traduit
	// un cas limite, ou mieux vaut payer le modele par defaut que deviner.
	MinConfidence float64 `yaml:"min_confidence"`
	// CooldownDuring marque, apres un echec, la duree pendant laquelle le tier
	// est ecarte de l'echelle. Evite de reessayer un modele mort a chaque
	// requete.
	CooldownDuring string `yaml:"cooldown"`
	// MaxAttempts borne le nombre de modeles testes. Zero signifie "toute
	// l'echelle".
	MaxAttempts int `yaml:"max_attempts"`
	// MaxRetries remplace le budget de reessais interne de Bifrost, active
	// seulement quand le routage l'est aussi : les deux se doubleraient.
	MaxRetries int                `yaml:"max_retries"`
	Classifier ClassifierSettings `yaml:"classifier"`
}

// CooldownTTL parse la duree de refroidissement et retourne defaut si la valeur
// est absente ou invalide.
func (r RoutingSettings) CooldownTTL(def time.Duration) time.Duration {
	return parseDuration(r.CooldownDuring, def)
}

// NormalizedMinConfidence borne la confiance dans [0,1] : une valeur hors
// bornes en YAML ne doit ni invalider le routage ni le rendre toujours actif.
func (r RoutingSettings) NormalizedMinConfidence(def float64) float64 {
	if r.MinConfidence < 0 || r.MinConfidence > 1 {
		return def
	}
	return r.MinConfidence
}

type GatewayConfig struct {
	ModelList       []ModelConfig      `yaml:"model_list"`
	GeneralSettings GeneralSettings    `yaml:"general_settings"`
	BifrostSettings BifrostSettings    `yaml:"bifrost_settings"`
	Cache           CacheSettingsGroup `yaml:"cache"`
	Routing         RoutingSettings    `yaml:"routing"`
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
