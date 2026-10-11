package infrastructure

import (
	"context"
	"encoding/json"
	"time"

	"bridge-gateway/domain"
)

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
