package application

import (
	"context"
	"strings"

	"bridge-gateway/domain"
)

type ProjectUseCase struct {
	EntityUseCase[domain.Project, domain.ProjectRepository]
}

func NewProjectUseCase(projects domain.ProjectRepository) *ProjectUseCase {
	return &ProjectUseCase{EntityUseCase: NewEntityUseCase[domain.Project, domain.ProjectRepository](projects)}
}

func (u *ProjectUseCase) Create(ctx context.Context, name, description string) (*domain.Project, error) {
	p := &domain.Project{
		Name:        strings.TrimSpace(name),
		Description: strings.TrimSpace(description),
	}
	if err := p.Validate(); err != nil {
		return nil, err
	}
	p.ID = u.newID("proj")
	p.CreatedAt = u.now().UTC()
	p.UpdatedAt = p.CreatedAt
	if err := u.repo.Create(ctx, p); err != nil {
		return nil, err
	}
	return p, nil
}

func (u *ProjectUseCase) CreateWithID(ctx context.Context, id, name, description string) (*domain.Project, error) {
	p := &domain.Project{
		Name:        strings.TrimSpace(name),
		Description: strings.TrimSpace(description),
	}
	if err := p.Validate(); err != nil {
		return nil, err
	}
	p.ID = id
	p.CreatedAt = u.now().UTC()
	p.UpdatedAt = p.CreatedAt
	if err := u.repo.Create(ctx, p); err != nil {
		return nil, err
	}
	return p, nil
}
