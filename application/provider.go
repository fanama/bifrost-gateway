package application

import (
	"context"
	"strings"

	"bridge-gateway/domain"
)

type ProviderUseCase struct {
	EntityUseCase[domain.Provider, domain.ProviderRepository]
}

func NewProviderUseCase(providers domain.ProviderRepository) *ProviderUseCase {
	return &ProviderUseCase{EntityUseCase: NewEntityUseCase[domain.Provider, domain.ProviderRepository](providers)}
}

func (u *ProviderUseCase) Create(ctx context.Context, name, baseURL, apiKey string) (*domain.Provider, error) {
	p := &domain.Provider{
		Name:    strings.TrimSpace(name),
		BaseURL: strings.TrimSpace(baseURL),
		APIKey:  strings.TrimSpace(apiKey),
	}
	if err := p.Validate(); err != nil {
		return nil, err
	}
	p.ID = u.newID("prov")
	p.CreatedAt = u.now().UTC()
	p.UpdatedAt = p.CreatedAt
	if err := u.repo.Create(ctx, p); err != nil {
		return nil, err
	}
	return p, nil
}

func (u *ProviderUseCase) Update(ctx context.Context, id string, name, baseURL, apiKey string) (*domain.Provider, error) {
	current, err := u.repo.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	current.Name = strings.TrimSpace(name)
	current.BaseURL = strings.TrimSpace(baseURL)
	current.APIKey = strings.TrimSpace(apiKey)
	if err := current.Validate(); err != nil {
		return nil, err
	}
	current.UpdatedAt = u.now().UTC()
	if err := u.repo.Update(ctx, current); err != nil {
		return nil, err
	}
	return current, nil
}
