package infrastructure

import (
	"context"
	"fmt"
	"math"
	"math/rand"
	"sort"
	"time"

	"bridge-gateway/domain"
)

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

	promptIDs, err := promptTokenIDs(st.tok, messages, s.maxNew, p.opts.MaxSeqLen)
	if err != nil {
		return "", err
	}
	kv := newOnnxKVCache(st.layers, st.kvHeads, st.headDim)
	logits, err := p.step(ctx, st, kv, promptIDs)
	if err != nil {
		return "", err
	}

	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	return p.decodeTokens(ctx, st, kv, logits, s, rng, emit)
}

// promptTokenIDs encodes the chat prompt and keeps its tail when the generation
// budget would overflow the context window; recent messages win.
func promptTokenIDs(tok *onnxChatTokenizer, messages []domain.ChatMessage, maxNew, maxSeq int) ([]int64, error) {
	ids := tok.Encode(renderChatPrompt(messages))
	if len(ids) == 0 {
		return nil, fmt.Errorf("onnx chat: empty prompt")
	}
	if overflow := len(ids) + maxNew - maxSeq; overflow > 0 {
		if overflow >= len(ids) {
			overflow = len(ids) - 1
		}
		ids = ids[overflow:]
	}
	out := make([]int64, len(ids))
	for i, id := range ids {
		out[i] = int64(id)
	}
	return out, nil
}

// decodeTokens samples until end-of-sequence or the token budget, emitting the
// stable decoded text after each step.
func (p *OnnxChatProvider) decodeTokens(
	ctx context.Context,
	st *onnxChatModel,
	kv *onnxKVCache,
	logits []float32,
	s onnxChatSettings,
	rng *rand.Rand,
	emit func(string) error,
) (string, error) {
	gen := make([]int, 0, s.maxNew)
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
		var err error
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

	probs, sum := softmaxWithTemperature(logits, s.temp)
	if tok, ok := sampleTopP(probs, sum, s.topP, rng); ok {
		return tok
	}
	return sampleDistribution(probs, sum, rng)
}

// softmaxWithTemperature exponentiates centered logits, which avoids overflow
// on extreme scores.
func softmaxWithTemperature(logits []float32, temp float64) ([]float64, float64) {
	probs := make([]float64, len(logits))
	maxLogit := float64(logits[0])
	for _, l := range logits {
		if float64(l) > maxLogit {
			maxLogit = float64(l)
		}
	}
	var sum float64
	for i, l := range logits {
		probs[i] = math.Exp((float64(l) - maxLogit) / temp)
		sum += probs[i]
	}
	return probs, sum
}

// sampleTopP keeps the smallest candidate set covering topP of the mass,
// renormalizes it, and draws from it.
func sampleTopP(probs []float64, sum, topP float64, rng *rand.Rand) (int, bool) {
	if topP <= 0 || topP >= 1 || len(probs) <= 1 {
		return 0, false
	}
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
		if cum >= topP {
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
			return order[i], true
		}
	}
	return order[keep-1], true
}

func sampleDistribution(probs []float64, sum float64, rng *rand.Rand) int {
	r := rng.Float64() * sum
	for i := range probs {
		r -= probs[i]
		if r <= 0 {
			return i
		}
	}
	return len(probs) - 1
}
