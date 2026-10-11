package infrastructure

import (
	"context"
	"fmt"
	"strings"

	"bridge-gateway/domain"
)

// Noms identifies du runtime local, aliases depuis le domain pour que l'UI
// et le routage partagent exactement les memes valeurs : ils apparaissent
// dans le badge du testeur, le select provider du formulaire de
// configuration, et dans les reponses d'embedding servees au front.
const (
	LocalProviderName      = domain.ProviderLocal
	LocalEmbeddingModel    = domain.ModelLocalEmbedding
	localDefaultDimensions = 384
	localMaxDimensions     = 65536
)

// LocalEmbedder est le runtime d'embedding embarque, entierement en Go :
// aucun serveur externe, aucune dependance CGO (le build release force
// CGO_ENABLED=0), donc le binaire reste autonome.
//
// Le moteur est un vectoriseur par hachage : les features (unigrammes,
// bigrammes de mots, trigrammes de caracteres) sont projetees par FNV-1a dans
// un espace de dimensions fixe, avec un signe derive du hash, puis le vecteur
// est normalise en L2. C'est deterministic, sans poids a charger et assez fin
// pour de la similarite lexicale. Il est place derriere la meme interface
// domain.EmbeddingProvider qu'un modele ONNX plus fin : remplacer embedText
// suffit pour brancher un vrai modele sans toucher au reste de la chaine
// (use case, handler, front HTMX, routage).
type LocalEmbedder struct {
	defaultDimensions int
}

// LocalEmbedderOptions parametre le runtime local.
type LocalEmbedderOptions struct {
	// DefaultDimensions est la dimension produite quand la requete n'en
	// precise pas. 384 suit la convention des petits modeles
	// sentence-transformers.
	DefaultDimensions int
}

func NewLocalEmbedder(opts LocalEmbedderOptions) *LocalEmbedder {
	d := opts.DefaultDimensions
	if d <= 0 {
		d = localDefaultDimensions
	}
	if d > localMaxDimensions {
		d = localMaxDimensions
	}
	return &LocalEmbedder{defaultDimensions: d}
}

var _ domain.EmbeddingProvider = (*LocalEmbedder)(nil)

// Embed encode un ou plusieurs textes localement. La signature est
// exactement celle attendue par EmbeddingUseCase, donc le front HTMX et
// l'API /v1/embeddings n'ont pas besoin de savoir quel runtime repond.
func (e *LocalEmbedder) Embed(ctx context.Context, cfg *domain.ChatConfig, req *domain.EmbeddingRequest) (*domain.EmbeddingResponse, error) {
	if req == nil {
		return nil, &domain.InvalidRequestError{Param: "input", Reason: "input is required"}
	}
	texts, err := parseEmbeddingTexts(req.Input)
	if err != nil {
		return nil, err
	}
	dims, err := e.dimensions(req)
	if err != nil {
		return nil, err
	}
	encoding, err := resolveEncodingFormat(req)
	if err != nil {
		return nil, err
	}

	model := LocalEmbeddingModel
	if cfg != nil && strings.TrimSpace(cfg.Model) != "" {
		model = cfg.Model
	}
	if strings.TrimSpace(req.Model) != "" {
		model = req.Model
	}

	data := make([]domain.EmbeddingItem, len(texts))
	tokens := 0
	for i, text := range texts {
		// Un appel local reste cancellable comme un appel distant : le ctx
		// est verifie entre chaque texte plutot que pendant le calcul.
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		vec := embedText(text, dims)
		tokens += localTokenCount(text)
		data[i] = newEmbeddingItem(i, vec, encoding)
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

// dimensions resout la dimension demandee. Sans demande, la valeur par
// defaut du runtime s'applique ; une demande hors bornes est un 400, pas un
// ecrasement silencieux, car le client a besoin de savoir que son vecteur
// n'a pas la forme attendue.
func (e *LocalEmbedder) dimensions(req *domain.EmbeddingRequest) (int, error) {
	if req.Dimensions == nil || *req.Dimensions == 0 {
		return e.defaultDimensions, nil
	}
	d := *req.Dimensions
	if d < 1 || d > localMaxDimensions {
		return 0, &domain.InvalidRequestError{
			Param:  "dimensions",
			Reason: fmt.Sprintf("must be between 1 and %d", localMaxDimensions),
		}
	}
	return d, nil
}
