package tests

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"bridge-gateway/application"
	"bridge-gateway/domain"
	"bridge-gateway/handlers"
	"bridge-gateway/infrastructure"
	"bridge-gateway/infrastructure/ragindex"
)

type ragStubEmbedder struct{}

func (ragStubEmbedder) Embed(_ context.Context, cfg *domain.ChatConfig, req *domain.EmbeddingRequest) (*domain.EmbeddingResponse, error) {
	var texts []string
	if err := json.Unmarshal(req.Input, &texts); err != nil {
		return nil, err
	}
	data := make([]domain.EmbeddingItem, len(texts))
	for i, text := range texts {
		vector := []float32{0.5, 0.5}
		if strings.Contains(strings.ToLower(text), "alpha") {
			vector = []float32{1, 0}
		}
		data[i] = domain.EmbeddingItem{Index: i, Object: "embedding", Embedding: vector}
	}
	return &domain.EmbeddingResponse{Object: "list", Data: data, Model: cfg.Model}, nil
}

type ragStubLLM struct{}

func (ragStubLLM) Chat(context.Context, *domain.ChatConfig, []domain.ChatMessage) (domain.LLMResult, error) {
	return domain.LLMResult{Content: "Orion [1]", Model: "rag-model"}, nil
}

func newRAGMux(t *testing.T) (*http.ServeMux, string) {
	t.Helper()
	mux, secret, _ := newRAGMuxWithMaster(t)
	return mux, secret
}

func newRAGMuxWithMaster(t *testing.T) (*http.ServeMux, string, string) {
	t.Helper()
	const masterKey = "master-test-key"
	ctx := context.Background()
	db := newTestDB(t)

	projects := infrastructure.NewProjectStore(db)
	project, err := application.NewProjectUseCase(projects).Create(ctx, "RAG", "")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	keys := infrastructure.NewAPIKeyStore(db)
	_, secret, err := application.NewAPIKeyUseCase(keys, projects).Create(ctx, project.ID, "rag-key")
	if err != nil {
		t.Fatalf("create key: %v", err)
	}
	configs := infrastructure.NewChatConfigStore(db)
	cfg, err := application.NewConfigUseCase(configs).Create(ctx, &domain.ChatConfig{
		ProjectID: project.ID, Name: "chat", Provider: "ollama", Model: "fake-chat",
	})
	if err != nil {
		t.Fatalf("create config: %v", err)
	}
	if _, err := application.NewConfigUseCase(configs).SetActive(ctx, project.ID, cfg.ID); err != nil {
		t.Fatalf("activate config: %v", err)
	}

	chunkStore := infrastructure.NewRAGChunkStore(db)
	indexManager := ragindex.NewManager(chunkStore)
	t.Cleanup(indexManager.Wait)
	service := application.NewRAGUseCase(
		infrastructure.NewRAGDocumentStore(db),
		chunkStore,
		infrastructure.NewRAGTextExtractor(0),
		ragStubEmbedder{},
		configs,
		infrastructure.NewRAGSettingsStore(db),
		ragStubLLM{},
	).WithIndex(indexManager)
	handler := handlers.NewRAGHandler(application.NewAuthUseCase(masterKey, keys, projects), service)

	mux := http.NewServeMux()
	handlers.RegisterRAGRoutes(mux, handler)
	return mux, secret, masterKey
}

