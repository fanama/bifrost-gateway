package domain

import (
	"strings"
	"time"
)

// Provider decrit un fournisseur LLM gerable depuis l'UI. La BaseURL sert
// d'autofill lors de la creation d'une configuration.
type Provider struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	BaseURL   string    `json:"base_url"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (p *Provider) Validate() error {
	if strings.TrimSpace(p.Name) == "" {
		return &ValidationError{Fields: []string{"name"}}
	}
	return nil
}

// ProviderRepository est le port de persistance des providers.
type ProviderRepository = UpdatableRepository[Provider]
