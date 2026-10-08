package infrastructure

import (
	"context"
	"fmt"
	"math"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"

	"bridge-gateway/domain"
	"github.com/shota3506/onnxruntime-purego/onnxruntime"
)

const (
	// onnxAPIVersion aligne l'interface C chargee par purego sur la
	// bibliotheque telechargee (ONNX Runtime 1.23.x). Une version
	// differente est refusee au chargement avec un message explicite.
	onnxAPIVersion = 23
	// onnxDefaultMaxSeqLen est la longueur fixe de sequence : padding et
	// troncature a cette taille, le masque d'attention ecarte le remplissage.
	onnxDefaultMaxSeqLen = 128
)

// OnnxEmbedderOptions localise les trois artefacts du runtime ONNX local.
// Chaque chemin surchargeable par variable d'environnement, avec un defaut
// sous models/onnx/ : rien n'est embarque dans le binaire, mais rien n'est
// non plus installe sur le systeme.
type OnnxEmbedderOptions struct {
	LibraryPath string // libonnxruntime.{dylib,so,dll}
	ModelPath   string // fichier .onnx
	VocabPath   string // vocab.txt WordPiece
	MaxSeqLen   int
}

// DefaultOnnxEmbedderOptions resout la configuration depuis l'environnement.
func DefaultOnnxEmbedderOptions() OnnxEmbedderOptions {
	return OnnxEmbedderOptions{
		LibraryPath: envOrDefault("ONNX_RUNTIME_LIB", defaultOnnxLibraryPath()),
		ModelPath:   envOrDefault("ONNX_MODEL_PATH", "models/onnx/model.onnx"),
		VocabPath:   envOrDefault("ONNX_VOCAB_PATH", "models/onnx/vocab.txt"),
		MaxSeqLen:   envIntOrDefault("ONNX_MAX_SEQ_LEN", onnxDefaultMaxSeqLen),
	}
}

func defaultOnnxLibraryPath() string {
	switch runtime.GOOS {
	case "windows":
		return "models/onnx/lib/onnxruntime.dll"
	case "linux":
		return "models/onnx/lib/libonnxruntime.so"
	default:
		return "models/onnx/lib/libonnxruntime.dylib"
	}
}

func envOrDefault(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

func envIntOrDefault(key string, fallback int) int {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return fallback
	}
	return n
}

// OnnxEmbedder execute un modele d'embedding ONNX in-process. La bibliotheque
// ONNX Runtime est chargee en pure Go (purego, sans CGO) depuis un chemin
// configurabe : le binaire reste compatible CGO_ENABLED=0, et un
// environnement sans librairie ni modele recoit une erreur explicite plutot
// qu'un echec silencieux.
//
// Le chargement est paresseux : il n'a lieu qu'au premier appel, et il est
// serialise par un mutex partage avec l'inference, ce qui evite de payer le
// cout d'un modele au demarrage du serveur quand rien ne l'appelle.
type OnnxEmbedder struct {
	opts OnnxEmbedderOptions

	mu      sync.Mutex
	runtime *onnxruntime.Runtime
	env     *onnxruntime.Env
	session *onnxruntime.Session
	tok     *wordPieceTokenizer
	dims    int // decouverts a la premiere inference (forme de sortie)
}

var _ domain.EmbeddingProvider = (*OnnxEmbedder)(nil)

// NewOnnxEmbedder construit le embedder. Les champs vides des options sont
// completees par les defauts, pour qu'un appelant puisse ne preciser que ce
// qu'il veut surcharger.
func NewOnnxEmbedder(opts OnnxEmbedderOptions) *OnnxEmbedder {
	def := DefaultOnnxEmbedderOptions()
	if opts.LibraryPath == "" {
		opts.LibraryPath = def.LibraryPath
	}
	if opts.ModelPath == "" {
		opts.ModelPath = def.ModelPath
	}
	if opts.VocabPath == "" {
		opts.VocabPath = def.VocabPath
	}
	if opts.MaxSeqLen <= 0 {
		opts.MaxSeqLen = def.MaxSeqLen
	}
	return &OnnxEmbedder{opts: opts}
}

