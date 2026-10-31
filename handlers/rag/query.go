package rag

import (
	"encoding/json"
	"net/http"

	"bridge-gateway/application"
	"bridge-gateway/handlers/httpx"
)

type queryRequest struct {
	Question     string `json:"question"`
	Model        string `json:"model,omitempty"`
	TopK         int    `json:"top_k,omitempty"`
	SystemPrompt string `json:"system_prompt,omitempty"`
}

type searchHit struct {
	DocumentID string  `json:"document_id"`
	FileName   string  `json:"file_name"`
	ChunkIndex int     `json:"chunk_index"`
	Text       string  `json:"text"`
	Score      float64 `json:"score"`
}

// HandleQuery answers a question from the project's indexed passages.
func (h *Handler) HandleQuery(w http.ResponseWriter, r *http.Request) {
	req, projectID, ok := h.decodeQuery(w, r)
	if !ok {
		return
	}
	answer, err := h.rag.Query(r.Context(), projectID, req.Question, req.options())
	if err != nil {
		httpx.WriteDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, answer)
}

// HandleSearch returns the retrieved passages without calling the chat model.
func (h *Handler) HandleSearch(w http.ResponseWriter, r *http.Request) {
	req, projectID, ok := h.decodeQuery(w, r)
	if !ok {
		return
	}
	chunks, err := h.rag.Search(r.Context(), projectID, req.Question, req.options())
	if err != nil {
		httpx.WriteDomainError(w, err)
		return
	}
	hits := make([]searchHit, 0, len(chunks))
	for _, item := range chunks {
		hits = append(hits, searchHit{
			DocumentID: item.Chunk.DocumentID,
			FileName:   item.Chunk.FileName,
			ChunkIndex: item.Chunk.Ordinal,
			Text:       item.Chunk.Text,
			Score:      item.Score,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"chunks": hits, "total": len(hits)})
}

func (h *Handler) decodeQuery(w http.ResponseWriter, r *http.Request) (*queryRequest, string, bool) {
	if r.Method != http.MethodPost {
		httpx.WriteMethodNotAllowed(w)
		return nil, "", false
	}
	projectID, ok := h.scope(w, r)
	if !ok {
		return nil, "", false
	}
	var req queryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteInvalidBody(w)
		return nil, "", false
	}
	return &req, projectID, true
}

func (q *queryRequest) options() application.RAGQueryOptions {
	return application.RAGQueryOptions{
		EmbeddingModel: q.Model,
		TopK:           q.TopK,
		SystemPrompt:   q.SystemPrompt,
	}
}
