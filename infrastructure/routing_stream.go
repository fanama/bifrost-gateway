package infrastructure

import (
	"context"
	"fmt"
	"log"

	"bridge-gateway/domain"
)

// ChatStream rejoue l'echelle de routage en streaming.
//
// La bascule ne peut plus etre arbitraire comme dans Chat : des que le client a
// recu un fragment, le texte est chez lui, changer de modele ajouterait une
// seconde reponse a la suite de la premiere. L'escalade est donc limitee au
// tout premier fragment : tant que rien n'a ete emis, un echec ouvre le tier
// suivant, exactement comme dans Chat. Au-dela, une coupure est propagee au
// client et le flux se termine.
//
// Ce compromis est inherent au streaming : c'est le prix de la latence reelle,
// et il est preferable a retenir le texte pour pouvoir encore decider.
func (p *RoutingLLMProvider) ChatStream(ctx context.Context, cfg *domain.ChatConfig, messages []domain.ChatMessage) (<-chan domain.StreamEvent, error) {
	ladder := p.ladder(ctx, cfg, messages)
	if len(ladder) == 0 {
		return domain.StreamFrom(ctx, p.inner, cfg, messages)
	}

	var (
		attempts []string
		lastErr  error
	)
	for i, target := range ladder {
		if p.maxAttempts > 0 && i >= p.maxAttempts {
			break
		}

		merged := p.applyTier(ctx, cfg, target)
		if merged == nil {
			attempts = append(attempts, fmt.Sprintf("%s/%s (identifiants manquants)", target.Provider, target.Model))
			continue
		}

		source, err := domain.StreamFrom(ctx, p.inner, merged, messages)
		if err == nil {
			// On attend le premier fragment pour savoir si le tier tient. C'est
			// le seul moment ou une bascule est encore sans consequence pour le
			// client.
			first, ok, waitErr := firstEvent(ctx, source)
			if ok {
				if len(attempts) > 0 {
					logRoutingFailover(cfg.Model, first.Model, attempts)
				}
				return p.pump(ctx, source, first), nil
			}
			if waitErr != nil {
				err = waitErr
			} else {
				err = fmt.Errorf("stream closed before any content")
			}
		}

		reason := failoverReason(err)
		attempts = append(attempts, fmt.Sprintf("%s/%s (%s)", target.Provider, target.Model, reason))
		lastErr = err

		if ctx.Err() != nil {
			break
		}
		if reason == FailoverClient {
			return nil, p.aggregateError(cfg, attempts, err)
		}
		if reason == FailoverAuth {
			log.Printf("routing: auth failure on %s/%s, escalating (check the API key for this configuration)", target.Provider, target.Model)
		}
		log.Printf("routing: %s/%s unavailable (%s), escalating", target.Provider, target.Model, reason)
		p.cooldownTier(target)
	}

	if lastErr == nil {
		lastErr = fmt.Errorf("no routing candidate was callable")
	}
	return nil, p.aggregateError(cfg, attempts, lastErr)
}

// firstEvent lit le premier fragment d'un flux. ok vaut false si le flux s'est
// ferme sans rien produire, ou si le contexte a tranche.
func firstEvent(ctx context.Context, source <-chan domain.StreamEvent) (domain.StreamEvent, bool, error) {
	select {
	case <-ctx.Done():
		return domain.StreamEvent{}, false, ctx.Err()
	case event, ok := <-source:
		if !ok {
			return domain.StreamEvent{}, false, nil
		}
		return event, true, nil
	}
}

// pump relaie un flux dont le premier fragment a deja ete lu.
func (p *RoutingLLMProvider) pump(ctx context.Context, source <-chan domain.StreamEvent, first domain.StreamEvent) <-chan domain.StreamEvent {
	out := make(chan domain.StreamEvent)
	go func() {
		defer close(out)

		pending := first
		for {
			select {
			case <-ctx.Done():
				return
			case out <- pending:
			}
			// Une coupure ne se rattrape pas : le client a deja une partie du
			// texte, relancer un autre tier produirait deux reponses enchainees.
			if pending.Done || pending.Err != nil {
				return
			}

			select {
			case <-ctx.Done():
				return
			case event, ok := <-source:
				if !ok {
					return
				}
				pending = event
			}
		}
	}()
	return out
}

var _ domain.StreamingLLMProvider = (*RoutingLLMProvider)(nil)
