package infrastructure

import (
	"errors"

	"bridge-gateway/domain"
)

type FileProjectStore struct {
	*Adapter[domain.Project]
}

func NewFileProjectStore(path string) (*FileProjectStore, error) {
	inner, err := NewJSONStore[domain.Project](path)
	if err != nil {
		return nil, err
	}
	return &FileProjectStore{Adapter: NewAdapter(
		inner,
		func(p *domain.Project) string { return p.ID },
		domain.ErrProjectNotFound,
		func(item domain.Project, items []domain.Project) error {
			for i := range items {
				if items[i].ID == item.ID {
					return errors.New("project already exists")
				}
			}
			return nil
		},
	)}, nil
}

var _ domain.ProjectRepository = (*FileProjectStore)(nil)
