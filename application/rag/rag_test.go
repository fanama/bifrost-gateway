package rag

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"bridge-gateway/domain"
	domainrag "bridge-gateway/domain/rag"
)

type fakeDocs struct {
	items []domainrag.Document
}

func (f *fakeDocs) List(context.Context) ([]domainrag.Document, error) { return f.items, nil }

func (f *fakeDocs) Get(_ context.Context, id string) (*domainrag.Document, error) {
	for i := range f.items {
		if f.items[i].ID == id {
			item := f.items[i]
			return &item, nil
		}
	}
	return nil, domainrag.ErrDocumentNotFound
}

func (f *fakeDocs) Create(_ context.Context, item *domainrag.Document) error {
	f.items = append(f.items, *item)
	return nil
}

func (f *fakeDocs) Delete(_ context.Context, id string) error {
	for i := range f.items {
		if f.items[i].ID == id {
			f.items = append(f.items[:i], f.items[i+1:]...)
			return nil
		}
	}
	return domainrag.ErrDocumentNotFound
}

func (f *fakeDocs) Update(_ context.Context, item *domainrag.Document) error {
	for i := range f.items {
		if f.items[i].ID == item.ID {
			f.items[i] = *item
			return nil
		}
	}
	return domainrag.ErrDocumentNotFound
}

func (f *fakeDocs) ListByProject(_ context.Context, projectID string) ([]domainrag.Document, error) {
	var out []domainrag.Document
	for _, item := range f.items {
		if item.ProjectID == projectID {
			out = append(out, item)
		}
	}
	return out, nil
}

type fakeChunks struct {
	items map[string][]domainrag.Chunk
}

func (f *fakeChunks) ReplaceForDocument(_ context.Context, _ string, documentID string, chunks []domainrag.Chunk) error {
	if f.items == nil {
		f.items = map[string][]domainrag.Chunk{}
	}
	f.items[documentID] = chunks
	return nil
}

func (f *fakeChunks) ListByProject(_ context.Context, _ string) ([]domainrag.Chunk, error) {
	var out []domainrag.Chunk
	for _, chunks := range f.items {
		out = append(out, chunks...)
	}
	return out, nil
}

func (f *fakeChunks) DeleteByDocument(_ context.Context, _ string, documentID string) error {
	delete(f.items, documentID)
	return nil
}

type fakeEmbedder struct{}

func (fakeEmbedder) Embed(_ context.Context, cfg *domain.ChatConfig, req *domain.EmbeddingRequest) (*domain.EmbeddingResponse, error) {
	var texts []string
	if err := json.Unmarshal(req.Input, &texts); err != nil {
		return nil, err
	}
	data := make([]domain.EmbeddingItem, len(texts))
	for i, text := range texts {
		data[i] = domain.EmbeddingItem{Index: i, Object: "embedding", Embedding: vectorFor(text)}
	}
	return &domain.EmbeddingResponse{Object: "list", Data: data, Model: cfg.Model}, nil
}

func vectorFor(text string) []float32 {
	lower := strings.ToLower(text)
	switch {
	case strings.Contains(lower, "alpha"):
		return []float32{1, 0}
	case strings.Contains(lower, "beta"):
		return []float32{0, 1}
	default:
		return []float32{0.5, 0.5}
	}
}

type fakeLLM struct {
	messages []domain.ChatMessage
}

func (f *fakeLLM) Chat(_ context.Context, _ *domain.ChatConfig, messages []domain.ChatMessage) (domain.LLMResult, error) {
	f.messages = messages
	return domain.LLMResult{Content: "Orion is the alpha project [1].", Model: "fake-model"}, nil
}

type fakeConfigs struct {
	cfg *domain.ChatConfig
}

func (f *fakeConfigs) List(context.Context) ([]domain.ChatConfig, error) { return nil, nil }

func (f *fakeConfigs) Get(context.Context, string) (*domain.ChatConfig, error) { return f.cfg, nil }

func (f *fakeConfigs) Create(context.Context, *domain.ChatConfig) error { return nil }

func (f *fakeConfigs) Delete(context.Context, string) error { return nil }

func (f *fakeConfigs) Update(context.Context, *domain.ChatConfig) error { return nil }

func (f *fakeConfigs) ListByProject(context.Context, string) ([]domain.ChatConfig, error) {
	return nil, nil
}

func (f *fakeConfigs) GetActiveByProject(context.Context, string) (*domain.ChatConfig, error) {
	return f.cfg, nil
}

func (f *fakeConfigs) SetActive(context.Context, string, string) error { return nil }

type fakeSettings struct {
	item *domainrag.Settings
}

func (f *fakeSettings) Get(context.Context, string) (*domainrag.Settings, error) {
	if f.item == nil {
		return nil, domainrag.ErrSettingsNotFound
	}
	return f.item, nil
}

func (f *fakeSettings) Save(_ context.Context, settings *domainrag.Settings) error {
	stored := *settings
	f.item = &stored
	return nil
}

type textExtractor struct{}

func (textExtractor) Extract(_ context.Context, _, _ string, content []byte) (string, error) {
	return string(content), nil
}

