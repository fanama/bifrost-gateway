package domain

import (
	"context"
	"strings"
	"time"
)

type ChatConfig struct {
	ID               string    `json:"id"`
	ProjectID        string    `json:"project_id"`
	Name             string    `json:"name"`
	Provider         string    `json:"provider"`
	Model            string    `json:"model"`
	Tier             string    `json:"tier,omitempty"`
	BaseURL          string    `json:"base_url,omitempty"`
	APIKey           string    `json:"api_key,omitempty"`
	SystemPrompt     string    `json:"system_prompt,omitempty"`
	Temperature      *float64  `json:"temperature,omitempty"`
	TopP             *float64  `json:"top_p,omitempty"`
	MaxTokens        *int      `json:"max_tokens,omitempty"`
	FrequencyPenalty *float64  `json:"frequency_penalty,omitempty"`
	PresencePenalty  *float64  `json:"presence_penalty,omitempty"`
	ResponseFormat   string    `json:"response_format,omitempty"`
	Active           bool      `json:"active"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

// Tiers de routage, du plus ecconomique au plus puissant. Une configuration
// sans tier reste utilisable : elle sert de point de depart mais ne figure pas
// dans l'echelle de bascule.
const (
	TierFast      = "fast"
	TierBalanced  = "balanced"
	TierFrontier  = "frontier"
	RoutingTiers  = 3
	tierUnordered = -1
)

// tierRanks fixe l'ordre de l'echelle. Il est volontairement code en dur : le
// rang d'un tier est une decision de produit, pas une donnee de configuration.
var tierRanks = map[string]int{
	TierFast:     0,
	TierBalanced: 1,
	TierFrontier: 2,
}

// TierRank renvoie le rang d'un tier dans l'echelle de routage. Un tier inconnu
// ou absent renvoie tierUnordered : la configuration est alors ignoree par le
// routeur plutot que de le placer arbitrairement en bout de chaine.
func (c *ChatConfig) TierRank() int {
	if c == nil {
		return tierUnordered
	}
	return TierRankOf(c.Tier)
}

// TierRankOf expose le rang d'un tier a partir de son seul nom.
func TierRankOf(tier string) int {
	if rank, ok := tierRanks[strings.ToLower(strings.TrimSpace(tier))]; ok {
		return rank
	}
	return tierUnordered
}

func (c *ChatConfig) Validate() error {
	var missing []string
	if strings.TrimSpace(c.Name) == "" {
		missing = append(missing, "name")
	}
	if strings.TrimSpace(c.Provider) == "" {
		missing = append(missing, "provider")
	}
	if strings.TrimSpace(c.Model) == "" {
		missing = append(missing, "model")
	}
	if len(missing) > 0 {
		return &ValidationError{Fields: missing}
	}

	if c.TierRank() == tierUnordered && strings.TrimSpace(c.Tier) != "" {
		return &ValidationError{Fields: []string{"tier"}, Reason: "doit valoir fast, balanced, frontier ou etre vide"}
	}

	if c.Temperature != nil && (*c.Temperature < 0 || *c.Temperature > 2) {
		return &ValidationError{Fields: []string{"temperature"}, Reason: "doit etre compris entre 0 et 2"}
	}
	if c.TopP != nil && (*c.TopP <= 0 || *c.TopP > 1) {
		return &ValidationError{Fields: []string{"top_p"}, Reason: "doit etre compris entre 0 (exclus) et 1"}
	}
	if c.MaxTokens != nil && *c.MaxTokens < 1 {
		return &ValidationError{Fields: []string{"max_tokens"}, Reason: "doit etre superieur a 0"}
	}
	if c.FrequencyPenalty != nil && (*c.FrequencyPenalty < -2 || *c.FrequencyPenalty > 2) {
		return &ValidationError{Fields: []string{"frequency_penalty"}, Reason: "doit etre compris entre -2 et 2"}
	}
	if c.PresencePenalty != nil && (*c.PresencePenalty < -2 || *c.PresencePenalty > 2) {
		return &ValidationError{Fields: []string{"presence_penalty"}, Reason: "doit etre compris entre -2 et 2"}
	}
	switch c.ResponseFormat {
	case "", "text", "json_object":
	default:
		return &ValidationError{Fields: []string{"response_format"}, Reason: "valeur inconnue"}
	}
	return nil
}

func (c *ChatConfig) ResponseFormatMap() map[string]any {
	switch c.ResponseFormat {
	case "json_object":
		return map[string]any{"type": "json_object"}
	case "text":
		return map[string]any{"type": "text"}
	}
	return nil
}

// TierOptions liste les tiers proposes par l'interface, dans l'ordre de l'echelle.
func TierOptions() []string {
	return []string{TierFast, TierBalanced, TierFrontier}
}

type ChatConfigRepository interface {
	UpdatableRepository[ChatConfig]
	ListByProject(ctx context.Context, projectID string) ([]ChatConfig, error)
	GetActiveByProject(ctx context.Context, projectID string) (*ChatConfig, error)
	SetActive(ctx context.Context, projectID string, id string) error
}
