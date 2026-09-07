package domain

import (
	"strings"
	"time"
)

type Project struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func (p *Project) Validate() error {
	if strings.TrimSpace(p.Name) == "" {
		return &ValidationError{Fields: []string{"name"}}
	}
	return nil
}

// ProjectRepository est le port de persistance des projets.
type ProjectRepository = UpdatableRepository[Project]
