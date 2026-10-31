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

// scope authenticates the caller and resolves the project it acts on. The
// master key must name a project explicitly; a project key always acts on its
// own project.
func (h *Handler) scope(w http.ResponseWriter, r *http.Request) (string, bool) {
	result, err := h.auth.Authenticate(r.Context(), r.Header.Get("Authorization"))
	if err != nil {
		httpx.WriteUnauthorized(w)
		return "", false
	}
	if result.Scope == "master" {
		id := strings.TrimSpace(r.Header.Get("X-Project-ID"))
		if id == "" {
			id = strings.TrimSpace(r.URL.Query().Get("project_id"))
		}
		if id == "" {
			httpx.WriteAPIError(w, http.StatusBadRequest, httpx.ErrTypeInvalidRequest,
				"project_id", "", "project_id is required when using the master key")
			return "", false
		}
		return id, true
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
