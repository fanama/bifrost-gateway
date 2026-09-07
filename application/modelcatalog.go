package application

import (
	"context"
	"strings"

	"bridge-gateway/domain"
)

type ModelCatalogUseCase struct {
	EntityUseCase[domain.CatalogModel, domain.ModelCatalogRepository]
}

func NewModelCatalogUseCase(catalog domain.ModelCatalogRepository) *ModelCatalogUseCase {
	return &ModelCatalogUseCase{EntityUseCase: NewEntityUseCase[domain.CatalogModel, domain.ModelCatalogRepository](catalog)}
}

func (u *ModelCatalogUseCase) Create(ctx context.Context, name, provider string) (*domain.CatalogModel, error) {
	m := &domain.CatalogModel{
		Name:     strings.TrimSpace(name),
		Provider: strings.ToLower(strings.TrimSpace(provider)),
	}
	if err := m.Validate(); err != nil {
		return nil, err
	}
	existing, err := u.repo.List(ctx)
	if err != nil {
		return nil, err
	}
	for _, e := range existing {
		if strings.EqualFold(e.Name, m.Name) && strings.EqualFold(e.Provider, m.Provider) {
			return &e, nil
		}
	}
	m.ID = u.newID("model")
	m.CreatedAt = u.now().UTC()
	if err := u.repo.Create(ctx, m); err != nil {
		return nil, err
	}
	return m, nil
}
