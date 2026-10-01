package infrastructure

import (
	"context"
	"database/sql"
	"errors"

	"bridge-gateway/domain"
)

// ProviderStore implemente domain.ProviderRepository sur SQLite.
type ProviderStore struct {
	*Adapter[domain.Provider]
}

// NewProviderStore charge le store sur db. Si la table est vide, seeds (defauts)
// sont inserees au premier demarrage.
func NewProviderStore(db *sql.DB, seeds []domain.Provider) (*ProviderStore, error) {
	if seeds == nil {
		seeds = []domain.Provider{}
	}
	inner := NewSQLStore[domain.Provider](db, TableProviders, func(p *domain.Provider) string { return p.ID })
	store := &ProviderStore{Adapter: NewAdapter(
		inner,
		func(p *domain.Provider) string { return p.ID },
		domain.ErrProviderNotFound,
		func(item domain.Provider, items []domain.Provider) error {
			for i := range items {
				if items[i].ID == item.ID {
					return errors.New("provider already exists")
				}
			}
			return nil
		},
	)}
	if err := inner.Seed(context.Background(), func() []domain.Provider { return seeds }); err != nil {
		return nil, err
	}
	return store, nil
}

// ListByProvider filtres le catalogue par provider pour le datalist de la
// modale de configuration.
func (s *ProviderStore) ListByName(ctx context.Context, name string) ([]domain.Provider, error) {
	return s.Store().WhereSQL(ctx, `json_extract(payload, '$.name') = ?`, name)
}

var _ domain.ProviderRepository = (*ProviderStore)(nil)
