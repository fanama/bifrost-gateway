package metrics

import (
	"context"
	"time"

	"bridge-gateway/domain"
)

// MeasuredLLMProvider decore un provider LLM pour compter les appels, les
// erreurs et la latence, sans changer son comportement visible.
//
// Il se place sous le cache de reponses (voir buildLLMProvider) : un hit de
// cache n'est pas un appel provider et ne doit donc pas gonfler les compteurs.
type MeasuredLLMProvider struct {
	inner   domain.LLMProvider
	metrics *MetricsCollector
}

func NewMeasuredLLMProvider(inner domain.LLMProvider, metrics *MetricsCollector) *MeasuredLLMProvider {
	return &MeasuredLLMProvider{inner: inner, metrics: metrics}
}

// Chat mesure l'appel complet. BeginLLM est toujours suivi de RecordLLM, y
// compris quand la reponse revient en erreur.
func (p *MeasuredLLMProvider) Chat(ctx context.Context, cfg *domain.ChatConfig, messages []domain.ChatMessage) (domain.LLMResult, error) {
	started := time.Now()
	p.metrics.BeginLLM()
	result, err := p.inner.Chat(ctx, cfg, messages)
	p.metrics.RecordLLM(cfg.Provider, cfg.Model, time.Since(started), err, false)
	return result, err
}

// ChatStream mesure le flux jusqu'a sa fermeture, pas seulement son ouverture :
// une reponse interrompue doit apparaitre comme une erreur.
//
// Le decorateur relaye les fragments sans les alterer. Si le client part
// (ctx annule), le flux amont est draine jusqu'a sa fermeture pour ne pas
// bloquer son producteur, puis l'appel est enregistre une seule fois.
func (p *MeasuredLLMProvider) ChatStream(ctx context.Context, cfg *domain.ChatConfig, messages []domain.ChatMessage) (<-chan domain.StreamEvent, error) {
	started := time.Now()
	p.metrics.BeginLLM()

	streaming, ok := p.inner.(domain.StreamingLLMProvider)
	if !ok {
		result, err := p.inner.Chat(ctx, cfg, messages)
		p.metrics.RecordLLM(cfg.Provider, cfg.Model, time.Since(started), err, true)
		return domain.StreamViaFallback(result, err)
	}

	in, err := streaming.ChatStream(ctx, cfg, messages)
	if err != nil {
		p.metrics.RecordLLM(cfg.Provider, cfg.Model, time.Since(started), err, true)
		return nil, err
	}

	out := make(chan domain.StreamEvent)
	go func() {
		defer close(out)

		var streamErr error
		for event := range in {
			if event.Err != nil {
				streamErr = event.Err
			}
			select {
			case out <- event:
			case <-ctx.Done():
				for range in {
				}
				p.metrics.RecordLLM(cfg.Provider, cfg.Model, time.Since(started), streamErr, true)
				return
			}
		}
		p.metrics.RecordLLM(cfg.Provider, cfg.Model, time.Since(started), streamErr, true)
	}()

	return out, nil
}
