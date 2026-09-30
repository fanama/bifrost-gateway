package domain

import (
	"strings"
	"time"
)

// Provider decrit un fournisseur LLM gerable depuis l'UI. La BaseURL et l'APIKey
// servent d'autofill ou de valeur par defaut lors de l'execution d'une configuration.
type Provider struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	BaseURL   string    `json:"base_url,omitempty"`
	APIKey    string    `json:"api_key,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (p *Provider) Validate() error {
	if strings.TrimSpace(p.Name) == "" {
		return &ValidationError{Fields: []string{"name"}}
	}
	return nil
}

func (p *Provider) MaskedKey() string {
	k := strings.TrimSpace(p.APIKey)
	if k == "" {
		return ""
	}
	if len(k) <= 8 {
		return "••••••••"
	}
	return k[:4] + "••••" + k[len(k)-4:]
}

// ProviderRepository est le port de persistance des providers.
type ProviderRepository = UpdatableRepository[Provider]
