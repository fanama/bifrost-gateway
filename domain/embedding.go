package domain

import (
	"context"
	"encoding/json"
	"strings"
)

// Noms partages du runtime d'embedding embarque. Une configuration dont le
// provider vaut ProviderLocal, ou une requete dont le modele vaut
// ModelLocalEmbedding, designe le runtime local : l'UI, le routage et
// l'infra s'alignent sur ces deux constantes pour ne jamais diverger.
const (
	ProviderLocal       = "local"
	ModelLocalEmbedding = "local-embedding"
	// ProviderOnnx designe le moteur ONNX local (pas un provider distant :
	// il n'a ni URL ni cle, l'inference se fait in-process).
	ProviderOnnx = "onnx"
	// ModelOnnxMiniLM est le modele sémantique ONNX embarqué (384 dims).
	ModelOnnxMiniLM = "onnx/all-MiniLM-L6-v2"
)

// EmbeddingChoice decrit une option d'embedding proposee au front du testeur
// et acceptee par l'API /v1/embeddings dans "model". Model est
// l'identifiant unique envoye par le client ; Provider designe le moteur :
// "local" (runtime Go embarque), "onnx" (runtime ONNX in-process), ou un
// provider distant connu (ollama...) resolu via Bifrost.
type EmbeddingChoice struct {
	Model    string
	Provider string
	Label    string
	Hint     string
}

// EmbeddingChoices est la liste curatee des modeles d'embedding. Elle est
// la source unique de verite : le select du testeur la rend telle quelle, et
// le routeur s'en sert pour resoudre un modele demande vers son moteur.
var EmbeddingChoices = []EmbeddingChoice{
	{
		Model:    ModelLocalEmbedding,
		Provider: ProviderLocal,
		Label:    "local-embedding — runtime local (Go pur)",
		Hint:     "Vectoriseur embarqué dans le binaire : aucune dépendance, aucun démarrage de service, lexical (non sémantique).",
	},
	{
		Model:    "nomic-embed-text",
		Provider: "ollama",
		Label:    "nomic-embed-text — Ollama (local)",
		Hint:     "Modèle sémantique servi par Ollama sur localhost (768 dimensions). Nécessite Ollama en marche.",
	},
	{
		Model:    ModelOnnxMiniLM,
		Provider: ProviderOnnx,
		Label:    "all-MiniLM-L6-v2 — ONNX (local)",
		Hint:     "Modèle sémantique ONNX exécuté in-process via le runtime ONNX (384 dimensions).",
	},
}

// FindEmbeddingChoice retourne le choix dont le modele correspond, insensible
// a la casse, ou nil si le modele n'appartient pas a la liste curatee.
func FindEmbeddingChoice(model string) *EmbeddingChoice {
	m := strings.TrimSpace(model)
	if m == "" {
		return nil
	}
	for i := range EmbeddingChoices {
		if strings.EqualFold(EmbeddingChoices[i].Model, m) {
			choice := EmbeddingChoices[i]
			return &choice
		}
	}
	return nil
}

type EmbeddingRequest struct {
	Input          json.RawMessage `json:"input"`
	Model          string          `json:"model,omitempty"`
	EncodingFormat *string         `json:"encoding_format,omitempty"`
	Dimensions     *int            `json:"dimensions,omitempty"`
	User           string          `json:"user,omitempty"`
}

type EmbeddingItem struct {
	Object       string    `json:"object"`
	Embedding    []float32 `json:"embedding,omitempty"`
	EmbeddingStr *string   `json:"-"`
	Index        int       `json:"index"`
}

func (e EmbeddingItem) MarshalJSON() ([]byte, error) {
	type Alias struct {
		Object    string `json:"object"`
		Embedding any    `json:"embedding"`
		Index     int    `json:"index"`
	}
	var emb any = e.Embedding
	if e.EmbeddingStr != nil {
		emb = *e.EmbeddingStr
	}
	if emb == nil {
		emb = []float32{}
	}
	return json.Marshal(Alias{
		Object:    e.Object,
		Embedding: emb,
		Index:     e.Index,
	})
}

type EmbeddingUsage struct {
	PromptTokens int `json:"prompt_tokens"`
	TotalTokens  int `json:"total_tokens"`
}

type EmbeddingResponse struct {
	Object string          `json:"object"`
	Data   []EmbeddingItem `json:"data"`
	Model  string          `json:"model"`
	Usage  EmbeddingUsage  `json:"usage"`
}

type EmbeddingProvider interface {
	Embed(ctx context.Context, cfg *ChatConfig, req *EmbeddingRequest) (*EmbeddingResponse, error)
}
