package catalog

import (
	"context"
	"strings"

	"bridge-gateway/application/entity"
	"bridge-gateway/domain"
)

type ModelCatalogUseCase struct {
	entity.EntityUseCase[domain.CatalogModel, domain.ModelCatalogRepository]
}

func NewModelCatalogUseCase(catalog domain.ModelCatalogRepository) *ModelCatalogUseCase {
	return &ModelCatalogUseCase{EntityUseCase: entity.NewEntityUseCase[domain.CatalogModel, domain.ModelCatalogRepository](catalog)}
}

// Create ajoute un modele au catalogue. kind vaut ModelKindChat ou
// ModelKindEmbedding (une valeur vide est normalisee en chat). Reajouter un
// modele deja present est idempotent ; si le kind force differe de celui
// stocke, il est corrige sur place, pour qu'un premier ajout mal type puisse
// etre repasse au bon type sans suppression manuelle.
func (u *ModelCatalogUseCase) Create(ctx context.Context, name, provider, kind string) (*domain.CatalogModel, error) {
	// The body below stays linear: reconciliation of an existing entry is
	// delegated to reconcileKind.
	m := &domain.CatalogModel{
		Name:     strings.TrimSpace(name),
		Provider: strings.ToLower(strings.TrimSpace(provider)),
		Kind:     domain.NormalizeModelKind(kind),
	}
	if err := m.Validate(); err != nil {
		return nil, err
	}
	existing, err := u.Repo.List(ctx)
	if err != nil {
		return nil, err
	}
	for i := range existing {
		if strings.EqualFold(existing[i].Name, m.Name) && strings.EqualFold(existing[i].Provider, m.Provider) {
			return u.reconcileKind(ctx, existing[i], m.Kind, kind)
		}
	}
	m.ID = u.NewID("model")
	m.CreatedAt = u.Now().UTC()
	if err := u.Repo.Create(ctx, m); err != nil {
		return nil, err
	}
	return m, nil
}

// reconcileKind returns an existing entry, correcting its kind only when the
// caller explicitly forced one that differs.
func (u *ModelCatalogUseCase) reconcileKind(ctx context.Context, entry domain.CatalogModel, normalized, forced string) (*domain.CatalogModel, error) {
	if strings.TrimSpace(forced) == "" || entry.Kind == normalized {
		return &entry, nil
	}
	entry.Kind = normalized
	if err := u.Repo.Update(ctx, &entry); err != nil {
		return nil, err
	}
	return &entry, nil
}
