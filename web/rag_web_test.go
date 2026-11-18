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
	settings := domain.RAGSettings{
		ProjectID: "p1", EmbeddingModel: domain.ModelLocalEmbedding, TopK: 4,
		ChunkSize: 1000, ChunkOverlap: 200, ChunkingMethod: domain.RAGChunkingFixed,
		RetrievalMode: domain.RAGRetrievalDense, RetrievalCandidates: 4,
		Reranker: domain.RAGRerankNone, MMRLambda: 0.7,
	}
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
func (s *stubRAG) DocumentChunks(context.Context, string, string) (*domain.Document, []domain.Chunk, error) {
	return &domain.Document{ID: "d1", FileName: "notes.txt", ChunkCount: 1, EmbeddingModel: domain.ModelLocalEmbedding},
		[]domain.Chunk{{ID: "d1:0", DocumentID: "d1", FileName: "notes.txt", Ordinal: 0, Text: "Passage alpha complet"}}, nil
}
func (s *stubRAG) DeleteDocument(context.Context, string, string) error { return nil }
func (s *stubRAG) Query(context.Context, string, string, application.RAGQueryOptions) (*domain.Answer, error) {
	return &domain.Answer{Answer: "Orion [1]", HasContext: true, Model: "test"}, nil
}
func (s *stubRAG) Search(_ context.Context, _ string, _ string, opts application.RAGQueryOptions) (*domain.RAGSearchResult, error) {
	s.queries = append(s.queries, opts)
	return &domain.RAGSearchResult{
		Chunks: []domain.RetrievedChunk{{
			Chunk: domain.Chunk{FileName: "notes.txt", Ordinal: 0, Text: "Passage alpha"},
			Score: 0.9,
		}},
		IndexState: domain.RAGIndexReady,
	}, nil
}

func (s *stubRAG) IndexStatus(context.Context, string) (domain.RAGIndexStatus, error) {
	return domain.RAGIndexStatus{ProjectID: "p1", State: domain.RAGIndexReady, Nodes: 4}, nil
}

func (s *stubRAG) RebuildIndex(context.Context, string) (domain.RAGIndexStatus, error) {
	return domain.RAGIndexStatus{ProjectID: "p1", State: domain.RAGIndexBuilding}, nil
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

func TestRAGPageRendersSettingsAndCorpus(t *testing.T) {
	rag := &stubRAG{}
	mux := newRAGWebServer(t, rag)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/rag", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Corpus et test") {
		t.Fatalf("rag page: code=%d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "notes.txt") {
		t.Fatalf("documents missing from merged page: %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `id="rag-document-dialog"`) {
		t.Fatalf("document dialog missing: %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `name="chunking_method"`) {
		t.Fatalf("chunking method selector missing: %s", rec.Body.String())
	}
	for _, method := range []string{"fixed", "sentence", "markdown", "semantic"} {
		if !strings.Contains(rec.Body.String(), `value="`+method+`"`) {
			t.Fatalf("chunking method %q not offered: %s", method, rec.Body.String())
		}
	}
	for _, name := range []string{"retrieval_mode", "retrieval_candidates", "reranker", "mmr_lambda"} {
		if !strings.Contains(rec.Body.String(), `name="`+name+`"`) {
			t.Fatalf("pipeline field %q missing: %s", name, rec.Body.String())
		}
	}
	for _, mode := range []string{"dense", "hybrid"} {
		if !strings.Contains(rec.Body.String(), `value="`+mode+`"`) {
			t.Fatalf("retrieval mode %q not offered: %s", mode, rec.Body.String())
		}
	}

	form := url.Values{
		"project_id": {"p1"}, "embedding_model": {domain.ModelLocalEmbedding},
		"top_k": {"3"}, "chunk_size": {"800"}, "chunk_overlap": {"100"},
		"chunking_method": {"sentence"},
		"retrieval_mode":  {"hybrid"}, "retrieval_candidates": {"6"},
		"reranker": {"mmr"}, "mmr_lambda": {"0.5"},
		"hnsw_m": {"16"}, "hnsw_ef_construction": {"200"}, "hnsw_ef_search": {"64"},
		"hnsw_tombstone_rebuild_ratio": {"0.2"}, "hnsw_memory_limit_mb": {"64"},
		"hnsw_recall_sample_rate": {"0"},
	}
	req := httptest.NewRequest(http.MethodPost, "/rag/settings", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || rag.saved == nil || rag.saved.TopK != 3 {
		t.Fatalf("settings save: code=%d saved=%#v body=%s", rec.Code, rag.saved, rec.Body.String())
	}
	if rag.saved.HNSWM != 16 {
		t.Fatalf("hnsw_m not parsed: %#v", rag.saved)
	}
	if rag.saved.ChunkingMethod != "sentence" {
		t.Fatalf("chunking_method not parsed: %#v", rag.saved)
	}
	if rag.saved.RetrievalMode != "hybrid" || rag.saved.RetrievalCandidates != 6 {
		t.Fatalf("retrieval fields not parsed: %#v", rag.saved)
	}
	if rag.saved.Reranker != "mmr" || rag.saved.MMRLambda != 0.5 {
		t.Fatalf("rerank fields not parsed: %#v", rag.saved)
	}
}

func TestRAGLegacyPagesRedirect(t *testing.T) {
	mux := newRAGWebServer(t, &stubRAG{})
	for _, path := range []string{"/rag/settings", "/rag/test"} {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path+"?project_id=p1", nil))
		if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/rag?project_id=p1" {
			t.Fatalf("%s redirect: code=%d location=%q", path, rec.Code, rec.Header().Get("Location"))
		}
	}
}

func TestRAGDocumentChunksFragment(t *testing.T) {
	mux := newRAGWebServer(t, &stubRAG{})
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/rag/documents/d1/chunks?project_id=p1", nil))
	body := rec.Body.String()
	if rec.Code != http.StatusOK || !strings.Contains(body, "Passage alpha complet") {
		t.Fatalf("chunks fragment: code=%d body=%s", rec.Code, body)
	}
	if !strings.Contains(body, "passage #0") || !strings.Contains(body, "notes.txt") {
		t.Fatalf("chunk metadata missing: %s", body)
	}
}

func TestRAGPageSearches(t *testing.T) {
	rag := &stubRAG{}
	mux := newRAGWebServer(t, rag)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/rag", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "notes.txt") {
		t.Fatalf("rag page: code=%d body=%s", rec.Code, rec.Body.String())
	}

	form := url.Values{"project_id": {"p1"}, "question": {"alpha ?"}, "mode": {"search"}, "top_k": {"2"}}
	req := httptest.NewRequest(http.MethodPost, "/rag/query", strings.NewReader(form.Encode()))
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
