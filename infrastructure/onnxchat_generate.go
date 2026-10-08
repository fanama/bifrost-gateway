package infrastructure

import (
	"context"
	"fmt"
	"math"
	"math/rand"
	"sort"
	"strings"
	"time"

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
		sent := ""
		first := true
		emit := func(text string) error {
			delta := ""
			if strings.HasPrefix(text, sent) {
				delta = text[len(sent):]
			}
			sent = text
			if delta == "" {
				return nil
			}
			ev := domain.StreamEvent{Delta: delta, Model: model}
			if first {
				ev.Role = "assistant"
				first = false
			}
			select {
			case out <- ev:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		_, err := p.generate(ctx, cfg, messages, emit)
		if err != nil {
			select {
			case out <- domain.StreamEvent{Err: err, Model: model}:
			case <-ctx.Done():
			}
			return
		}
		select {
		case out <- domain.StreamEvent{Model: model, Done: true}:
		case <-ctx.Done():
		}
	}()
	return out, nil
}

// generate est la boucle reelle : prefill du prompt, puis echantillonnage pas
// a pas jusqu'au jeton d'arret ou a la budget de tokens. emit (optionnel)
// recoit le texte cumule stable a chaque token, pour le streaming.
func (p *OnnxChatProvider) generate(
	ctx context.Context,
	cfg *domain.ChatConfig,
	messages []domain.ChatMessage,
	emit func(string) error,
) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	st, err := p.stateLocked(chatModelID(cfg))
	if err != nil {
		return "", err
	}
	s := p.settings(cfg)
	if s.maxNew > p.opts.MaxSeqLen-1 {
		s.maxNew = p.opts.MaxSeqLen - 1
	}

	ids := st.tok.Encode(renderChatPrompt(messages))
	if len(ids) == 0 {
		return "", fmt.Errorf("onnx chat: empty prompt")
	}
	// La fenetre couvre prompt + generation : on tronque la tete du prompt,
	// les messages recents primant sur l'ancien.
	if overflow := len(ids) + s.maxNew - p.opts.MaxSeqLen; overflow > 0 {
		if overflow >= len(ids) {
			overflow = len(ids) - 1
		}
		ids = ids[overflow:]
	}

	kv := newOnnxKVCache(st.layers, st.kvHeads, st.headDim)
	promptIDs := make([]int64, len(ids))
	for i, id := range ids {
		promptIDs[i] = int64(id)
	}
	logits, err := p.step(ctx, st, kv, promptIDs)
	if err != nil {
		return "", err
	}

	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	var gen []int
	text := ""
	for len(gen) < s.maxNew {
		tok := sampleToken(logits, s, rng)
		if st.eos[tok] {
			break
		}
		gen = append(gen, tok)
		text = st.tok.DecodeStable(gen)
		if emit != nil {
			if err := emit(text); err != nil {
				return "", err
			}
		}
		logits, err = p.step(ctx, st, kv, []int64{int64(tok)})
		if err != nil {
			return "", err
		}
	}
	final := st.tok.Decode(gen)
	if emit != nil && final != text {
		if err := emit(final); err != nil {
			return "", err
		}
	}
	return final, nil
}

// sampleToken choisit le token suivant : glouton (argmax) si la temperature
// est nulle ou negative, sinon softmax temperature puis noyau top_p. Les
// logits infinis sont bornes par le softmax centre sur le maximum, ce qui
// evite un debordement pour des scores extremes.
func sampleToken(logits []float32, s onnxChatSettings, rng *rand.Rand) int {
	if s.temp <= 0 {
		best := 0
		for i := 1; i < len(logits); i++ {
			if logits[i] > logits[best] {
				best = i
			}
		}
		return best
	}

	probs := make([]float64, len(logits))
	var sum float64
	maxLogit := float64(logits[0])
	for _, l := range logits {
		if float64(l) > maxLogit {
			maxLogit = float64(l)
		}
	}
	for i, l := range logits {
		probs[i] = math.Exp((float64(l) - maxLogit) / s.temp)
		sum += probs[i]
	}

	// Noyau top_p : on garde le plus petit ensemble de candidats couvrant la
	// masse demandee, on renormalise, puis on tire.
	if s.topP > 0 && s.topP < 1 && len(probs) > 1 {
		order := make([]int, len(probs))
		for i := range order {
			order[i] = i
		}
		sort.Slice(order, func(a, b int) bool { return probs[order[a]] > probs[order[b]] })
		var cum float64
		keep := 0
		for keep < len(order) {
			cum += probs[order[keep]] / sum
			keep++
			if cum >= s.topP {
				break
			}
		}
		var kept float64
		for i := 0; i < keep; i++ {
			kept += probs[order[i]]
		}
		r := rng.Float64() * kept
		for i := 0; i < keep; i++ {
			r -= probs[order[i]]
			if r <= 0 {
				return order[i]
			}
		}
		return order[keep-1]
	}

	r := rng.Float64() * sum
	for i := range probs {
		r -= probs[i]
		if r <= 0 {
			return i
		}
	}
	return len(probs) - 1
}
