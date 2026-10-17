package httpx

import (
	"context"

	"bridge-gateway/application"
	"bridge-gateway/domain"
)

// Authenticator validates a Bearer token (master key or project API key).
type Authenticator interface {
	Authenticate(ctx context.Context, bearer string) (*application.AuthResult, error)
}

// Enricher applies the enrichment pipeline to an incoming chat request.
type Enricher interface {
	Enrich(req *domain.ChatRequest) (*domain.EnrichedRequest, error)
}

// ChatCompletion exposes both synchronous and streaming chat completion flows.
type ChatCompletion interface {
	Send(ctx context.Context, cfgID string, history []domain.ChatMessage, message string, opts *domain.RequestOptions) (application.ChatResult, error)
	Stream(ctx context.Context, cfgID string, history []domain.ChatMessage, message string, opts *domain.RequestOptions) (<-chan domain.StreamEvent, error)
}

// ActiveConfigLoader loads the active configuration for a project.
type ActiveConfigLoader interface {
	Active(ctx context.Context, projectID string) (*domain.ChatConfig, error)
}

// ConfigQuerier extends ActiveConfigLoader with full configuration listing.
type ConfigQuerier interface {
	ActiveConfigLoader
	List(ctx context.Context) ([]domain.ChatConfig, error)
}

// ModelLister returns the merged list of known models.
type ModelLister interface {
	List(ctx context.Context) ([]domain.ModelInfo, error)
}

// Embedder computes embeddings using the given configuration.
type Embedder interface {
	EmbedWithConfig(ctx context.Context, cfg *domain.ChatConfig, req *domain.EmbeddingRequest) (*domain.EmbeddingResponse, error)
}
