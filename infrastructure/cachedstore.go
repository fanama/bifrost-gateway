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

// CachedAPIKeyStore decorateur de cache pour domain.APIKeyRepository.
//
// FindByDigest est le chemin le plus chaud du gateway : chaque requete
// /v1/chat/completions resout sa cle via cette methode. Le TTL est court par
// defaut pour qu'une cle revoquee depuis l'UI soit purgee rapidement.
type CachedAPIKeyStore struct {
	inner     domain.APIKeyRepository
	cache     domain.Cache
	byDigest  domain.CacheKeyFactory
	byID      domain.CacheKeyFactory
	byProject domain.CacheKeyFactory
	ttl       time.Duration
}

func NewCachedAPIKeyStore(inner domain.APIKeyRepository, cache domain.Cache, ttl time.Duration) *CachedAPIKeyStore {
	return &CachedAPIKeyStore{
		inner:     inner,
		cache:     cache,
		byDigest:  domain.NewCacheKeyFactory("apikey.digest"),
		byID:      domain.NewCacheKeyFactory("apikey"),
		byProject: domain.NewCacheKeyFactory("apikey.project"),
		ttl:       ttl,
	}
}

func (s *CachedAPIKeyStore) List(ctx context.Context) ([]domain.APIKey, error) {
	return s.inner.List(ctx)
}

func (s *CachedAPIKeyStore) Get(ctx context.Context, id string) (*domain.APIKey, error) {
	cacheKey := s.byID.Key(id)
	if raw, ok := s.cache.Get(cacheKey); ok {
		var k domain.APIKey
		if err := json.Unmarshal(raw, &k); err == nil {
			return &k, nil
		}
	}

	record, err := s.inner.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	s.put(cacheKey, record)
	return record, nil
}

func (s *CachedAPIKeyStore) Create(ctx context.Context, record *domain.APIKey) error {
	if err := s.inner.Create(ctx, record); err != nil {
		return err
	}
	s.invalidate(record)
	return nil
}

// Delete invalide l'identifiant, le digest et le namespace du projet, pour
// qu'une cle revoquee ne puisse plus etre utilisee. La cle est relue avant
// suppression pour connaetre son digest, condition de l'invalidation.
func (s *CachedAPIKeyStore) Delete(ctx context.Context, id string) error {
	record, err := s.inner.Get(ctx, id)
	if err != nil {
		return err
	}
	if err := s.inner.Delete(ctx, id); err != nil {
		return err
	}
	s.invalidate(record)
	return nil
}

func (s *CachedAPIKeyStore) ListByProject(ctx context.Context, projectID string) ([]domain.APIKey, error) {
	return s.inner.ListByProject(ctx, projectID)
}

func (s *CachedAPIKeyStore) FindByDigest(ctx context.Context, digest string) (*domain.APIKey, error) {
	if digest == "" {
		return nil, domain.ErrAPIKeyNotFound
	}

	cacheKey := s.byDigest.Key(digest)
	if raw, ok := s.cache.Get(cacheKey); ok {
		var k domain.APIKey
		if err := json.Unmarshal(raw, &k); err == nil {
			return &k, nil
		}
	}

	record, err := s.inner.FindByDigest(ctx, digest)
	if err != nil {
		// Une absence n'est pas memorisee : une cle peut etre creee a tout moment.
		return nil, err
	}
	s.put(cacheKey, record)
	return record, nil
}

func (s *CachedAPIKeyStore) put(key string, record *domain.APIKey) {
	raw, err := json.Marshal(record)
	if err != nil {
		return
	}
	s.cache.Set(key, raw, s.ttl)
}

func (s *CachedAPIKeyStore) invalidate(record *domain.APIKey) {
	s.cache.Delete(s.byID.Key(record.ID))
	s.cache.Delete(s.byDigest.Key(record.Digest))
	s.cache.Delete(s.byProject.Key(record.ProjectID))
}

var _ domain.APIKeyRepository = (*CachedAPIKeyStore)(nil)

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
