package application

import (
	"context"
	"strings"
	"time"

	"bridge-gateway/domain"
)

type APIKeyUseCase struct {
	keys     domain.APIKeyRepository
	projects domain.ProjectRepository
	idGen    func() string
	now      func() time.Time
}

func NewAPIKeyUseCase(keys domain.APIKeyRepository, projects domain.ProjectRepository) *APIKeyUseCase {
	return &APIKeyUseCase{keys: keys, projects: projects, idGen: domain.NewID, now: time.Now}
}

func (u *APIKeyUseCase) ListByProject(ctx context.Context, projectID string) ([]domain.APIKey, error) {
	return u.keys.ListByProject(ctx, projectID)
}

func (u *APIKeyUseCase) Get(ctx context.Context, id string) (*domain.APIKey, error) {
	return u.keys.Get(ctx, id)
}

// Create genere une nouvelle cle pour le projet et retourne le secret en clair
// (a afficher une seule fois). Seule l'empreinte SHA-256 est persistee.
func (u *APIKeyUseCase) Create(ctx context.Context, projectID, name string) (newKey *domain.APIKey, plaintext string, err error) {
	if _, err := u.projects.Get(ctx, projectID); err != nil {
		return nil, "", err
	}
	k := &domain.APIKey{
		ProjectID: projectID,
		Name:      strings.TrimSpace(name),
	}
	if err := k.Validate(); err != nil {
		return nil, "", err
	}
	secret, prefix, digest := domain.NewAPIKey()
	k.ID = "key-" + domain.NewID()
	k.Prefix = prefix
	k.Digest = digest
	k.CreatedAt = u.now().UTC()
	if err := u.keys.Create(ctx, k); err != nil {
		return nil, "", err
	}
	return k, secret, nil
}

func (u *APIKeyUseCase) Delete(ctx context.Context, id string) error {
	return u.keys.Delete(ctx, id)
}
