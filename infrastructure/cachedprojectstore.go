package infrastructure

import (
	"context"
	"encoding/json"
	"time"

	"bridge-gateway/domain"
)

// CachedProjectStore decorateur de cache pour domain.ProjectRepository.
// List n'est pas memorise (l'UI doit reflectir les creations immediatement) mais
// les lectures par identifiant, utilisees par l'authentification, le sont.
type CachedProjectStore struct {
	inner domain.ProjectRepository
	cache domain.Cache
	byID  domain.CacheKeyFactory
	ttl   time.Duration
}

func NewCachedProjectStore(inner domain.ProjectRepository, cache domain.Cache, ttl time.Duration) *CachedProjectStore {
	return &CachedProjectStore{
		inner: inner,
		cache: cache,
		byID:  domain.NewCacheKeyFactory("project"),
		ttl:   ttl,
	}
}

func (s *CachedProjectStore) List(ctx context.Context) ([]domain.Project, error) {
	return s.inner.List(ctx)
}

func (s *CachedProjectStore) Get(ctx context.Context, id string) (*domain.Project, error) {
	key := s.byID.Key(id)
	if raw, ok := s.cache.Get(key); ok {
		var p domain.Project
		if err := json.Unmarshal(raw, &p); err == nil {
			return &p, nil
		}
	}

	project, err := s.inner.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if raw, err := json.Marshal(project); err == nil {
		s.cache.Set(key, raw, s.ttl)
	}
	return project, nil
}

func (s *CachedProjectStore) Create(ctx context.Context, project *domain.Project) error {
	if err := s.inner.Create(ctx, project); err != nil {
		return err
	}
	s.cache.Delete(s.byID.Key(project.ID))
	return nil
}

func (s *CachedProjectStore) Update(ctx context.Context, project *domain.Project) error {
	if err := s.inner.Update(ctx, project); err != nil {
		return err
	}
	s.cache.Delete(s.byID.Key(project.ID))
	return nil
}

func (s *CachedProjectStore) Delete(ctx context.Context, id string) error {
	if err := s.inner.Delete(ctx, id); err != nil {
		return err
	}
	s.cache.Delete(s.byID.Key(id))
	return nil
}

var _ domain.ProjectRepository = (*CachedProjectStore)(nil)
