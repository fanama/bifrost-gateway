package rag

import (
	"io"
	"net/http"
	"strings"

	"bridge-gateway/application"
	"bridge-gateway/domain"
	"bridge-gateway/handlers/httpx"
)

const maxUploadBytes = 25 << 20

// HandleDocuments serves GET (list) and POST (multipart upload) on
// /v1/rag/documents.
func (h *Handler) HandleDocuments(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		h.listDocuments(w, r)
	case http.MethodPost:
		h.uploadDocument(w, r)
	default:
		httpx.WriteMethodNotAllowed(w)
	}
}

func (h *Handler) listDocuments(w http.ResponseWriter, r *http.Request) {
	projectID, ok := h.scope(w, r)
	if !ok {
		return
	}
	docs, err := h.rag.Documents(r.Context(), projectID)
	if err != nil {
		httpx.WriteDomainError(w, err)
		return
	}
	if docs == nil {
		docs = []domain.Document{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"documents": docs, "total": len(docs)})
}

func (h *Handler) uploadDocument(w http.ResponseWriter, r *http.Request) {
	projectID, ok := h.scope(w, r)
	if !ok {
		return
	}
	if err := r.ParseMultipartForm(maxUploadBytes); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "multipart form with a 'file' field is required")
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "multipart form with a 'file' field is required")
		return
	}
	defer file.Close()

	content, err := io.ReadAll(io.LimitReader(file, maxUploadBytes+1))
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "cannot read uploaded file")
		return
	}
	if len(content) > maxUploadBytes {
		httpx.WriteError(w, http.StatusRequestEntityTooLarge, "file exceeds the upload limit")
		return
	}
	opts := application.RAGIngestOptions{EmbeddingModel: r.FormValue("model")}
	doc, err := h.rag.Ingest(r.Context(), projectID, header.Filename, header.Header.Get("Content-Type"), content, opts)
	if err != nil {
		httpx.WriteDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, doc)
}

// HandleDocumentByID serves DELETE /v1/rag/documents/{id}.
func (h *Handler) HandleDocumentByID(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		httpx.WriteMethodNotAllowed(w)
		return
	}
	projectID, ok := h.scope(w, r)
	if !ok {
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/v1/rag/documents/")
	id = strings.Trim(id, "/")
	if id == "" || strings.Contains(id, "/") {
		httpx.WriteError(w, http.StatusNotFound, "document not found")
		return
	}
	if err := h.rag.DeleteDocument(r.Context(), projectID, id); err != nil {
		httpx.WriteDomainError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
