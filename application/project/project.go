package project

import (
	"context"
	"strings"

	"bridge-gateway/application/entity"
	"bridge-gateway/domain"
)

type ProjectUseCase struct {
	entity.EntityUseCase[domain.Project, domain.ProjectRepository]
}

func NewProjectUseCase(projects domain.ProjectRepository) *ProjectUseCase {
	return &ProjectUseCase{EntityUseCase: entity.NewEntityUseCase[domain.Project, domain.ProjectRepository](projects)}
}

func (u *ProjectUseCase) Create(ctx context.Context, name, description string) (*domain.Project, error) {
	p := &domain.Project{
		Name:        strings.TrimSpace(name),
		Description: strings.TrimSpace(description),
	}
	if err := p.Validate(); err != nil {
		return nil, err
	}
	p.ID = u.NewID("proj")
	p.CreatedAt = u.Now().UTC()
	p.UpdatedAt = p.CreatedAt
	if err := u.Repo.Create(ctx, p); err != nil {
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
	p.CreatedAt = u.Now().UTC()
	p.UpdatedAt = p.CreatedAt
	if err := u.Repo.Create(ctx, p); err != nil {
		return nil, err
	}
	return p, nil
}
