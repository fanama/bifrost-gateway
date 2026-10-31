package rag

import (
	"context"
	"encoding/json"
	"strings"

	"bridge-gateway/domain"
)

func (s *Service) embedTexts(ctx context.Context, projectID string, texts []string, model string) ([][]float32, string, error) {
	cfg, err := s.embeddingConfig(ctx, projectID, model)
	if err != nil {
		return nil, "", err
	}
	input, err := json.Marshal(texts)
	if err != nil {
		return nil, "", err
	}
	resp, err := s.embedder.Embed(ctx, cfg, &domain.EmbeddingRequest{Input: input, Model: cfg.Model})
	if err != nil {
		return nil, "", err
	}
	vectors, err := orderedVectors(resp, len(texts))
	if err != nil {
		return nil, "", err
	}
	return vectors, cfg.Model, nil
}

func orderedVectors(resp *domain.EmbeddingResponse, count int) ([][]float32, error) {
	if resp == nil || len(resp.Data) != count {
		return nil, &domain.InvalidRequestError{Param: "embedding", Reason: "embedding provider returned an unexpected number of vectors"}
	}
	vectors := make([][]float32, count)
	for _, item := range resp.Data {
		if item.Index < 0 || item.Index >= count {
			return nil, &domain.InvalidRequestError{Param: "embedding", Reason: "embedding index out of range"}
		}
		if len(item.Embedding) == 0 {
			return nil, &domain.InvalidRequestError{Param: "embedding", Reason: "embedding provider returned an empty vector"}
		}
		vectors[item.Index] = item.Embedding
	}
	return vectors, nil
}

// embeddingConfig resolves the model used for RAG. Local embeddings are the
// default: they work offline and need no project configuration. A stored
// configuration is cloned only to reuse its endpoint for remote models.
func (s *Service) embeddingConfig(ctx context.Context, projectID, model string) (*domain.ChatConfig, error) {
	model = strings.TrimSpace(model)
	if model == "" {
		model = domain.ModelLocalEmbedding
	}
	provider := providerFor(model)
	cfg := &domain.ChatConfig{ProjectID: projectID, Provider: provider, Model: model, Active: true}
	if s.configs == nil {
		return cfg, nil
	}
	stored, err := s.configs.GetActiveByProject(ctx, projectID)
	if err != nil || stored == nil {
		return cfg, nil
	}
	clone := *stored
	clone.Provider = provider
	clone.Model = model
	if provider == domain.ProviderLocal || provider == domain.ProviderOnnx {
		clone.BaseURL = ""
		clone.APIKey = ""
	}
	return &clone, nil
}

func providerFor(model string) string {
	switch {
	case model == domain.ModelLocalEmbedding:
		return domain.ProviderLocal
	case strings.HasPrefix(model, domain.OnnxModelPrefix):
		return domain.ProviderOnnx
	default:
		return "ollama"
	}
}
