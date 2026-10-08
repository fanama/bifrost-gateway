package infrastructure

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"unicode"

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

// resolveEncodingFormat valide encoding_format et rend "float" ou "base64".
// Partage par les moteurs local et ONNX pour que l'API expose les memes
// formats quel que soit le runtime qui repond.
func resolveEncodingFormat(req *domain.EmbeddingRequest) (string, error) {
	if req.EncodingFormat == nil || strings.TrimSpace(*req.EncodingFormat) == "" {
		return "float", nil
	}
	encoding := strings.ToLower(strings.TrimSpace(*req.EncodingFormat))
	if encoding != "float" && encoding != "base64" {
		return "", &domain.InvalidRequestError{Param: "encoding_format", Reason: "only float and base64 are supported"}
	}
	return encoding, nil
}

// newEmbeddingItem construit un item de reponse dans le format demande.
func newEmbeddingItem(index int, vec []float32, encoding string) domain.EmbeddingItem {
	item := domain.EmbeddingItem{Index: index, Object: "embedding"}
	if encoding == "base64" {
		encoded := encodeFloat32Base64(vec)
		item.EmbeddingStr = &encoded
		return item
	}
	item.Embedding = vec
	return item
}

// parseEmbeddingTexts decode la forme OpenAI de "input" : une chaine ou un
// tableau de chaines. Un tableau numerique (embeddings a re-encoder) est
// rejete explicitement : le runtime local n'accepte que du texte, et un
// message clair vaut mieux qu'un echec de unmarshal obscur.
func parseEmbeddingTexts(raw json.RawMessage) ([]string, error) {
	if len(raw) == 0 {
		return nil, &domain.InvalidRequestError{Param: "input", Reason: "input is required"}
	}
	var single string
	if err := json.Unmarshal(raw, &single); err == nil {
		return []string{single}, nil
	}
	var many []string
	if err := json.Unmarshal(raw, &many); err == nil {
		if len(many) == 0 {
			return nil, &domain.InvalidRequestError{Param: "input", Reason: "input is required"}
		}
		return many, nil
	}
	return nil, &domain.InvalidRequestError{
		Param:  "input",
		Reason: "must be a string or an array of strings (numeric input is not supported by the local runtime)",
	}
}

// embedText produit un vecteur L2-normalise de dim dimensions. Un texte vide
// (autorise par l'API) produit un vecteur nul : diviser par une norme nulle
// corromprait le resultat, donc la normalisation est conditionnelle.
func embedText(text string, dim int) []float32 {
	vec := make([]float32, dim)
	for _, feature := range localFeatures(text) {
		h := fnv64a(feature)
		idx := int(h % uint64(dim))
		if h>>63 == 1 {
			vec[idx] -= 1
		} else {
			vec[idx] += 1
		}
	}

	var sum float64
	for _, v := range vec {
		sum += float64(v) * float64(v)
	}
	if sum > 0 {
		inv := float32(1 / math.Sqrt(sum))
		for i := range vec {
			vec[i] *= inv
		}
	}
	return vec
}

// localFeatures decoupe le texte en features de trois niveaux :
// unigrammes (w:), bigrammes adjacents (b:) et trigrammes de caracteres
// (c:) pour amortir la morphologie francaise (paisible / paisiblement).
// Le prefixe de type evite qu'un unigramme et un trigramme identiques
// partagent le meme emplacement.
func localFeatures(text string) []string {
	tokens := localTokens(text)
	features := make([]string, 0, len(tokens)*4)
	for i, tok := range tokens {
		features = append(features, "w:"+tok)
		if i > 0 {
			features = append(features, "b:"+tokens[i-1]+"\x00"+tok)
		}
		runes := []rune(tok)
		if len(runes) >= 4 {
			for j := 0; j+3 <= len(runes); j++ {
				features = append(features, "c:"+string(runes[j:j+3]))
			}
		}
	}
	return features
}

// localTokens decoupe en mots sur les lettres et chiffres, insensible a la
// casse. La decomposition est faite rune par rune pour traiter correctement
// les caracteres accents en UTF-8.
func localTokens(text string) []string {
	var tokens []string
	var cur []rune
	flush := func() {
		if len(cur) > 0 {
			tokens = append(tokens, string(cur))
			cur = cur[:0]
		}
	}
	for _, r := range strings.ToLower(text) {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			cur = append(cur, r)
		} else {
			flush()
		}
	}
	flush()
	return tokens
}

// fnv64a calcule le hash FNV-1a 64 bits sans allocation, ce qui garde
// l'encodage d'un lot de textes lineaire en pratique.
func fnv64a(s string) uint64 {
	const (
		offset64 = 14695981039346656037
		prime64  = 1099511628211
	)
	h := uint64(offset64)
	for i := 0; i < len(s); i++ {
		h ^= uint64(s[i])
		h *= prime64
	}
	return h
}

// localTokenCount estime les tokens par comptage de mots, la meme heuristique
// que handlers/estimateTokens : un client ne doit pas voir deux comptes
// differents selon le runtime qui a repondu.
func localTokenCount(text string) int {
	return len(strings.Fields(text))
}

// encodeFloat32Base64 rend l'encodage base64 (float32 little-endian) attendu
// par encoding_format=base64 de l'API OpenAI.
func encodeFloat32Base64(vec []float32) string {
	buf := make([]byte, 4*len(vec))
	for i, v := range vec {
		binary.LittleEndian.PutUint32(buf[i*4:], math.Float32bits(v))
	}
	return base64.StdEncoding.EncodeToString(buf)
}
