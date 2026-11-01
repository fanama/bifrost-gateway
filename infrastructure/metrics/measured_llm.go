package metrics

import (
	"context"
	"strings"
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

// Chat mesure l'appel complet. BeginLLM est toujours suivi de record, y compris
// quand la reponse revient en erreur.
func (p *MeasuredLLMProvider) Chat(ctx context.Context, cfg *domain.ChatConfig, messages []domain.ChatMessage) (domain.LLMResult, error) {
	started := time.Now()
	p.metrics.BeginLLM()
	result, err := p.inner.Chat(ctx, cfg, messages)
	p.record(cfg, messages, result.Model, result.Content, false, started, err)
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
		p.record(cfg, messages, result.Model, result.Content, true, started, err)
		return domain.StreamViaFallback(result, err)
	}

	in, err := streaming.ChatStream(ctx, cfg, messages)
	if err != nil {
		p.record(cfg, messages, "", "", true, started, err)
		return nil, err
	}

	out := make(chan domain.StreamEvent)
	go p.relay(ctx, in, out, cfg, messages, started)
	return out, nil
}

// relay relaie les fragments jusqu'a la fermeture du flux amont, puis
// journalise l'appel. Le texte est reconstitue au fil des fragments : c'est la
// reponse complete qui interesse le journal, pas chaque delta.
func (p *MeasuredLLMProvider) relay(
	ctx context.Context,
	in <-chan domain.StreamEvent,
	out chan<- domain.StreamEvent,
	cfg *domain.ChatConfig,
	messages []domain.ChatMessage,
	started time.Time,
) {
	defer close(out)

	var (
		streamErr error
		model     string
		content   strings.Builder
	)
	for event := range in {
		if event.Model != "" {
			model = event.Model
		}
		if event.Err != nil {
			streamErr = event.Err
		}
		content.WriteString(event.Delta)
		select {
		case out <- event:
		case <-ctx.Done():
			for range in {
			}
			p.record(cfg, messages, model, content.String(), true, started, streamErr)
			return
		}
	}
	p.record(cfg, messages, model, content.String(), true, started, streamErr)
}

// record cloture une mesure : compteurs agreges puis entree du journal. Les
// deux ecritures sont faites ici pour qu'aucun chemin d'appel n'oublie l'une
// des deux.
func (p *MeasuredLLMProvider) record(
	cfg *domain.ChatConfig,
	messages []domain.ChatMessage,
	servedModel, output string,
	stream bool,
	started time.Time,
	err error,
) {
	duration := time.Since(started)
	p.metrics.RecordLLM(cfg.Provider, cfg.Model, duration, err, stream)
	p.metrics.RecordLLMLog(newCallLog(cfg, messages, servedModel, output, stream, duration, err))
}
