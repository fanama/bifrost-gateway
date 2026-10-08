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

// Create ajoute un modele au catalogue. kind vaut ModelKindChat ou
// ModelKindEmbedding (une valeur vide est normalisee en chat). Reajouter un
// modele deja present est idempotent ; si le kind force differe de celui
// stocke, il est corrige sur place, pour qu'un premier ajout mal type puisse
// etre repasse au bon type sans suppression manuelle.
func (u *ModelCatalogUseCase) Create(ctx context.Context, name, provider, kind string) (*domain.CatalogModel, error) {
	m := &domain.CatalogModel{
		Name:     strings.TrimSpace(name),
		Provider: strings.ToLower(strings.TrimSpace(provider)),
		Kind:     domain.NormalizeModelKind(kind),
	}
	if err := m.Validate(); err != nil {
		return nil, err
	}
	existing, err := u.repo.List(ctx)
	if err != nil {
		return nil, err
	}
	for i := range existing {
		if strings.EqualFold(existing[i].Name, m.Name) && strings.EqualFold(existing[i].Provider, m.Provider) {
			e := existing[i]
			if strings.TrimSpace(kind) != "" && e.Kind != m.Kind {
				e.Kind = m.Kind
				if err := u.repo.Update(ctx, &e); err != nil {
					return nil, err
				}
				return &e, nil
			}
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
