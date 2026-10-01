package infrastructure

import (
	"context"
	"database/sql"

	"bridge-gateway/domain"
)

// APIKeyStore implemente domain.APIKeyRepository sur SQLite.
type APIKeyStore struct {
	*Adapter[domain.APIKey]
}

func NewAPIKeyStore(db *sql.DB) *APIKeyStore {
	inner := NewSQLStore[domain.APIKey](db, TableAPIKeys, func(k *domain.APIKey) string { return k.ID })
	return &APIKeyStore{Adapter: NewAdapter(
		inner,
		func(k *domain.APIKey) string { return k.ID },
		domain.ErrAPIKeyNotFound,
		nil,
	)}
}

func (s *APIKeyStore) ListByProject(ctx context.Context, projectID string) ([]domain.APIKey, error) {
	return s.Store().WhereSQL(ctx, `json_extract(payload, '$.project_id') = ?`, projectID)
}

// FindByDigest est le chemin le plus chaud du gateway : chaque requete
// /v1/chat/completions resout sa cle via cette methode. Le predicat est pousse
// jusqu'a l'index json_extract(payload, '$.digest').
func (s *APIKeyStore) FindByDigest(ctx context.Context, digest string) (*domain.APIKey, error) {
	if digest == "" {
		return nil, domain.ErrAPIKeyNotFound
	}
	item, ok, err := s.Store().FindBySQL(ctx, `json_extract(payload, '$.digest') = ?`, digest)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, domain.ErrAPIKeyNotFound
	}
	return item, nil
}

var _ domain.APIKeyRepository = (*APIKeyStore)(nil)
