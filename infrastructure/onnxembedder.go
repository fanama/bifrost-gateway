package infrastructure

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"bridge-gateway/domain"
	"github.com/shota3506/onnxruntime-purego/onnxruntime"
)

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
