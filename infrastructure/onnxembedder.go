package infrastructure

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
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
	// OnnxModelsDir est la racine des modeles ONNX locaux : chaque
	// repertoire contenant model.onnx et vocab.txt y devient un modele
	// selectable sous l'id "onnx/<repertoire>". Constante exportee pour que
	// le decouverture (main) et l'embedder parlent du meme chemin.
	OnnxModelsDir = "models/onnx"
)

// OnnxEmbedderOptions localise les trois artefacts du runtime ONNX local.
// Chaque chemin surchargeable par variable d'environnement, avec un defaut
// sous models/onnx/ : rien n'est embarque dans le binaire, mais rien n'est
// non plus installe sur le systeme.
type OnnxEmbedderOptions struct {
	LibraryPath string // libonnxruntime.{dylib,so,dll}
	ModelPath   string // fichier .onnx du modele par defaut
	VocabPath   string // vocab.txt WordPiece du modele par defaut
	// ModelsDir est la racine des modeles discovers (repertoires
	// <id>/model.onnx + vocab.txt). Surchargable pour les tests, qui
	// s'executent avec un CWD different de la racine du module.
	ModelsDir string
	MaxSeqLen int
}

// DefaultOnnxEmbedderOptions resout la configuration depuis l'environnement.
func DefaultOnnxEmbedderOptions() OnnxEmbedderOptions {
	return OnnxEmbedderOptions{
		LibraryPath: envOrDefault("ONNX_RUNTIME_LIB", defaultOnnxLibraryPath()),
		ModelPath:   envOrDefault("ONNX_MODEL_PATH", filepath.Join(OnnxModelsDir, "model.onnx")),
		VocabPath:   envOrDefault("ONNX_VOCAB_PATH", filepath.Join(OnnxModelsDir, "vocab.txt")),
		ModelsDir:   OnnxModelsDir,
		MaxSeqLen:   envIntOrDefault("ONNX_MAX_SEQ_LEN", onnxDefaultMaxSeqLen),
	}
}

