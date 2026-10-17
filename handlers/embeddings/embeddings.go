package embeddings

import (
	"context"
	"encoding/json"
	"net/http"

	"bridge-gateway/application"
	"bridge-gateway/domain"
	"bridge-gateway/domain/errs"
	"bridge-gateway/handlers/httpx"
)

// EmbeddingHandler serves POST /v1/embeddings.
type EmbeddingHandler struct {
	auth       httpx.Authenticator
	embeddings httpx.Embedder
	configs    httpx.ConfigQuerier
}

// NewEmbeddingHandler constructs an EmbeddingHandler with consumer-side ports.
func NewEmbeddingHandler(
	auth httpx.Authenticator,
	embeddings httpx.Embedder,
	configs httpx.ConfigQuerier,
) *EmbeddingHandler {
	return &EmbeddingHandler{auth: auth, embeddings: embeddings, configs: configs}
}

// HandleEmbedding handles POST /v1/embeddings.
func (h *EmbeddingHandler) HandleEmbedding(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httpx.WriteMethodNotAllowed(w)
		return
	}

	authResult, err := h.auth.Authenticate(r.Context(), r.Header.Get("Authorization"))
	if err != nil {
		httpx.WriteUnauthorized(w)
		return
	}

	var req domain.EmbeddingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteInvalidBody(w)
		return
	}
	if len(req.Input) == 0 {
		httpx.WriteAPIError(w, http.StatusBadRequest, httpx.ErrTypeInvalidRequest, "input", "", "input is required")
		return
	}

	cfg, err := h.resolveConfig(r.Context(), authResult)
	if err != nil {
		httpx.WriteDomainError(w, err)
		return
	}

	resp, err := h.embeddings.EmbedWithConfig(r.Context(), cfg, &req)
	if err != nil {
		httpx.WriteDomainError(w, err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func (h *EmbeddingHandler) resolveConfig(ctx context.Context, authResult *application.AuthResult) (*domain.ChatConfig, error) {
	if authResult.Scope == "project" {
		return h.configs.Active(ctx, authResult.Project.ID)
	}
	list, err := h.configs.List(ctx)
	if err != nil || len(list) == 0 {
		return nil, errs.New(errs.CodeInvalidRequest, "no configuration available for embedding")
	}
	for i := range list {
		if list[i].Active {
			return &list[i], nil
		}
	}
	return &list[0], nil
}
