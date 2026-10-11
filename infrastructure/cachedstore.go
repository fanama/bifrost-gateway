package infrastructure

import (
	"context"
	"encoding/json"
	"time"

	"bridge-gateway/domain"
)

// CachedConfigStore decorateur de cache pour domain.ChatConfigRepository.
//
// Il utilise le port domain.Cache (implementation en memoire dans ce paquet,
// substitution directe de Redis). Les lectures par identifiant sont memorisees,
// tout comme la configuration active d'un projet.
//
// Toute ecriture invalide les cles concernees, donc aucune valeur perimee ne
// peut etre servie apres un update, un delete ou un SetActive.
type CachedConfigStore struct {
	inner  domain.ChatConfigRepository
	cache  domain.Cache
	byID   domain.CacheKeyFactory
	active domain.CacheKeyFactory
	ttl    time.Duration
}

func NewCachedConfigStore(inner domain.ChatConfigRepository, cache domain.Cache, ttl time.Duration) *CachedConfigStore {
	return &CachedConfigStore{
		inner:  inner,
		cache:  cache,
		byID:   domain.NewCacheKeyFactory("config"),
		active: domain.NewCacheKeyFactory("active"),
		ttl:    ttl,
	}
}

// List n'est pas memorise : l'UI liste les configurations d'un projet et doit
// reflecter immediatement toute creation ou edition.
func (s *CachedConfigStore) List(ctx context.Context) ([]domain.ChatConfig, error) {
	return s.inner.List(ctx)
}

func (s *CachedConfigStore) Get(ctx context.Context, id string) (*domain.ChatConfig, error) {
	key := s.byID.Key(id)
	if raw, ok := s.cache.Get(key); ok {
		var cfg domain.ChatConfig
		if err := json.Unmarshal(raw, &cfg); err == nil {
			return &cfg, nil
		}
	}

	cfg, err := s.inner.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	s.put(s.byID.Key(id), cfg)
	return cfg, nil
}

func (s *CachedConfigStore) Create(ctx context.Context, cfg *domain.ChatConfig) error {
	if err := s.inner.Create(ctx, cfg); err != nil {
		return err
	}
	s.invalidate(cfg)
	return nil
}

func (s *CachedConfigStore) Update(ctx context.Context, cfg *domain.ChatConfig) error {
	if err := s.inner.Update(ctx, cfg); err != nil {
		return err
	}
	s.invalidate(cfg)
	return nil
}

func (s *CachedConfigStore) Delete(ctx context.Context, id string) error {
	if err := s.inner.Delete(ctx, id); err != nil {
		return err
	}
	s.cache.Delete(s.byID.Key(id))
	s.cache.DeletePrefix(s.active.Prefix())
	return nil
}

func (s *CachedConfigStore) ListByProject(ctx context.Context, projectID string) ([]domain.ChatConfig, error) {
	return s.inner.ListByProject(ctx, projectID)
}

func (s *CachedConfigStore) GetActiveByProject(ctx context.Context, projectID string) (*domain.ChatConfig, error) {
	key := s.active.Key(projectID)
	if raw, ok := s.cache.Get(key); ok {
		var cfg domain.ChatConfig
		if err := json.Unmarshal(raw, &cfg); err == nil {
			return &cfg, nil
		}
	}

	cfg, err := s.inner.GetActiveByProject(ctx, projectID)
	if err != nil {
		// ErrNoActiveConfig n'est pas memorise : la selection peut changer a
		// tout moment depuis l'UI.
		return nil, err
	}
	s.put(key, cfg)
	return cfg, nil
}

func (s *CachedConfigStore) SetActive(ctx context.Context, projectID string, id string) error {
	if err := s.inner.SetActive(ctx, projectID, id); err != nil {
		return err
	}
	// Invalidation par namespace : tout ce qui derive de la selection active
	// du projet devient caduc.
	s.cache.DeletePrefix(s.active.Prefix())
	return nil
}

func (s *CachedConfigStore) put(key string, cfg *domain.ChatConfig) {
	raw, err := json.Marshal(cfg)
	if err != nil {
		return
	}
	s.cache.Set(key, raw, s.ttl)
}

func (s *CachedConfigStore) invalidate(cfg *domain.ChatConfig) {
	s.cache.Delete(s.byID.Key(cfg.ID))
	s.cache.Delete(s.active.Key(cfg.ProjectID))
}

var _ domain.ChatConfigRepository = (*CachedConfigStore)(nil)
