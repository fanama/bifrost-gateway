package rag

import (
	"net/http"

	"bridge-gateway/handlers/httpx"
)

// HandleIndexStatus returns the per-project index state.
func (h *Handler) HandleIndexStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httpx.WriteMethodNotAllowed(w)
		return
	}
	projectID, ok := h.scope(w, r)
	if !ok {
		return
	}
	status, err := h.rag.IndexStatus(r.Context(), projectID)
	if err != nil {
		httpx.WriteDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, status)
}

// HandleIndexRebuild starts a background rebuild and returns 202 with the
// current status. Callers poll HandleIndexStatus for completion.
func (h *Handler) HandleIndexRebuild(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httpx.WriteMethodNotAllowed(w)
		return
	}
	projectID, ok := h.scope(w, r)
	if !ok {
		return
	}
	status, err := h.rag.RebuildIndex(r.Context(), projectID)
	if err != nil {
		httpx.WriteDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, status)
}
