package infrastructure

import (
	"context"
	"strings"

	"bridge-gateway/domain"
)

var (
	_ domain.LLMProvider          = (*OnnxChatProvider)(nil)
	_ domain.StreamingLLMProvider = (*OnnxChatProvider)(nil)
)

// chatModelID resout la configuration vers l'identifiant du repertoire du
// modele : le prefixe onnx/ est optionnel, la config peut nommer le
// repertoire brut ("smollm2-135m-instruct").
func chatModelID(cfg *domain.ChatConfig) string {
	m := ""
	if cfg != nil {
		m = strings.TrimSpace(cfg.Model)
	}
	return strings.TrimPrefix(m, domain.OnnxModelPrefix)
}

// onnxChatSettings sont les reglages effectifs d'une generation.
type onnxChatSettings struct {
	maxNew int
	temp   float64
	topP   float64
}

// settings derive les reglages de la configuration. Defauts alignes sur les
// recommandations SmolLM2 (temperature 0.6, top_p 0.9) ; temperature <= 0
// est une generation gloutonne (argmax), deterministe.
func (p *OnnxChatProvider) settings(cfg *domain.ChatConfig) onnxChatSettings {
	s := onnxChatSettings{maxNew: p.opts.MaxNewTokens, temp: 0.6, topP: 0.9}
	if cfg == nil {
		return s
	}
	if cfg.MaxTokens != nil && *cfg.MaxTokens > 0 {
		s.maxNew = *cfg.MaxTokens
	}
	if cfg.Temperature != nil {
		s.temp = *cfg.Temperature
	}
	if cfg.TopP != nil {
		s.topP = *cfg.TopP
	}
	return s
}

// Chat genere une reponse en une passe. Le chargement du modele est paresseux
// et le plus long verrou du processus (l'inference est serialisee) : deux
// conversations locales attendent l'une l'autre plutot que de partager un
// cache KV qui ne l'est pas.
func (p *OnnxChatProvider) Chat(ctx context.Context, cfg *domain.ChatConfig, messages []domain.ChatMessage) (domain.LLMResult, error) {
	text, err := p.generate(ctx, cfg, messages, nil)
	if err != nil {
		return domain.LLMResult{}, err
	}
	return domain.LLMResult{Content: text, Model: chatResultModel(cfg)}, nil
}

// chatResultModel nomme le modele qui a repondu : la config si elle en nomme
// un, sinon l'identifiant du repertoire discovered.
func chatResultModel(cfg *domain.ChatConfig) string {
	if cfg != nil && strings.TrimSpace(cfg.Model) != "" {
		return strings.TrimSpace(cfg.Model)
	}
	return domain.OnnxModelPrefix + chatModelID(cfg)
}

// ChatStream rejoue la meme generation fragment par fragment : chaque token
// devient un delta, le texte stable est cumule pour ne jamais emettre un
// octet orphelin (UTF-8 coupe entre deux tokens).
func (p *OnnxChatProvider) ChatStream(ctx context.Context, cfg *domain.ChatConfig, messages []domain.ChatMessage) (<-chan domain.StreamEvent, error) {
	// Chargement anticipé : une configuration cassee est une erreur
	// synchrone, pas un flux qui s'ouvre et se referme aussitot.
	p.mu.Lock()
	_, err := p.stateLocked(chatModelID(cfg))
	p.mu.Unlock()
	if err != nil {
		return nil, err
	}

	out := make(chan domain.StreamEvent)
	model := chatResultModel(cfg)
	go func() {
		defer close(out)
		emitter := &generationEmitter{ctx: ctx, out: out, model: model}
		if _, err := p.generate(ctx, cfg, messages, emitter.emit); err != nil {
			sendStreamEvent(ctx, out, domain.StreamEvent{Err: err, Model: model})
			return
		}
		sendStreamEvent(ctx, out, domain.StreamEvent{Model: model, Done: true})
	}()
	return out, nil
}

// generationEmitter converts cumulative model text into deltas, keeping UTF-8
// runes intact and emitting the assistant role exactly once.
type generationEmitter struct {
	ctx   context.Context
	out   chan<- domain.StreamEvent
	model string
	sent  string
	first bool
}

func (e *generationEmitter) emit(text string) error {
	delta := ""
	if strings.HasPrefix(text, e.sent) {
		delta = text[len(e.sent):]
	}
	e.sent = text
	if delta == "" {
		return nil
	}
	event := domain.StreamEvent{Delta: delta, Model: e.model}
	if !e.first {
		e.first = true
		event.Role = "assistant"
	}
	select {
	case e.out <- event:
		return nil
	case <-e.ctx.Done():
		return e.ctx.Err()
	}
}
