package infrastructure

import (
	"context"
	"database/sql"

	"bridge-gateway/domain"
)

// ModelCatalogStore implemente domain.ModelCatalogRepository sur SQLite.
type ModelCatalogStore struct {
	*Adapter[domain.CatalogModel]
}

func NewModelCatalogStore(db *sql.DB, seeds []domain.CatalogModel) (*ModelCatalogStore, error) {
	if seeds == nil {
		seeds = []domain.CatalogModel{}
	}
	inner := NewSQLStore[domain.CatalogModel](db, TableCatalogModel, func(m *domain.CatalogModel) string { return m.ID })
	store := &ModelCatalogStore{Adapter: NewAdapter(
		inner,
		func(m *domain.CatalogModel) string { return m.ID },
		domain.ErrCatalogModelNotFound,
		nil,
	)}
	if err := inner.Seed(context.Background(), func() []domain.CatalogModel { return seeds }); err != nil {
		return nil, err
	}
	return store, nil
}

// ListByProvider alimente la route /projects/{id}/configs/models, qui rend le
// datalist filtre par provider pour le champ modele de la modale.
func (s *ModelCatalogStore) ListByProvider(ctx context.Context, provider string) ([]domain.CatalogModel, error) {
	return s.Store().WhereSQL(ctx, `json_extract(payload, '$.provider') = ?`, provider)
}

var _ domain.ModelCatalogRepository = (*ModelCatalogStore)(nil)
