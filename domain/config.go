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
	BaseURL          string    `json:"base_url,omitempty"`
	APIKey           string    `json:"api_key,omitempty"`
	SystemPrompt     string    `json:"system_prompt,omitempty"`
	CostCenter       string    `json:"cost_center"`
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
	if strings.TrimSpace(c.CostCenter) == "" {
		missing = append(missing, "cost_center")
	}
	if len(missing) > 0 {
		return &ValidationError{Fields: missing}
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

type ChatConfigRepository interface {
	UpdatableRepository[ChatConfig]
	ListByProject(ctx context.Context, projectID string) ([]ChatConfig, error)
	GetActiveByProject(ctx context.Context, projectID string) (*ChatConfig, error)
	SetActive(ctx context.Context, projectID string, id string) error
}
