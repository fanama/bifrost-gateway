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
	"bridge-gateway/infrastructure"
)

type stubEmbeddingProvider struct{}

func (s stubEmbeddingProvider) Embed(ctx context.Context, cfg *domain.ChatConfig, req *domain.EmbeddingRequest) (*domain.EmbeddingResponse, error) {
	return &domain.EmbeddingResponse{
		Object: "list",
		Model:  cfg.Model,
		Data: []domain.EmbeddingItem{
			{
				Index:     0,
				Object:    "embedding",
				Embedding: []float32{0.1, 0.2, -0.3, 0.4},
			},
			{
				Index:     1,
				Object:    "embedding",
				Embedding: []float32{0.1, 0.2, -0.25, 0.38},
			},
		},
		Usage: domain.EmbeddingUsage{
			PromptTokens: 12,
			TotalTokens:  12,
		},
	}, nil
}

func TestEmbeddingUIPageAndCompute(t *testing.T) {
	ctx := context.Background()
	db := newTestWebDB(t)

	projectStore := infrastructure.NewProjectStore(db)
	configStore := infrastructure.NewChatConfigStore(db)
	providerStore, _ := infrastructure.NewProviderStore(db, nil)
	keyStore := infrastructure.NewAPIKeyStore(db)
	catalogStore, _ := infrastructure.NewModelCatalogStore(db, nil)

	projectUC := application.NewProjectUseCase(projectStore)
	proj, _ := projectUC.Create(ctx, "Test Embedding Project", "")

	configUC := application.NewConfigUseCase(configStore)
	cfg, err := configUC.Create(ctx, &domain.ChatConfig{
		ProjectID: proj.ID,
		Name:      "Embed Config",
		Provider:  "openai",
		Model:     "text-embedding-3-small",
		Active:    true,
	})
	if err != nil {
		t.Fatalf("create config: %v", err)
	}

	keyUC := application.NewAPIKeyUseCase(keyStore, projectStore)
	providerUC := application.NewProviderUseCase(providerStore)
	catalogUC := application.NewModelCatalogUseCase(catalogStore)
	modelUC := application.NewModelUseCase(nil, configStore, catalogStore)

	embeddingUC := application.NewEmbeddingUseCase(configStore, stubEmbeddingProvider{})

	server := NewServer(nil, configUC, modelUC, projectUC, keyUC, providerUC, catalogUC, embeddingUC, "http://localhost:11434")
	mux := http.NewServeMux()
	server.Register(mux)

	// 1. GET /embeddings-test returns 200 with HTML form
	reqPage := httptest.NewRequest("GET", "/embeddings-test", nil)
	recPage := httptest.NewRecorder()
	mux.ServeHTTP(recPage, reqPage)

	if recPage.Code != http.StatusOK {
		t.Fatalf("page_embeddings status: expected 200, got %d", recPage.Code)
	}
	bodyPage := recPage.Body.String()
	if !strings.Contains(bodyPage, "Testeur d'Embeddings") {
		t.Fatalf("expected title in page, got: %s", bodyPage)
	}
	if !strings.Contains(bodyPage, cfg.Name) {
		t.Fatalf("expected config name %q in page, got: %s", cfg.Name, bodyPage)
	}

	// 2. POST /embeddings-test/compute with single mode
	formSingle := url.Values{
		"config_id":  {cfg.ID},
		"mode":       {"single"},
		"input_text": {"Bonjour le monde"},
	}
	reqSingle := httptest.NewRequest("POST", "/embeddings-test/compute", strings.NewReader(formSingle.Encode()))
	reqSingle.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	recSingle := httptest.NewRecorder()
	mux.ServeHTTP(recSingle, reqSingle)

	if recSingle.Code != http.StatusOK {
		t.Fatalf("compute single status: expected 200, got %d: %s", recSingle.Code, recSingle.Body)
	}
	bodySingle := recSingle.Body.String()
	if !strings.Contains(bodySingle, "Résultats de vectorisation") {
		t.Fatalf("expected result header in response, got: %s", bodySingle)
	}
	if !strings.Contains(bodySingle, "text-embedding-3-small") {
		t.Fatalf("expected model name in result, got: %s", bodySingle)
	}

	// 3. POST /embeddings-test/compute with compare mode
	formCompare := url.Values{
		"config_id":    {cfg.ID},
		"mode":         {"compare"},
		"input_text_a": {"Texte 1"},
		"input_text_b": {"Texte 2"},
	}
	reqCompare := httptest.NewRequest("POST", "/embeddings-test/compute", strings.NewReader(formCompare.Encode()))
	reqCompare.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	recCompare := httptest.NewRecorder()
	mux.ServeHTTP(recCompare, reqCompare)

	if recCompare.Code != http.StatusOK {
		t.Fatalf("compute compare status: expected 200, got %d: %s", recCompare.Code, recCompare.Body)
	}
	bodyCompare := recCompare.Body.String()
	if !strings.Contains(bodyCompare, "Score de similarité cosinus") {
		t.Fatalf("expected similarity score in compare response, got: %s", bodyCompare)
	}
}