func newTestService(t *testing.T) (*Service, *fakeDocs, *fakeChunks, *fakeLLM) {
	t.Helper()
	docs, chunks, llm := &fakeDocs{}, &fakeChunks{}, &fakeLLM{}
	service := NewService(docs, chunks, textExtractor{}, fakeEmbedder{}, &fakeConfigs{
		cfg: &domain.ChatConfig{ID: "cfg", ProjectID: "p1", Name: "chat", Provider: "ollama", Model: "fake-model"},
	}, nil, llm)
	service.ids = func() string { return "doc-1" }
	return service, docs, chunks, llm
}

func TestIngestStoresDocumentAndChunks(t *testing.T) {
	service, docs, chunks, _ := newTestService(t)
	doc, err := service.Ingest(context.Background(), "p1", "notes.txt", "text/plain",
		[]byte("The alpha project is called Orion."), IngestOptions{})
	if err != nil {
		t.Fatalf("ingest: %v", err)
	}
	if doc.ID != "doc-1" || doc.ChunkCount != 1 || doc.EmbeddingModel != domain.ModelLocalEmbedding {
		t.Fatalf("unexpected document: %#v", doc)
	}
	if len(docs.items) != 1 || len(chunks.items["doc-1"]) != 1 {
		t.Fatalf("document not persisted: docs=%d chunks=%d", len(docs.items), len(chunks.items["doc-1"]))
	}
}

func TestQueryReturnsGroundedAnswerAndSources(t *testing.T) {
	service, _, _, llm := newTestService(t)
	_, _ = service.Ingest(context.Background(), "p1", "notes.txt", "text/plain",
		[]byte("The alpha project is called Orion."), IngestOptions{})
	answer, err := service.Query(context.Background(), "p1", "What is the alpha project called?", QueryOptions{TopK: 3})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if !answer.HasContext || len(answer.Sources) != 1 || answer.Sources[0].FileName != "notes.txt" {
		t.Fatalf("unexpected answer: %#v", answer)
	}
	if !strings.Contains(llm.messages[1].Content, "Orion") {
		t.Fatalf("passage not sent to the model: %s", llm.messages[1].Content)
	}
}

func TestQueryWithoutDocumentsUsesNoContextPrompt(t *testing.T) {
	service, _, _, llm := newTestService(t)
	answer, err := service.Query(context.Background(), "p1", "anything?", QueryOptions{})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if answer.HasContext || len(answer.Sources) != 0 {
		t.Fatalf("unexpected context: %#v", answer)
	}
	if !strings.Contains(llm.messages[1].Content, "No passage matched") {
		t.Fatalf("no-context prompt missing: %s", llm.messages[1].Content)
	}
}

func TestSearchRequiresQuestion(t *testing.T) {
	service, _, _, _ := newTestService(t)
	if _, err := service.Search(context.Background(), "p1", "  ", QueryOptions{}); err == nil {
		t.Fatal("expected validation error")
	}
}

func TestDeleteDocumentRemovesChunks(t *testing.T) {
	service, docs, chunks, _ := newTestService(t)
	_, _ = service.Ingest(context.Background(), "p1", "notes.txt", "text/plain", []byte("alpha"), IngestOptions{})
	if err := service.DeleteDocument(context.Background(), "p1", "doc-1"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if len(docs.items) != 0 || len(chunks.items) != 0 {
		t.Fatalf("document not deleted: docs=%d chunks=%d", len(docs.items), len(chunks.items))
	}
}

func newSettingsService(t *testing.T, settings *domainrag.Settings) *Service {
	t.Helper()
	service, _, _, _ := newTestService(t)
	service.settings = &fakeSettings{item: settings}
	return service
}

func TestSaveSettingsValidatesAndPersists(t *testing.T) {
	service := newSettingsService(t, nil)
	saved, err := service.SaveSettings(context.Background(), &domainrag.Settings{
		ProjectID: "p1", TopK: 2, ChunkSize: 100, ChunkOverlap: 10,
	})
	if err != nil {
		t.Fatalf("save settings: %v", err)
	}
	if saved.EmbeddingModel != domain.ModelLocalEmbedding || saved.UpdatedAt.IsZero() {
		t.Fatalf("unexpected saved settings: %#v", saved)
	}
	if _, err := service.SaveSettings(context.Background(), &domainrag.Settings{
		ProjectID: "p1", TopK: 100, ChunkSize: 100,
	}); err == nil {
		t.Fatal("expected validation error for top_k")
	}
}

func TestIngestAndQueryApplyStoredSettings(t *testing.T) {
	service := newSettingsService(t, &domainrag.Settings{
		ProjectID: "p1", TopK: 1, ChunkSize: 5, ChunkOverlap: 0,
		SystemPrompt: "Cite les sources.",
	})
	service.settings.(*fakeSettings).item.EmbeddingModel = domain.ModelLocalEmbedding
	doc, err := service.Ingest(context.Background(), "p1", "notes.txt", "text/plain",
		[]byte("alpha beta gamma"), IngestOptions{})
	if err != nil {
		t.Fatalf("ingest: %v", err)
	}
	if doc.ChunkCount < 2 {
		t.Fatalf("stored chunk size not applied: %#v", doc)
	}
	answer, err := service.Query(context.Background(), "p1", "alpha?", QueryOptions{})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(answer.Sources) != 1 {
		t.Fatalf("stored top_k not applied: %#v", answer.Sources)
	}
	if !strings.Contains(service.llm.(*fakeLLM).messages[0].Content, "Cite les sources") {
		t.Fatalf("stored system prompt not applied: %#v", service.llm.(*fakeLLM).messages)
	}
}
