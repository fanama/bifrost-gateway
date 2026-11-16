package infrastructure

import (
	"context"
	"fmt"
	"log"
	"strings"

	"bridge-gateway/domain"
)

// ModelFallbackLLMProvider essaie les entrees de la chaine d'une configuration
// dans l'ordre : le modele principal d'abord, puis ses replis. Chaque entree
// porte son provider, donc un repli peut etre heberge ailleurs que le principal.
// La premiere reponse obtenue est servie ; une erreur passe a l'entree suivante.
// C'est la logique de repli ordonnee de freelm, appliquee aux modeles d'un
// projet.
//
// Un flux ne bascule qu'avant le premier fragment : apres, le client a deja
// recu du texte et changer de modele produirait une reponse incoherente.
type ModelFallbackLLMProvider struct {
	inner domain.LLMProvider
}

func NewModelFallbackLLMProvider(inner domain.LLMProvider) *ModelFallbackLLMProvider {
	return &ModelFallbackLLMProvider{inner: inner}
}

var _ domain.LLMProvider = (*ModelFallbackLLMProvider)(nil)
var _ domain.StreamingLLMProvider = (*ModelFallbackLLMProvider)(nil)

// Chat sert la requete avec la premiere entree de la chaine qui repond.
func (p *ModelFallbackLLMProvider) Chat(ctx context.Context, cfg *domain.ChatConfig, messages []domain.ChatMessage) (domain.LLMResult, error) {
	candidates := cfg.Candidates()
	if len(candidates) <= 1 {
		return p.inner.Chat(ctx, cfg, messages)
	}

	var lastErr error
	for _, choice := range candidates {
		result, err := p.inner.Chat(ctx, cfg.WithModel(choice), messages)
		if err == nil {
			logModelFallback(cfg.Model, choice.Label())
			return result, nil
		}
		if ctx.Err() != nil {
			return domain.LLMResult{}, err
		}
		lastErr = err
		logModelAttemptFailure(choice, err)
	}
	return domain.LLMResult{}, aggregateModelError(candidates, lastErr)
}

// ChatStream sert le flux de la premiere entree qui produit un fragment. Une
// tentative qui se ferme sans contenu est traitee comme un echec.
func (p *ModelFallbackLLMProvider) ChatStream(ctx context.Context, cfg *domain.ChatConfig, messages []domain.ChatMessage) (<-chan domain.StreamEvent, error) {
	candidates := cfg.Candidates()
	if len(candidates) <= 1 {
		return domain.StreamFrom(ctx, p.inner, cfg, messages)
	}

	var lastErr error
	for _, choice := range candidates {
		source, err := domain.StreamFrom(ctx, p.inner, cfg.WithModel(choice), messages)
		if err == nil {
			first, opened := firstUsableEvent(ctx, source)
			if opened == nil {
				logModelFallback(cfg.Model, choice.Label())
				return pumpFromFirst(ctx, source, first), nil
			}
			err = opened
		}
		if ctx.Err() != nil {
			return nil, err
		}
		lastErr = err
		logModelAttemptFailure(choice, err)
	}
	return nil, aggregateModelError(candidates, lastErr)
}

// firstUsableEvent lit le premier fragment d'un flux. Une chaine close sans
// contenu, ou un fragment d'erreur, signifie que ce modele n'a rien produit :
// l'appelant peut essayer le suivant.
func firstUsableEvent(ctx context.Context, source <-chan domain.StreamEvent) (domain.StreamEvent, error) {
	first, ok, err := firstEvent(ctx, source)
	if err != nil {
		return domain.StreamEvent{}, err
	}
	if !ok {
		return domain.StreamEvent{}, fmt.Errorf("stream closed before any content")
	}
	if first.Err != nil {
		return domain.StreamEvent{}, first.Err
	}
	return first, nil
}

func pumpFromFirst(ctx context.Context, source <-chan domain.StreamEvent, first domain.StreamEvent) <-chan domain.StreamEvent {
	out := make(chan domain.StreamEvent)
	go func() {
		defer close(out)
		relayStreamEvents(ctx, source, out, first)
	}()
	return out
}

func logModelFallback(primary, served string) {
	if primary != served {
		log.Printf("model fallback: %s unavailable, served by %s", primary, served)
	}
}

func logModelAttemptFailure(choice domain.ModelChoice, err error) {
	log.Printf("model fallback: %s unavailable (%s), trying the next model", choice.Label(), failoverReason(err))
}

func aggregateModelError(candidates []domain.ModelChoice, lastErr error) error {
	if lastErr == nil {
		lastErr = fmt.Errorf("no model was callable")
	}
	labels := make([]string, 0, len(candidates))
	for _, choice := range candidates {
		labels = append(labels, choice.Label())
	}
	return fmt.Errorf("all models failed (%s): %w", strings.Join(labels, ", "), lastErr)
}
