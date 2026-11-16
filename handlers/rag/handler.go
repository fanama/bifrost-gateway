// Package rag is the HTTP delivery layer for the RAG bounded context.
package rag

import (
	"encoding/json"
	"net/http"
	"strings"

	"bridge-gateway/handlers/httpx"
)

// Handler translates HTTP requests into RAG use case calls.
type Handler struct {
	auth httpx.Authenticator
	rag  httpx.RAGService
}

// NewHandler builds the delivery handler from consumer-side ports.
func NewHandler(auth httpx.Authenticator, service httpx.RAGService) *Handler {
	return &Handler{auth: auth, rag: service}
}

// scope authenticates the caller and binds the request to the project carried
// by its API key. RAG routes never accept the master key or a project chosen by
// the client: the token is the only source of tenancy.
func (h *Handler) scope(w http.ResponseWriter, r *http.Request) (string, bool) {
	result, err := h.auth.Authenticate(r.Context(), r.Header.Get("Authorization"))
	if err != nil || result.Scope != "project" {
		httpx.WriteUnauthorized(w)
		return "", false
	}
	if result.Project == nil || strings.TrimSpace(result.Project.ID) == "" {
		httpx.WriteUnauthorized(w)
		return "", false
	}
	return result.Project.ID, true
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