func defaultOnnxLibraryPath() string {
	switch runtime.GOOS {
	case "windows":
		return filepath.Join(OnnxModelsDir, "lib", "onnxruntime.dll")
	case "linux":
		return filepath.Join(OnnxModelsDir, "lib", "libonnxruntime.so")
	default:
		return filepath.Join(OnnxModelsDir, "lib", "libonnxruntime.dylib")
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
// Le chargement est paresseux : chaque modele n'est charge qu'a son premier
// appel, et tout est serialise par un mutex partage avec l'inference, ce qui
// evite de payer le cout d'un modele au demarrage du serveur quand rien ne
// l'appelle. Les modeles cohabitent ensuite en memoire, chacun avec sa
// session et son tokenizer propres.
type OnnxEmbedder struct {
	opts OnnxEmbedderOptions

	mu       sync.Mutex
	runtime  *onnxruntime.Runtime
	env      *onnxruntime.Env
	sessions map[string]*onnxModelState // etat charge, par identifiant de modele
}

// onnxModelState regroupe la session et le tokenizer d'un modele charge. La
// forme de sortie (dims) y est decouverte a la premiere inference : deux
// modeles du repertoire peuvent produire des dimensions differentes.
type onnxModelState struct {
	session *onnxruntime.Session
	tok     *wordPieceTokenizer
	dims    int
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
	if opts.ModelsDir == "" {
		opts.ModelsDir = def.ModelsDir
	}
	if opts.MaxSeqLen <= 0 {
		opts.MaxSeqLen = def.MaxSeqLen
	}
	return &OnnxEmbedder{opts: opts}
}

// Embed encode un ou plusieurs textes avec le modele ONNX designe par la
// requete (par defaut le modele curate onnx/all-MiniLM-L6-v2) : chaque
// identifiant resout vers ses propres artefacts et sa propre session.
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

	state, err := e.stateLocked(model)
	if err != nil {
		return nil, err
	}
	vectors, err := e.runLocked(ctx, texts, state)
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

// pathsFor resout l'identifiant d'un modele vers ses deux artefacts. Le
// modele par defaut conserve les chemins plats resolus depuis
// l'environnement (ONNX_MODEL_PATH / ONNX_VOCAB_PATH) ; tout autre
// identifiant designe un repertoire de OnnxModelsDir, convention qui rend les
// modeles locaux decouvrables sans configuration.
func (e *OnnxEmbedder) pathsFor(modelID string) (modelPath, vocabPath string, err error) {
	id := strings.TrimSpace(modelID)
	if id == "" {
		id = domain.ModelOnnxMiniLM
	}
	if id == domain.ModelOnnxMiniLM {
		return e.opts.ModelPath, e.opts.VocabPath, nil
	}
	rel := strings.TrimPrefix(id, domain.OnnxModelPrefix)
	// Un identifiant ne designe qu'un repertoire direct : ni sous-chemin, ni
	// remontee d'arborescence — l'id vient d'une requete client.
	if rel == "" || rel == "." || rel == ".." ||
		strings.Contains(rel, "..") || strings.ContainsAny(rel, `/\`) {
		return "", "", &domain.InvalidRequestError{
			Param:  "model",
			Reason: fmt.Sprintf("onnx model id invalid: %q (expected %s<repertoire> under %s/)", id, domain.OnnxModelPrefix, e.opts.ModelsDir),
		}
	}
	base := filepath.Join(e.opts.ModelsDir, rel)
	return filepath.Join(base, "model.onnx"), filepath.Join(base, "vocab.txt"), nil
}

// stateLocked retourne l'etat charge du modele, cree au premier appel. La
// bibliotheque et l'environnement ONNX sont partages par tous les modeles :
// seule la session et le tokenizer sont propres a chacun. Chaque manque est
// signale par un InvalidRequestError portant sur le parametre "model" : le
// testeur l'affiche en toast, l'API le rend en 400 — dans les deux cas le
// client voit exactement quel fichier manque plutot qu'une erreur d'infrence
// obscure.
func (e *OnnxEmbedder) stateLocked(modelID string) (*onnxModelState, error) {
	key := strings.TrimSpace(modelID)
	if key == "" {
		key = domain.ModelOnnxMiniLM
	}
	if state, ok := e.sessions[key]; ok {
		return state, nil
	}

	// L'identifiant est valide avant toute lecture disque : un id invalide
	// est reconnu memes sans bibliotheque ni modele installes.
	modelPath, vocabPath, err := e.pathsFor(key)
	if err != nil {
		return nil, err
	}

	if e.runtime == nil {
		if _, err := os.Stat(e.opts.LibraryPath); err != nil {
			return nil, &domain.InvalidRequestError{
				Param: "model",
				Reason: fmt.Sprintf(
					"onnx runtime not configured: onnx runtime library not found at %q (set ONNX_RUNTIME_LIB)",
					e.opts.LibraryPath,
				),
			}
		}
		rt, err := onnxruntime.NewRuntime(e.opts.LibraryPath, onnxAPIVersion)
		if err != nil {
			return nil, &domain.InvalidRequestError{Param: "model", Reason: "onnx runtime load failed: " + err.Error()}
		}
		env, err := rt.NewEnv("embedding", onnxruntime.LoggingLevelWarning)
		if err != nil {
			rt.Close()
			return nil, &domain.InvalidRequestError{Param: "model", Reason: "onnx env init failed: " + err.Error()}
		}
		e.runtime = rt
		e.env = env
	}

	hint := "set ONNX_MODEL_PATH / ONNX_VOCAB_PATH"
	if key != domain.ModelOnnxMiniLM {
		hint = fmt.Sprintf("a local model needs %s/<repertoire>/{model.onnx,vocab.txt}", e.opts.ModelsDir)
	}
	for _, missing := range []struct {
		label string
		path  string
	}{
		{"onnx model", modelPath},
		{"onnx vocab", vocabPath},
	} {
		if _, err := os.Stat(missing.path); err != nil {
			return nil, &domain.InvalidRequestError{
				Param:  "model",
				Reason: fmt.Sprintf("onnx model not configured: %s not found at %q (%s)", missing.label, missing.path, hint),
			}
		}
	}

	// CPUExecutionProvider est le provider par defaut : nommer explicitement
	// le CPU echoue sur certains builds d'ONNX Runtime ("Unknown provider
	// name"), on laisse donc la liste vide pour obtenir le defaut.
	session, err := e.runtime.NewSession(e.env, modelPath, &onnxruntime.SessionOptions{})
	if err != nil {
		return nil, &domain.InvalidRequestError{Param: "model", Reason: "onnx session init failed: " + err.Error()}
	}
	tok, err := newWordPieceTokenizer(vocabPath, e.opts.MaxSeqLen)
	if err != nil {
		session.Close()
		return nil, &domain.InvalidRequestError{Param: "model", Reason: "onnx tokenizer init failed: " + err.Error()}
	}

	state := &onnxModelState{session: session, tok: tok}
	if e.sessions == nil {
		e.sessions = make(map[string]*onnxModelState, 2)
	}
	e.sessions[key] = state
	return state, nil
}

// runLocked tokenise le lot, execute le modele designe par state et pool les
// sorties en un vecteur L2-normalise par texte.
func (e *OnnxEmbedder) runLocked(ctx context.Context, texts []string, state *onnxModelState) ([][]float32, error) {
	batch := len(texts)
	seq := state.tok.maxSeqLen

	ids := make([]int64, batch*seq)
	mask := make([]int64, batch*seq)
	types := make([]int64, batch*seq)
	for i, text := range texts {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		tIDs, tMask, tTypes := state.tok.encode(text)
		copy(ids[i*seq:], tIDs)
		copy(mask[i*seq:], tMask)
		copy(types[i*seq:], tTypes)
	}

	// Seuls les inputs reels du modele sont fournis : passer token_type_ids
	// a un modele qui ne les declare pas fait echouer toute l'inference.
	available := make(map[string]bool, 3)
	for _, name := range state.session.InputNames() {
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

	outputs, err := state.session.Run(ctx, inputs)
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

	value, name, err := pickEmbeddingOutput(state.session, outputs)
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
	if state.dims == 0 && len(vectors) > 0 {
		state.dims = len(vectors[0])
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
