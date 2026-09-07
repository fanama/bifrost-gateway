package infrastructure

import (
	"context"
	"errors"

	"bridge-gateway/domain"
)

type FileConfigStore struct {
	*Adapter[domain.ChatConfig]
}

func NewFileConfigStore(path string) (*FileConfigStore, error) {
	inner, err := NewJSONStore[domain.ChatConfig](path)
	if err != nil {
		return nil, err
	}
	return &FileConfigStore{Adapter: NewAdapter(
		inner,
		func(c *domain.ChatConfig) string { return c.ID },
		domain.ErrConfigNotFound,
		nil,
	)}, nil
}

func (s *FileConfigStore) ListByProject(_ context.Context, projectID string) ([]domain.ChatConfig, error) {
	return s.store.Where(func(c domain.ChatConfig) bool { return c.ProjectID == projectID }), nil
}

func (s *FileConfigStore) GetActiveByProject(_ context.Context, projectID string) (*domain.ChatConfig, error) {
	cfg, ok := s.store.FindWhere(func(c domain.ChatConfig) bool { return c.ProjectID == projectID && c.Active })
	if !ok {
		return nil, domain.ErrNoActiveConfig
	}
	return cfg, nil
}

func (s *FileConfigStore) SetActive(_ context.Context, projectID string, id string) error {
	return s.store.Mutate(func(configs *[]domain.ChatConfig) error {
		found := false
		for i := range *configs {
			if (*configs)[i].ProjectID != projectID {
				continue
			}
			(*configs)[i].Active = (*configs)[i].ID == id
			if (*configs)[i].ID == id {
				found = true
			}
		}
		if !found {
			return domain.ErrConfigNotFound
		}
		return nil
	})
}

var _ domain.ChatConfigRepository = (*FileConfigStore)(nil)

func IsNotFound(err error) bool {
	return errors.Is(err, domain.ErrConfigNotFound)
}
