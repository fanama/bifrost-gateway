package application

import (
	"context"
	"strings"

	"bridge-gateway/domain"
)

// AuthUseCase authentifie les clients API (Bearer). Soit la master key globale,
// soit une cle de projet. Retourne le projet associe (nil pour la master key).
type AuthUseCase struct {
	masterKey string
	keys      domain.APIKeyRepository
	projects  domain.ProjectRepository
}

func NewAuthUseCase(masterKey string, keys domain.APIKeyRepository, projects domain.ProjectRepository) *AuthUseCase {
	return &AuthUseCase{masterKey: strings.TrimSpace(masterKey), keys: keys, projects: projects}
}

type AuthResult struct {
	Project  *domain.Project
	Scope    string // "master" | "project"
	APIKeyID string
}

func (a *AuthUseCase) Authenticate(ctx context.Context, bearer string) (*AuthResult, error) {
	secret := strings.TrimSpace(bearer)
	secret = strings.TrimPrefix(secret, "Bearer ")
	secret = strings.TrimPrefix(secret, "bearer ")
	secret = strings.TrimSpace(secret)
	if secret == "" {
		return nil, domain.ErrInvalidAPIKey
	}
	if a.masterKey != "" && secret == a.masterKey {
		return &AuthResult{Scope: "master"}, nil
	}
	key, err := a.keys.FindByDigest(ctx, domain.HashAPIKey(secret))
	if err != nil {
		return nil, domain.ErrInvalidAPIKey
	}
	project, err := a.projects.Get(ctx, key.ProjectID)
	if err != nil {
		return nil, domain.ErrInvalidAPIKey
	}
	return &AuthResult{Scope: "project", Project: project, APIKeyID: key.ID}, nil
}
