package infrastructure

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
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

func (p *CachedLLMProvider) Chat(ctx context.Context, cfg *domain.ChatConfig, messages []domain.ChatMessage) (domain.LLMResult, error) {
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
		var cached domain.LLMResult
		if err := json.Unmarshal(raw, &cached); err == nil {
			return cached, nil
		}
	}

	reply, err := p.inner.Chat(ctx, cfg, messages)
	if err != nil {
		return domain.LLMResult{}, err
	}

	// Le modele qui a repondu est memorise avec le texte : le cache separe du
	// routeur, donc une entrie peut avoir ete produite par un autre modele que
	// celui de la configuration demandee. Sans cela, un hit rapporterait un modele
	// qui n'a pas produit la reponse.
	if raw, err := json.Marshal(reply); err == nil {
		p.cache.Set(key, raw, time.Duration(p.ttl)*time.Second)
	}
	return reply, nil
}

// cacheKey derive une empreinte stable de tout ce qui determine la reponse du
// provider : identite de la config + historique des messages. Les champs de
// sampling (temperature, max_tokens) sont inclus pour ne pas servir la reponse
// d'un appel non deterministe a une requete configuree differemment.
//
// tier figure dans la cle parce qu'il conditionne le modele reellement choisi
// par le routeur : deux configurations identiques mais de tiers differents
// peuvent aboutir a deux modeles differents pour la meme conversation.
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
	writeField("tier", cfg.Tier)
	writeField("base_url", cfg.BaseURL)
	writeField("temperature", fmt.Sprintf("%g", valueOrZero(cfg.Temperature)))
	writeField("max_tokens", fmt.Sprintf("%d", valueOrZero(cfg.MaxTokens)))
	writeField("system_prompt", cfg.SystemPrompt)
	// Le format est indexe avec le schema, et sur sa valeur effective : deux
	// configurations qui demandent la meme sortie structuree mais avec des
	// schemas differents ne doivent pas partager une entree de cache. Sans cela,
	// une reponse conforme au schema A serait servie a une requete qui attend le
	// schema B.
	writeField("response_format", cfg.EffectiveResponseFormat())
	writeField("response_schema", cfg.ResponseSchema)

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

// ChatStream sert le cache de facon incremental.
//
// Un hit ne peut pas etre rejoue fragment par fragment : l'entree ne contient
// que le texte final. Il est donc redonne en un seul fragment, ce qui reste une
// reponse SSE valide. Un miss, lui, est streamed pour de vrai puis memorise a
// la cloture, assemblee a partir des fragments.
//
// La cle est celle de Chat : une requete identique a ete servie par le meme
// texte, donc la partager nintroduit aucune divergence.
func (p *CachedLLMProvider) ChatStream(ctx context.Context, cfg *domain.ChatConfig, messages []domain.ChatMessage) (<-chan domain.StreamEvent, error) {
	if p.ttl <= 0 {
		return domain.StreamFrom(ctx, p.inner, cfg, messages)
	}

	key, err := p.cacheKey(cfg, messages)
	if err != nil {
		return domain.StreamFrom(ctx, p.inner, cfg, messages)
	}

	if raw, ok := p.cache.Get(key); ok {
		var cached domain.LLMResult
		if err := json.Unmarshal(raw, &cached); err == nil {
			return domain.StreamViaFallback(cached, nil)
		}
	}

	source, err := domain.StreamFrom(ctx, p.inner, cfg, messages)
	if err != nil {
		return nil, err
	}

	out := make(chan domain.StreamEvent)
	go func() {
		defer close(out)

		var assembled strings.Builder
		for {
			select {
			case <-ctx.Done():
				// Abandon du client : rien n'est memorise, une entree partielle
				// serait servie a une requete ulterieure comme une reponse
				// complete alors qu'elle est tronquee.
				return
			case event, ok := <-source:
				if !ok {
					return
				}
				if event.Err != nil {
					// Reponse incomplete : elle ne doit jamais etre memorisee.
					select {
					case out <- event:
					case <-ctx.Done():
					}
					return
				}
				assembled.WriteString(event.Delta)

				select {
				case out <- event:
				case <-ctx.Done():
					return
				}

				if event.Done {
					// La reponse est complete : elle seule merite d'etre memorisee.
					entry := domain.LLMResult{Content: assembled.String(), Model: event.Model}
					if raw, err := json.Marshal(entry); err == nil {
						p.cache.Set(key, raw, time.Duration(p.ttl)*time.Second)
					}
					return
				}
			}
		}
	}()

	return out, nil
}

var _ domain.StreamingLLMProvider = (*CachedLLMProvider)(nil)