// Embed encode un ou plusieurs textes avec le modele ONNX charge.
func (e *OnnxEmbedder) Embed(ctx context.Context, cfg *domain.ChatConfig, req *domain.EmbeddingRequest) (*domain.EmbeddingResponse, error) {
	if req == nil {
		return nil, &domain.InvalidRequestError{Param: "input", Reason: "input is required"}
	}
	texts, err := parseEmbeddingTexts(req.Input)
	if err != nil {
		return nil, err
	}
	encoding, err := resolveEncodingFormat(req)
	if err != nil {
		return nil, err
	}

	model := domain.ModelOnnxMiniLM
	if cfg != nil && strings.TrimSpace(cfg.Model) != "" {
		model = cfg.Model
	}
	if strings.TrimSpace(req.Model) != "" {
		model = req.Model
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	if err := e.loadLocked(); err != nil {
		return nil, err
	}
	vectors, err := e.runLocked(ctx, texts)
	if err != nil {
		return nil, err
	}

	// Le modele produit une dimension fixe : demander plus est une erreur
	// explicite (pas de padding invente), demander moins tronque puis
	// renormalise, comme l'accepte l'API OpenAI.
	dims := len(vectors[0])
	if req.Dimensions != nil && *req.Dimensions > 0 {
		want := *req.Dimensions
		if want > dims {
			return nil, &domain.InvalidRequestError{
				Param:  "dimensions",
				Reason: fmt.Sprintf("model %s produces %d dimensions, cannot expand to %d", model, dims, want),
			}
		}
		for i := range vectors {
			vectors[i] = vectors[i][:want]
			l2Normalize(vectors[i])
		}
	}

	data := make([]domain.EmbeddingItem, len(texts))
	tokens := 0
	for i, vec := range vectors {
		data[i] = newEmbeddingItem(i, vec, encoding)
		tokens += localTokenCount(texts[i])
	}

	return &domain.EmbeddingResponse{
		Object: "list",
		Data:   data,
		Model:  model,
		Usage: domain.EmbeddingUsage{
			PromptTokens: tokens,
			TotalTokens:  tokens,
		},
	}, nil
}

// loadLocked initialise la bibliotheque, la session et le tokenizer au premier
// appel. Chaque manque est signale par un InvalidRequestError portant sur le
// parametre "model" : le testeur l'affiche en toast, l'API le rend en 400 —
// dans les deux cas le client voit exactement quel fichier manque plutot
// qu'une erreur d'infrence obscure.
func (e *OnnxEmbedder) loadLocked() error {
	if e.session != nil {
		return nil
	}

	for _, missing := range []struct {
		label string
		path  string
	}{
		{"onnx runtime library", e.opts.LibraryPath},
		{"onnx model", e.opts.ModelPath},
		{"onnx vocab", e.opts.VocabPath},
	} {
		if _, err := os.Stat(missing.path); err != nil {
			return &domain.InvalidRequestError{
				Param: "model",
				Reason: fmt.Sprintf(
					"onnx runtime not configured: %s not found at %q (set ONNX_RUNTIME_LIB / ONNX_MODEL_PATH / ONNX_VOCAB_PATH)",
					missing.label, missing.path,
				),
			}
		}
	}

	rt, err := onnxruntime.NewRuntime(e.opts.LibraryPath, onnxAPIVersion)
	if err != nil {
		return &domain.InvalidRequestError{Param: "model", Reason: "onnx runtime load failed: " + err.Error()}
	}
	env, err := rt.NewEnv("embedding", onnxruntime.LoggingLevelWarning)
	if err != nil {
		rt.Close()
		return &domain.InvalidRequestError{Param: "model", Reason: "onnx env init failed: " + err.Error()}
	}
	// CPUExecutionProvider est le provider par defaut : nommer explicitement
	// le CPU echoue sur certains builds d'ONNX Runtime ("Unknown provider
	// name"), on laisse donc la liste vide pour obtenir le defaut.
	session, err := rt.NewSession(env, e.opts.ModelPath, &onnxruntime.SessionOptions{})
	if err != nil {
		env.Close()
		rt.Close()
		return &domain.InvalidRequestError{Param: "model", Reason: "onnx session init failed: " + err.Error()}
	}
	tok, err := newWordPieceTokenizer(e.opts.VocabPath, e.opts.MaxSeqLen)
	if err != nil {
		session.Close()
		env.Close()
		rt.Close()
		return &domain.InvalidRequestError{Param: "model", Reason: "onnx tokenizer init failed: " + err.Error()}
	}

	e.runtime = rt
	e.env = env
	e.session = session
	e.tok = tok
	return nil
}

// runLocked tokenise le lot, execute le modele et pool les sorties en un
// vecteur L2-normalise par texte.
func (e *OnnxEmbedder) runLocked(ctx context.Context, texts []string) ([][]float32, error) {
	batch := len(texts)
	seq := e.tok.maxSeqLen

	ids := make([]int64, batch*seq)
	mask := make([]int64, batch*seq)
	types := make([]int64, batch*seq)
	for i, text := range texts {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		tIDs, tMask, tTypes := e.tok.encode(text)
		copy(ids[i*seq:], tIDs)
		copy(mask[i*seq:], tMask)
		copy(types[i*seq:], tTypes)
	}

	// Seuls les inputs reels du modele sont fournis : passer token_type_ids
	// a un modele qui ne les declare pas fait echouer toute l'inference.
	available := make(map[string]bool, 3)
	for _, name := range e.session.InputNames() {
		available[name] = true
	}
	if !available["input_ids"] {
		return nil, &domain.InvalidRequestError{Param: "model", Reason: "onnx model has no input_ids input; unsupported architecture"}
	}

	inputs := map[string]*onnxruntime.Value{}
	closeInputs := func() {
		for _, v := range inputs {
			v.Close()
		}
	}
	var err error
	if inputs["input_ids"], err = onnxruntime.NewTensorValue[int64](e.runtime, ids, []int64{int64(batch), int64(seq)}); err != nil {
		return nil, fmt.Errorf("onnx input_ids: %w", err)
	}
	if available["attention_mask"] {
		if inputs["attention_mask"], err = onnxruntime.NewTensorValue[int64](e.runtime, mask, []int64{int64(batch), int64(seq)}); err != nil {
			closeInputs()
			return nil, fmt.Errorf("onnx attention_mask: %w", err)
		}
	}
	if available["token_type_ids"] {
		if inputs["token_type_ids"], err = onnxruntime.NewTensorValue[int64](e.runtime, types, []int64{int64(batch), int64(seq)}); err != nil {
			closeInputs()
			return nil, fmt.Errorf("onnx token_type_ids: %w", err)
		}
	}
	defer closeInputs()

	outputs, err := e.session.Run(ctx, inputs)
	if err != nil {
		return nil, fmt.Errorf("onnx inference: %w", err)
	}
	defer func() {
		for _, v := range outputs {
			if v != nil {
				v.Close()
			}
		}
	}()

	value, name, err := pickEmbeddingOutput(e.session, outputs)
	if err != nil {
		return nil, err
	}
	data, shape, err := onnxruntime.GetTensorData[float32](value)
	if err != nil {
		return nil, fmt.Errorf("onnx read %s: %w", name, err)
	}

	vectors, err := poolOutput(data, shape, mask, batch, seq)
	if err != nil {
		return nil, err
	}
	for _, vec := range vectors {
		l2Normalize(vec)
	}
	if e.dims == 0 && len(vectors) > 0 {
		e.dims = len(vectors[0])
	}
	return vectors, nil
}

// pickEmbeddingOutput choisit la sortie a pooler, par preference
// sentence_embedding (export optimum, deja pool) puis last_hidden_state
// (export brut, a moyenner), enfin la premiere sortie declaree par le
// modele — l'ordre des sorties est une information du modele, pas un
// iteration de map.
func pickEmbeddingOutput(session *onnxruntime.Session, outputs map[string]*onnxruntime.Value) (*onnxruntime.Value, string, error) {
	for _, want := range []string{"sentence_embedding", "last_hidden_state"} {
		if v, ok := outputs[want]; ok && v != nil {
			return v, want, nil
		}
	}
	for _, name := range session.OutputNames() {
		if v, ok := outputs[name]; ok && v != nil {
			return v, name, nil
		}
	}
	return nil, "", fmt.Errorf("onnx model exposes no usable output (have: %s)", strings.Join(session.OutputNames(), ", "))
}

// poolOutput convertit la sortie du modele en un vecteur par texte :
// [batch, dims] est deja pool (sentence_embedding), [batch, seq, dims] est
// moyenne sur les positions reelles (attention mask), les autres formes sont
// refusees plutot que devinees.
func poolOutput(data []float32, shape, mask []int64, batch, seq int) ([][]float32, error) {
	switch len(shape) {
	case 2:
		if int(shape[0]) != batch {
			return nil, fmt.Errorf("onnx output batch mismatch: %d != %d", shape[0], batch)
		}
		dims := int(shape[1])
		if len(data) != batch*dims {
			return nil, fmt.Errorf("onnx output size mismatch: %d != %d", len(data), batch*dims)
		}
		out := make([][]float32, batch)
		for i := range out {
			vec := make([]float32, dims)
			copy(vec, data[i*dims:(i+1)*dims])
			out[i] = vec
		}
		return out, nil
	case 3:
		if int(shape[0]) != batch || int(shape[1]) != seq {
			return nil, fmt.Errorf("onnx output shape mismatch: %v (batch %d, seq %d)", shape, batch, seq)
		}
		dims := int(shape[2])
		if len(data) != batch*seq*dims {
			return nil, fmt.Errorf("onnx output size mismatch: %d != %d", len(data), batch*seq*dims)
		}
		out := make([][]float32, batch)
		for i := 0; i < batch; i++ {
			vec := make([]float32, dims)
			count := int64(0)
			for s := 0; s < seq; s++ {
				if mask[i*seq+s] == 0 {
					continue
				}
				count++
				base := (i*seq + s) * dims
				for d := 0; d < dims; d++ {
					vec[d] += data[base+d]
				}
			}
			if count > 0 {
				inv := float32(1) / float32(count)
				for d := range vec {
					vec[d] *= inv
				}
			}
			out[i] = vec
		}
		return out, nil
	default:
		return nil, fmt.Errorf("unsupported onnx output rank %d (shape %v)", len(shape), shape)
	}
}

// l2Normalize ramene un vecteur sur la sphere unite : la similarite cosinus
// du testeur et les distances des clients deviennent un simple produit scalaire.
func l2Normalize(vec []float32) {
	var sum float64
	for _, v := range vec {
		sum += float64(v) * float64(v)
	}
	if sum == 0 {
		return
	}
	inv := float32(1 / math.Sqrt(sum))
	for i := range vec {
		vec[i] *= inv
	}
}
