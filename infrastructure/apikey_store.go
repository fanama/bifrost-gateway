package infrastructure

import (
	"context"

	"bridge-gateway/domain"
)

type FileAPIKeyStore struct {
	*Adapter[domain.APIKey]
}

func NewFileAPIKeyStore(path string) (*FileAPIKeyStore, error) {
	inner, err := NewJSONStore[domain.APIKey](path)
	if err != nil {
		return nil, err
	}
	return &FileAPIKeyStore{Adapter: NewAdapter(
		inner,
		func(k *domain.APIKey) string { return k.ID },
		domain.ErrAPIKeyNotFound,
		nil,
	)}, nil
}

func (s *FileAPIKeyStore) ListByProject(_ context.Context, projectID string) ([]domain.APIKey, error) {
	return s.store.Where(func(k domain.APIKey) bool { return k.ProjectID == projectID }), nil
}

func (s *FileAPIKeyStore) FindByDigest(_ context.Context, digest string) (*domain.APIKey, error) {
	if digest == "" {
		return nil, domain.ErrAPIKeyNotFound
	}
	k, ok := s.store.FindWhere(func(key domain.APIKey) bool { return key.Digest == digest })
	if !ok {
		return nil, domain.ErrAPIKeyNotFound
	}
	return k, nil
}

var _ domain.APIKeyRepository = (*FileAPIKeyStore)(nil)
