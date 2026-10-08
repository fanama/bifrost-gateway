package infrastructure

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"bridge-gateway/domain"
	"github.com/shota3506/onnxruntime-purego/onnxruntime"
)

// OnnxChatModelsDir est la racine des modeles de chat ONNX : chaque
// repertoire contenant model.onnx + tokenizer.json + config.json devient une
// config de chat provider "onnx" executable in-process.
const OnnxChatModelsDir = OnnxModelsDir + "/chat"

// defaultChatSystemPrompt est le system par defaut du template SmolLM2,
// insere quand la conversation ne commence pas par un message system.
const defaultChatSystemPrompt = "<|im_start|>system\nYou are a helpful AI assistant named SmolLM, trained by Hugging Face\n"

// OnnxChatOptions localise les artefacts du runtime de chat. Les champs vides
// prennent les defauts ; ModelsDir est surchargeable pour les tests.
type OnnxChatOptions struct {
	LibraryPath  string
	ModelsDir    string
	MaxSeqLen    int // fenetre de contexte cumulee (prompt + generation)
	MaxNewTokens int // longueur de generation par defaut
}

func DefaultOnnxChatOptions() OnnxChatOptions {
	return OnnxChatOptions{
		LibraryPath:  envOrDefault("ONNX_CHAT_RUNTIME_LIB", defaultOnnxLibraryPath()),
		ModelsDir:    OnnxChatModelsDir,
		MaxSeqLen:    envIntOrDefault("ONNX_CHAT_MAX_SEQ_LEN", 2048),
		MaxNewTokens: envIntOrDefault("ONNX_CHAT_MAX_NEW_TOKENS", 256),
	}
}

// onnxChatModel est l'etat charge d'un modele de chat : session, tokenizer,
// geometrie KV (tires de config.json) et jetons d'arret.
type onnxChatModel struct {
	session *onnxruntime.Session
	tok     *onnxChatTokenizer
	kvHeads int
	headDim int
	layers  int
	eos     map[int]bool
}

// onnxChatConfig reflete les champs de config.json necessaires au cache.
type onnxChatConfig struct {
	NumKeyValueHeads  int `json:"num_key_value_heads"`
	NumAttentionHeads int `json:"num_attention_heads"`
	HiddenSize        int `json:"hidden_size"`
	NumHiddenLayers   int `json:"num_hidden_layers"`
}

type onnxChatGenConfig struct {
	EOSTokenID json.Number `json:"eos_token_id"` // int ou [int]
}

// LoadOnnxChatModel dira si un repertoire est un modele de chat utilisable.
func IsOnnxChatModelDir(dir string) bool {
	for _, f := range []string{"model.onnx", "tokenizer.json", "config.json"} {
		if !isOnnxArtifact(dir, f) {
			return false
		}
	}
	return true
}

// ListOnnxChatModels decouvre les modeles de chat sous dir (defaut :
// models/onnx/chat/), tries par nom pour un affichage stable.
func ListOnnxChatModels(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var ids []string
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		if IsOnnxChatModelDir(filepath.Join(dir, e.Name())) {
			ids = append(ids, e.Name())
		}
	}
	sort.Strings(ids)
	return ids, nil
}

// OnnxChatProvider execute un modele de chat ONNX in-process, sans CGO ni
// service compagnon. Le chargement est paresseux et serialise : chaque modele
// a sa session et son tokenizer, la bibliotheque ONNX est partagee avec le
// embedder s'il tourne dans le meme processus.
type OnnxChatProvider struct {
	opts OnnxChatOptions

	mu      sync.Mutex
	runtime *onnxruntime.Runtime
	env     *onnxruntime.Env
	models  map[string]*onnxChatModel
}

// NewOnnxChatProvider complete les options vides avec les defauts.
func NewOnnxChatProvider(opts OnnxChatOptions) *OnnxChatProvider {
	def := DefaultOnnxChatOptions()
	if opts.LibraryPath == "" {
		opts.LibraryPath = def.LibraryPath
	}
	if opts.ModelsDir == "" {
		opts.ModelsDir = def.ModelsDir
	}
	if opts.MaxSeqLen <= 0 {
		opts.MaxSeqLen = def.MaxSeqLen
	}
	if opts.MaxNewTokens <= 0 {
		opts.MaxNewTokens = def.MaxNewTokens
	}
	return &OnnxChatProvider{opts: opts, models: map[string]*onnxChatModel{}}
}

