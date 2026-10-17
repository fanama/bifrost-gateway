package infrastructure

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"bridge-gateway/domain"
	"github.com/shota3506/onnxruntime-purego/onnxruntime"
)

// readOnnxChatModelMeta lit config.json (geometrie KV) et
// generation_config.json (jetons d'arret). Les champs absents prennent des
// defauts coherents avec l'export optimum : heads g toutes egales sans
// num_key_value_heads.
func readOnnxChatModelMeta(base string, sess *onnxruntime.Session, tok *onnxChatTokenizer) (*onnxChatModel, error) {
	cfg, err := readOnnxChatConfig(base)
	if err != nil {
		return nil, err
	}
	kvHeads, headDim, err := chatGeometry(cfg)
	if err != nil {
		return nil, err
	}

	st := &onnxChatModel{
		session: sess,
		tok:     tok,
		kvHeads: kvHeads,
		headDim: headDim,
		layers:  cfg.NumHiddenLayers,
	}
	if st.layers <= 0 {
		// Fallback: the graph declares its layers through its inputs.
		st.layers = len(sess.InputNames()) / 2
	}
	st.eos = readEOSTokens(base)
	return st, nil
}

func readOnnxChatConfig(base string) (*onnxChatConfig, error) {
	raw, err := os.ReadFile(filepath.Join(base, "config.json"))
	if err != nil {
		return nil, fmt.Errorf("onnx chat config: %w", err)
	}
	var cfg onnxChatConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil, fmt.Errorf("onnx chat config: %w", err)
	}
	return &cfg, nil
}

// chatGeometry derives the KV head count and head width, defaulting grouped
// query attention to the full head count when num_key_value_heads is absent.
func chatGeometry(cfg *onnxChatConfig) (kvHeads, headDim int, err error) {
	if cfg.NumAttentionHeads <= 0 || cfg.HiddenSize <= 0 || cfg.HiddenSize%cfg.NumAttentionHeads != 0 {
		return 0, 0, fmt.Errorf("onnx chat config: invalid geometry (heads=%d hidden=%d)", cfg.NumAttentionHeads, cfg.HiddenSize)
	}
	kvHeads = cfg.NumKeyValueHeads
	if kvHeads <= 0 {
		kvHeads = cfg.NumAttentionHeads
	}
	return kvHeads, cfg.HiddenSize / cfg.NumAttentionHeads, nil
}

// readEOSTokens reads stopping IDs; a missing or unreadable file falls back to
// the standard Llama/SmolLM end-of-sequence ID.
func readEOSTokens(base string) map[int]bool {
	eos := map[int]bool{}
	if raw, err := os.ReadFile(filepath.Join(base, "generation_config.json")); err == nil {
		var gen onnxChatGenConfig
		if err := json.Unmarshal(raw, &gen); err == nil {
			for _, v := range parseEOSTokenID(gen.EOSTokenID) {
				eos[v] = true
			}
		}
	}
	if len(eos) == 0 {
		eos[2] = true
	}
	return eos
}

// renderChatPrompt assemble le prompt selon le template de chat du modele
// (SmolLM2, HuggingfaceJBaber) :
//   - system par defaut si la conversation n'ouvre pas sur un system ;
//   - chaque message est encadre par <|im_start|>role ...
//
// Le marqueur final <|im_start|>assistant\n attend la reponse du modele.
func renderChatPrompt(messages []domain.ChatMessage) string {
	var b strings.Builder
	if len(messages) == 0 || !strings.EqualFold(strings.TrimSpace(messages[0].Role), "system") {
		b.WriteString(defaultChatSystemPrompt)
	}
	for _, m := range messages {
		role := strings.ToLower(strings.TrimSpace(m.Role))
		if role == "" {
			role = "user"
		}
		b.WriteString("\u003c|im_start|>" + role + "\n" + m.Content + "\u003c|im_end|>\n")
	}
	b.WriteString("\u003c|im_start|>assistant\n")
	return b.String()
}

// parseEOSTokenID accepte les deux formes d'HF : un entier ou un tableau.
func parseEOSTokenID(n json.Number) []int {
	if s := strings.TrimSpace(n.String()); s != "" {
		var single int
		if err := json.Unmarshal([]byte(s), &single); err == nil {
			return []int{single}
		}
		var many []int
		if err := json.Unmarshal([]byte(s), &many); err == nil {
			return many
		}
	}
	return nil
}
