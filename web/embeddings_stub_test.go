package web

import (
	"context"
	"errors"
	"testing"

	"bridge-gateway/domain"
)

type stubEmbeddingService struct {
	resp *domain.EmbeddingResponse
	err  error
	cfg  *domain.ChatConfig
	req  *domain.EmbeddingRequest
}

func (s *stubEmbeddingService) EmbedWithConfig(_ context.Context, cfg *domain.ChatConfig, req *domain.EmbeddingRequest) (*domain.EmbeddingResponse, error) {
	s.cfg = cfg
	s.req = req
	return s.resp, s.err
}

func TestRunEmbeddingForwardsChoiceAndReturnsLatency(t *testing.T) {
	service := &stubEmbeddingService{resp: &domain.EmbeddingResponse{
		Model: "onnx/all-MiniLM-L6-v2",
		Data:  []domain.EmbeddingItem{{Index: 0, Embedding: []float32{0.1, 0.2}}},
	}}
	controller := embeddingsController{embeddings: service}
	choice := &domain.EmbeddingChoice{Provider: "onnx", Model: "onnx/all-MiniLM-L6-v2"}

	resp, latency, msg := controller.runEmbedding(context.Background(), choice, &domain.EmbeddingRequest{Input: []byte(`"hello"`)})
	if msg != "" {
		t.Fatalf("unexpected message: %s", msg)
	}
	if resp != service.resp {
		t.Fatalf("response was not forwarded")
	}
	if latency < 0 {
		t.Fatalf("latency = %d, want >= 0", latency)
	}
	if service.cfg == nil || service.cfg.Provider != "onnx" || service.cfg.Model != choice.Model {
		t.Fatalf("configuration not derived from choice: %+v", service.cfg)
	}
	if service.req == nil || string(service.req.Input) != `"hello"` {
		t.Fatalf("request not forwarded: %+v", service.req)
	}
}

func TestRunEmbeddingReportsEmptyDataAndProviderErrors(t *testing.T) {
	controller := embeddingsController{embeddings: &stubEmbeddingService{resp: &domain.EmbeddingResponse{}}}
	choice := &domain.EmbeddingChoice{Provider: "onnx", Model: "onnx/m"}

	if _, _, msg := controller.runEmbedding(context.Background(), choice, &domain.EmbeddingRequest{}); msg == "" {
		t.Fatal("expected a message for an empty embedding response")
	}

	controller.embeddings = &stubEmbeddingService{err: errors.New("provider down")}
	if _, _, msg := controller.runEmbedding(context.Background(), choice, &domain.EmbeddingRequest{}); msg != "provider down" {
		t.Fatalf("message = %q, want the provider error", msg)
	}
}
