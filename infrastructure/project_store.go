package infrastructure

import (
	"database/sql"
	"errors"

	"bridge-gateway/domain"
)

// ProjectStore implemente domain.ProjectRepository sur SQLite.
type ProjectStore struct {
	*Adapter[domain.Project]
}

func NewProjectStore(db *sql.DB) *ProjectStore {
	inner := NewSQLStore[domain.Project](db, TableProjects, func(p *domain.Project) string { return p.ID })
	return &ProjectStore{Adapter: NewAdapter(
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
	)}
}

var _ domain.ProjectRepository = (*ProjectStore)(nil)
