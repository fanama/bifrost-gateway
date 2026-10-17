package project

import (
	"bridge-gateway/domain/errs"
	"bridge-gateway/domain/shared"

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
		return &errs.ValidationError{Fields: []string{"name"}}
	}
	return nil
}

// ProjectRepository est le port de persistance des projets.
type ProjectRepository = shared.UpdatableRepository[Project]