// stateLocked retourne l'etat charge du modele (creation au premier appel).
// modelID est le nom du repertoire sous models/onnx/chat/ ; les chemins
// manquants sont des erreurs explicites citant le fichier attendu.
func (p *OnnxChatProvider) stateLocked(modelID string) (*onnxChatModel, error) {
	id := strings.TrimSpace(modelID)
	if id == "" || strings.ContainsAny(id, `/\\`) || strings.Contains(id, "..") {
		return nil, fmt.Errorf("onnx chat model id invalid: %q (expected a directory under %s)", modelID, p.opts.ModelsDir)
	}
	if st, ok := p.models[id]; ok {
		return st, nil
	}
	base := filepath.Join(p.opts.ModelsDir, id)
	for _, f := range []string{"model.onnx", "tokenizer.json", "config.json"} {
		if !isOnnxArtifact(base, f) {
			return nil, fmt.Errorf("onnx chat model not configured: %s not found at %q (expected %s/{model.onnx,tokenizer.json,config.json})", f, filepath.Join(base, f), base)
		}
	}

	if p.runtime == nil {
		if _, err := os.Stat(p.opts.LibraryPath); err != nil {
			return nil, fmt.Errorf("onnx runtime not configured: library not found at %q (set ONNX_RUNTIME_LIB)", p.opts.LibraryPath)
		}
		rt, err := onnxruntime.NewRuntime(p.opts.LibraryPath, onnxAPIVersion)
		if err != nil {
			return nil, fmt.Errorf("onnx runtime load failed: %w", err)
		}
		env, err := rt.NewEnv("chat", onnxruntime.LoggingLevelWarning)
		if err != nil {
			rt.Close()
			return nil, fmt.Errorf("onnx env init failed: %w", err)
		}
		p.runtime = rt
		p.env = env
	}

	sess, err := p.runtime.NewSession(p.env, filepath.Join(base, "model.onnx"), &onnxruntime.SessionOptions{})
	if err != nil {
		return nil, fmt.Errorf("onnx chat session init failed: %w", err)
	}
	tok, err := newOnnxChatTokenizer(filepath.Join(base, "tokenizer.json"))
	if err != nil {
		sess.Close()
		return nil, err
	}
	st, err := readOnnxChatModelMeta(base, sess, tok)
	if err != nil {
		sess.Close()
		return nil, err
	}
	p.models[id] = st
	return st, nil
}

// readOnnxChatModelMeta lit config.json (geometrie KV) et
// generation_config.json (jetons d'arret). Les champs absents prennent des
// defauts coherents avec l'export optimum : heads g toutes egales sans
// num_key_value_heads.
func readOnnxChatModelMeta(base string, sess *onnxruntime.Session, tok *onnxChatTokenizer) (*onnxChatModel, error) {
	raw, err := os.ReadFile(filepath.Join(base, "config.json"))
	if err != nil {
		return nil, fmt.Errorf("onnx chat config: %w", err)
	}
	var cfg onnxChatConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil, fmt.Errorf("onnx chat config: %w", err)
	}
	if cfg.NumAttentionHeads <= 0 || cfg.HiddenSize <= 0 || cfg.HiddenSize%cfg.NumAttentionHeads != 0 {
		return nil, fmt.Errorf("onnx chat config: invalid geometry (heads=%d hidden=%d)", cfg.NumAttentionHeads, cfg.HiddenSize)
	}
	kvHeads := cfg.NumKeyValueHeads
	if kvHeads <= 0 {
		kvHeads = cfg.NumAttentionHeads
	}
	st := &onnxChatModel{
		session: sess,
		tok:     tok,
		kvHeads: kvHeads,
		headDim: cfg.HiddenSize / cfg.NumAttentionHeads,
		layers:  cfg.NumHiddenLayers,
		eos:     map[int]bool{},
	}
	if st.layers <= 0 {
		// Repli : le graphe declare lui-meme ses couches via ses inputs.
		st.layers = len(sess.InputNames()) / 2
	}
	if raw, err := os.ReadFile(filepath.Join(base, "generation_config.json")); err == nil {
		var gen onnxChatGenConfig
		if err := json.Unmarshal(raw, &gen); err == nil {
			for _, v := range parseEOSTokenID(gen.EOSTokenID) {
				st.eos[v] = true
			}
		}
	}
	if len(st.eos) == 0 {
		st.eos[2] = true // dernier recours : id de fin standard Llama/SmolLM
	}
	return st, nil
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
