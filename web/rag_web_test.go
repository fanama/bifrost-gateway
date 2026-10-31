package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"bridge-gateway/application"
	"bridge-gateway/domain"
)

type stubProjects struct{}

func (stubProjects) List(context.Context) ([]domain.Project, error) {
	return []domain.Project{{ID: "p1", Name: "Projet RAG"}}, nil
}
func (stubProjects) Get(context.Context, string) (*domain.Project, error) {
	return &domain.Project{ID: "p1", Name: "Projet RAG"}, nil
}
func (stubProjects) Create(context.Context, string, string) (*domain.Project, error) {
	return &domain.Project{ID: "p1", Name: "Projet RAG"}, nil
}

type stubModels struct{}

func (stubModels) List(context.Context) ([]domain.ModelInfo, error) { return nil, nil }
func (stubModels) ListForProvider(context.Context, string) ([]domain.ModelInfo, error) {
	return nil, nil
}

type stubRAG struct {
	saved   *domain.RAGSettings
	queries []application.RAGQueryOptions
}

func (s *stubRAG) Settings(context.Context, string) (*domain.RAGSettings, error) {
	settings := domain.RAGSettings{ProjectID: "p1", EmbeddingModel: domain.ModelLocalEmbedding, TopK: 4, ChunkSize: 1000, ChunkOverlap: 200}
	return &settings, nil
}
func (s *stubRAG) SaveSettings(_ context.Context, settings *domain.RAGSettings) (*domain.RAGSettings, error) {
	copy := *settings
	s.saved = &copy
	return &copy, nil
}
func (s *stubRAG) Ingest(context.Context, string, string, string, []byte, application.RAGIngestOptions) (*domain.Document, error) {
	return &domain.Document{ID: "d1", FileName: "notes.txt", ChunkCount: 1}, nil
}
func (s *stubRAG) Documents(context.Context, string) ([]domain.Document, error) {
	return []domain.Document{{ID: "d1", FileName: "notes.txt", ChunkCount: 1, EmbeddingModel: domain.ModelLocalEmbedding}}, nil
}
func (s *stubRAG) DeleteDocument(context.Context, string, string) error { return nil }
func (s *stubRAG) Query(context.Context, string, string, application.RAGQueryOptions) (*domain.Answer, error) {
	return &domain.Answer{Answer: "Orion [1]", HasContext: true, Model: "test"}, nil
}
func (s *stubRAG) Search(_ context.Context, _ string, _ string, opts application.RAGQueryOptions) ([]domain.RetrievedChunk, error) {
	s.queries = append(s.queries, opts)
	return []domain.RetrievedChunk{{
		Chunk: domain.Chunk{FileName: "notes.txt", Ordinal: 0, Text: "Passage alpha"},
		Score: 0.9,
	}}, nil
}

func newRAGWebServer(t *testing.T, rag *stubRAG) *http.ServeMux {
	t.Helper()
	server, err := NewServer(nil, nil, stubModels{}, stubProjects{}, nil, nil, nil, nil, nil, "", rag)
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	mux := http.NewServeMux()
	server.Register(mux)
	return mux
}

func TestRAGSettingsPageRendersAndSaves(t *testing.T) {
	rag := &stubRAG{}
	mux := newRAGWebServer(t, rag)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/rag/settings", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "RAG — Paramètres") {
		t.Fatalf("settings page: code=%d body=%s", rec.Code, rec.Body.String())
	}

	form := url.Values{
		"project_id": {"p1"}, "embedding_model": {domain.ModelLocalEmbedding},
		"top_k": {"3"}, "chunk_size": {"800"}, "chunk_overlap": {"100"},
	}
	req := httptest.NewRequest(http.MethodPost, "/rag/settings", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || rag.saved == nil || rag.saved.TopK != 3 {
		t.Fatalf("settings save: code=%d saved=%#v body=%s", rec.Code, rag.saved, rec.Body.String())
	}
}

func TestRAGTestPageSearches(t *testing.T) {
	rag := &stubRAG{}
	mux := newRAGWebServer(t, rag)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/rag/test", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "notes.txt") {
		t.Fatalf("test page: code=%d body=%s", rec.Code, rec.Body.String())
	}

	form := url.Values{"project_id": {"p1"}, "question": {"alpha ?"}, "mode": {"search"}, "top_k": {"2"}}
	req := httptest.NewRequest(http.MethodPost, "/rag/test/query", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Passage alpha") {
		t.Fatalf("search: code=%d body=%s", rec.Code, rec.Body.String())
	}
	if len(rag.queries) != 1 || rag.queries[0].TopK != 2 {
		t.Fatalf("top_k not forwarded: %#v", rag.queries)
	}
}
