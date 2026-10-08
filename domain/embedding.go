package domain

import (
	"context"
	"encoding/json"
)

// Noms partages du runtime d'embedding embarque. Une configuration dont le
// provider vaut ProviderLocal, ou une requete dont le modele vaut
// ModelLocalEmbedding, designe le runtime local : l'UI, le routage et
// l'infra s'alignent sur ces deux constantes pour ne jamais diverger.
const (
	ProviderLocal       = "local"
	ModelLocalEmbedding = "local-embedding"
)

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