func ragUpload(t *testing.T, mux *http.ServeMux, secret, name, content string) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", name)
	if err != nil {
		t.Fatalf("form file: %v", err)
	}
	if _, err := io.WriteString(part, content); err != nil {
		t.Fatalf("write part: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/rag/documents", &body)
	req.Header.Set("Authorization", "Bearer "+secret)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func ragJSON(t *testing.T, mux *http.ServeMux, method, target, secret string, body any) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	req := httptest.NewRequest(method, target, bytes.NewReader(raw))
	req.Header.Set("Authorization", "Bearer "+secret)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func TestRAGUploadQueryAndDelete(t *testing.T) {
	mux, secret := newRAGMux(t)
	upload := ragUpload(t, mux, secret, "notes.txt", "The alpha project is called Orion.")
	if upload.Code != http.StatusCreated {
		t.Fatalf("upload status %d: %s", upload.Code, upload.Body.String())
	}
	var doc domain.Document
	if err := json.Unmarshal(upload.Body.Bytes(), &doc); err != nil {
		t.Fatalf("decode document: %v", err)
	}

	answer := ragJSON(t, mux, http.MethodPost, "/v1/rag/query", secret, map[string]any{
		"question": "What is the alpha project called?",
	})
	if answer.Code != http.StatusOK {
		t.Fatalf("query status %d: %s", answer.Code, answer.Body.String())
	}
	var got domain.Answer
	if err := json.Unmarshal(answer.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode answer: %v", err)
	}
	if !got.HasContext || len(got.Sources) != 1 || got.Sources[0].FileName != "notes.txt" {
		t.Fatalf("unexpected answer: %#v", got)
	}

	del := ragJSON(t, mux, http.MethodDelete, "/v1/rag/documents/"+doc.ID, secret, nil)
	if del.Code != http.StatusNoContent {
		t.Fatalf("delete status %d: %s", del.Code, del.Body.String())
	}
}

func TestRAGUploadRejectsUnsupportedType(t *testing.T) {
	mux, secret := newRAGMux(t)
	rec := ragUpload(t, mux, secret, "report.docx", "PK\x03\x04binary")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestRAGIndexStatusRebuildAndSearchFlag(t *testing.T) {
	mux, secret := newRAGMux(t)
	if rec := ragUpload(t, mux, secret, "notes.txt", "The alpha project is called Orion."); rec.Code != http.StatusCreated {
		t.Fatalf("upload: %d %s", rec.Code, rec.Body.String())
	}

	statusRec := ragJSON(t, mux, http.MethodGet, "/v1/rag/index/status", secret, nil)
	if statusRec.Code != http.StatusOK {
		t.Fatalf("status: %d %s", statusRec.Code, statusRec.Body.String())
	}
	var status domain.RAGIndexStatus
	if err := json.Unmarshal(statusRec.Body.Bytes(), &status); err != nil {
		t.Fatalf("decode status: %v", err)
	}
	if status.State != domain.RAGIndexReady || status.Nodes < 1 {
		t.Fatalf("unexpected index status: %#v", status)
	}

	rebuild := ragJSON(t, mux, http.MethodPost, "/v1/rag/index/rebuild", secret, nil)
	if rebuild.Code != http.StatusAccepted {
		t.Fatalf("rebuild: %d %s", rebuild.Code, rebuild.Body.String())
	}

	search := ragJSON(t, mux, http.MethodPost, "/v1/rag/search", secret, map[string]any{"question": "alpha?"})
	if search.Code != http.StatusOK {
		t.Fatalf("search: %d %s", search.Code, search.Body.String())
	}
	var payload struct {
		Total      int    `json:"total"`
		Degraded   bool   `json:"degraded"`
		IndexState string `json:"index_state"`
	}
	if err := json.Unmarshal(search.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode search: %v", err)
	}
	if payload.Total == 0 || payload.Degraded || payload.IndexState != string(domain.RAGIndexReady) {
		t.Fatalf("unexpected search metadata: %#v", payload)
	}
}

// RAG is project-token only: the master key cannot pick a project, and a
// project token always acts on its own project regardless of client hints.
func TestRAGQueryRejectsMasterKeyAndBindsProjectToToken(t *testing.T) {
	mux, secret, master := newRAGMuxWithMaster(t)

	masterRec := ragJSON(t, mux, http.MethodPost, "/v1/rag/query?project_id=other", master, map[string]any{
		"question": "alpha?",
	})
	if masterRec.Code != http.StatusUnauthorized {
		t.Fatalf("master key must not call RAG: %d %s", masterRec.Code, masterRec.Body.String())
	}

	req := httptest.NewRequest(http.MethodPost, "/v1/rag/query?project_id=other", strings.NewReader(`{"question":"alpha?"}`))
	req.Header.Set("Authorization", "Bearer "+secret)
	req.Header.Set("X-Project-ID", "other")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("project token must act on its own project: %d %s", rec.Code, rec.Body.String())
	}
}

func TestRAGQueryRequiresAuth(t *testing.T) {
	mux, _ := newRAGMux(t)
	req := httptest.NewRequest(http.MethodPost, "/v1/rag/query", strings.NewReader(`{"question":"x"}`))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}
