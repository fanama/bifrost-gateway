package config

import (
	"context"
	"strings"
	"time"

	"bridge-gateway/domain/errs"
	"bridge-gateway/domain/shared"
)

type ChatConfig struct {
	ID           string   `json:"id"`
	ProjectID    string   `json:"project_id"`
	Name         string   `json:"name"`
	Provider     string   `json:"provider"`
	Model        string   `json:"model"`
	Tier         string   `json:"tier,omitempty"`
	BaseURL      string   `json:"base_url,omitempty"`
	APIKey       string   `json:"api_key,omitempty"`
	SystemPrompt string   `json:"system_prompt,omitempty"`
	Temperature  *float64 `json:"temperature,omitempty"`
	TopP         *float64 `json:"top_p,omitempty"`
	MaxTokens    *int     `json:"max_tokens,omitempty"`
	// ReasoningEffort regle le thinking des modeles de raisonnement : "none"
	// le desactive, les autres niveaux le dosent. Vide laisse le comportement
	// par defaut du provider, qui peut raisonner de lui-meme.
	ReasoningEffort  string   `json:"reasoning_effort,omitempty"`
	FrequencyPenalty *float64 `json:"frequency_penalty,omitempty"`
	PresencePenalty  *float64 `json:"presence_penalty,omitempty"`
	ResponseFormat   string   `json:"response_format,omitempty"`
	// ResponseSchema porte le JSON Schema impose au modele lorsque
	// ResponseFormat vaut "json_schema". Il est stocke en clair : c'est ce que le
	// provider attend, et il doit etre relu sans reinterpretation.
	ResponseSchema string    `json:"response_schema,omitempty"`
	Active         bool      `json:"active"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

func (c *ChatConfig) Validate() error {
	if err := c.validateIdentity(); err != nil {
		return err
	}
	if err := c.validateSampling(); err != nil {
		return err
	}
	return c.validateResponseFormat()
}

// validateIdentity checks the required naming fields and the optional routing
// tier.
func (c *ChatConfig) validateIdentity() error {
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
		return &errs.ValidationError{Fields: missing}
	}
	if c.TierRank() == tierUnordered && strings.TrimSpace(c.Tier) != "" {
		return &errs.ValidationError{Fields: []string{"tier"}, Reason: "doit valoir fast, balanced, frontier ou etre vide"}
	}
	return nil
}

// validateSampling checks the optional generation controls. Each pointer must
// distinguish "absent" from an explicit zero, hence the nil guards.
func (c *ChatConfig) validateSampling() error {
	if c.Temperature != nil && (*c.Temperature < 0 || *c.Temperature > 2) {
		return &errs.ValidationError{Fields: []string{"temperature"}, Reason: "doit etre compris entre 0 et 2"}
	}
	if c.TopP != nil && (*c.TopP <= 0 || *c.TopP > 1) {
		return &errs.ValidationError{Fields: []string{"top_p"}, Reason: "doit etre compris entre 0 (exclus) et 1"}
	}
	if c.MaxTokens != nil && *c.MaxTokens < 1 {
		return &errs.ValidationError{Fields: []string{"max_tokens"}, Reason: "doit etre superieur a 0"}
	}
	if !ValidReasoningEffort(c.ReasoningEffort) {
		return &errs.ValidationError{Fields: []string{"reasoning_effort"}, Reason: "doit valoir none, minimal, low, medium, high ou etre vide"}
	}
	if c.FrequencyPenalty != nil && (*c.FrequencyPenalty < -2 || *c.FrequencyPenalty > 2) {
		return &errs.ValidationError{Fields: []string{"frequency_penalty"}, Reason: "doit etre compris entre -2 et 2"}
	}
	if c.PresencePenalty != nil && (*c.PresencePenalty < -2 || *c.PresencePenalty > 2) {
		return &errs.ValidationError{Fields: []string{"presence_penalty"}, Reason: "doit etre compris entre -2 et 2"}
	}
	return nil
}

func (c *ChatConfig) validateResponseFormat() error {
	switch c.EffectiveResponseFormat() {
	case "", ResponseFormatText, ResponseFormatJSONObject:
		return nil
	case ResponseFormatJSONSchema:
		if _, err := c.ResponseSchemaObject(); err != nil {
			return &errs.ValidationError{Fields: []string{"response_schema"}, Reason: err.Error()}
		}
		return nil
	default:
		return &errs.ValidationError{Fields: []string{"response_format"}, Reason: "valeur inconnue"}
	}
}

// ReasoningEfforts liste les valeurs acceptees pour le thinking, du desactive
// au plus profond. La valeur vide laisse le provider appliquer son defaut.
var ReasoningEfforts = []string{"none", "minimal", "low", "medium", "high"}

// ValidReasoningEffort indique si une valeur peut etre transmise au provider.
func ValidReasoningEffort(effort string) bool {
	if effort == "" {
		return true
	}
	for _, allowed := range ReasoningEfforts {
		if effort == allowed {
			return true
		}
	}
	return false
}

type ChatConfigRepository interface {
	shared.UpdatableRepository[ChatConfig]
	ListByProject(ctx context.Context, projectID string) ([]ChatConfig, error)
	GetActiveByProject(ctx context.Context, projectID string) (*ChatConfig, error)
	SetActive(ctx context.Context, projectID string, id string) error
}
