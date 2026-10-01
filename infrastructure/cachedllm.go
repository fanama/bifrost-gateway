package infrastructure

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"bridge-gateway/domain"
)

// CachedLLMProvider decorateur de cache pour domain.LLMProvider.
//
// Objectif : ne pas rappeler un provider pour une requete strictement identique.
// La cle est une empreinte de la configuration effective (provider, modele, base
// URL) et des messages envoyes, donc deux appels identiques partagent l'entree.
//
// Les erreurs ne sont jamais mises en cache : un echec transitoire (rate limit,
// provider indisponible) ne doit pas etre rejoue pendant toute la duree du TTL.
//
// Le cache est un detail d'infrastructure : le domaine ne connait que
// domain.LLMProvider et domain.Cache.
type CachedLLMProvider struct {
	inner domain.LLMProvider
	cache domain.Cache
	keys  domain.CacheKeyFactory
	ttl   int // en secondes, pour que le timeout reste configurable
}

func NewCachedLLMProvider(inner domain.LLMProvider, cache domain.Cache, ttl time.Duration) *CachedLLMProvider {
	return &CachedLLMProvider{
		inner: inner,
		cache: cache,
		keys:  domain.NewCacheKeyFactory("llm"),
		ttl:   int(ttl.Seconds()),
	}
}

func (p *CachedLLMProvider) Chat(ctx context.Context, cfg *domain.ChatConfig, messages []domain.ChatMessage) (string, error) {
	if p.ttl <= 0 {
		return p.inner.Chat(ctx, cfg, messages)
	}

	key, err := p.cacheKey(cfg, messages)
	if err != nil {
		// Impossible de serialiser : on delega sans mettre en cache plutot que
		// d'echouer.
		return p.inner.Chat(ctx, cfg, messages)
	}

	if raw, ok := p.cache.Get(key); ok {
		var cached string
		if err := json.Unmarshal(raw, &cached); err == nil {
			return cached, nil
		}
	}

	reply, err := p.inner.Chat(ctx, cfg, messages)
	if err != nil {
		return "", err
	}

	if raw, err := json.Marshal(reply); err == nil {
		p.cache.Set(key, raw, time.Duration(p.ttl)*time.Second)
	}
	return reply, nil
}

// cacheKey derive une empreinte stable de tout ce qui determine la reponse du
// provider : identite de la config + historique des messages. Les champs de
// sampling (temperature, max_tokens) sont inclus pour ne pas servir la reponse
// d'un appel non deterministe a une requete configuree differemment.
func (p *CachedLLMProvider) cacheKey(cfg *domain.ChatConfig, messages []domain.ChatMessage) (string, error) {
	h := sha256.New()

	writeField := func(name, value string) {
		h.Write([]byte(name))
		h.Write([]byte{0})
		h.Write([]byte(value))
		h.Write([]byte{0})
	}

	writeField("provider", cfg.Provider)
	writeField("model", cfg.Model)
	writeField("base_url", cfg.BaseURL)
	writeField("temperature", fmt.Sprintf("%g", valueOrZero(cfg.Temperature)))
	writeField("max_tokens", fmt.Sprintf("%d", valueOrZero(cfg.MaxTokens)))
	writeField("system_prompt", cfg.SystemPrompt)
	writeField("response_format", cfg.ResponseFormat)

	for _, m := range messages {
		writeField("role", m.Role)
		writeField("content", m.Content)
	}

	return p.keys.Key(hex.EncodeToString(h.Sum(nil))), nil
}

// valueOrZero rend un pointeur nil et sa valeur pointee indiscernables pour la
// construction de la cle : deux configurations sans temperature ne doivent pas
// produire deux entrees.
func valueOrZero[T comparable](v *T) T {
	if v == nil {
		var zero T
		return zero
	}
	return *v
}

var _ domain.LLMProvider = (*CachedLLMProvider)(nil)
