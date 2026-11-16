package web

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"bridge-gateway/application"
	"bridge-gateway/domain"
	"bridge-gateway/infrastructure"
)

func newTestWebDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := infrastructure.OpenDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := infrastructure.Migrate(context.Background(), db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestProjectConfigModelsHandler(t *testing.T) {
	db := newTestWebDB(t)
	ctx := context.Background()

	projectStore := infrastructure.NewProjectStore(db)
	configStore := infrastructure.NewChatConfigStore(db)
	keyStore := infrastructure.NewAPIKeyStore(db)
	providerStore, _ := infrastructure.NewProviderStore(db, nil)
	catalogStore, _ := infrastructure.NewModelCatalogStore(db, []domain.CatalogModel{
		{ID: "m1", Name: "qwen3:8b", Provider: "ollama"},
		{ID: "m2", Name: "mistral-large", Provider: "mistral"},
	})

	_ = providerStore.Create(ctx, &domain.Provider{
		ID:      "p1",
		Name:    "mistral",
		BaseURL: "https://api.mistral.ai",
		APIKey:  "mis-secret-key",
	})
	_ = providerStore.Create(ctx, &domain.Provider{
		ID:      "p2",
		Name:    "ollama",
		BaseURL: "http://localhost:11434",
	})

	projectUC := application.NewProjectUseCase(projectStore)
	proj, _ := projectUC.Create(ctx, "Test Project", "")

	configUC := application.NewConfigUseCase(configStore)
	keyUC := application.NewAPIKeyUseCase(keyStore, projectStore)
	providerUC := application.NewProviderUseCase(providerStore)
	catalogUC := application.NewModelCatalogUseCase(catalogStore)
	modelUC := application.NewModelUseCase(nil, configStore, catalogStore)

	server, err := NewServer(nil, configUC, modelUC, projectUC, keyUC, providerUC, catalogUC, nil, nil, "http://localhost:11434")
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	mux := http.NewServeMux()
	server.Register(mux)

	// 1. GET /projects/{pid}/configs/models?provider=mistral should return only mistral models in a select box
	req := httptest.NewRequest("GET", "/projects/"+proj.ID+"/configs/models?provider=mistral", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `<select id="cfg-model" name="model" required>`) {
		t.Fatalf("expected select element with id cfg-model, got: %s", body)
	}
	if !strings.Contains(body, "mistral-large") {
		t.Fatalf("expected mistral-large in select options, got: %s", body)
	}
	if strings.Contains(body, "qwen3:8b") {
		t.Fatalf("did not expect qwen3:8b (ollama) in mistral models, got: %s", body)
	}

	// 2. GET /projects/{pid}/configs/models with empty provider should return select box with no models
	reqEmpty := httptest.NewRequest("GET", "/projects/"+proj.ID+"/configs/models?provider=", nil)
	recEmpty := httptest.NewRecorder()
	mux.ServeHTTP(recEmpty, reqEmpty)

	if recEmpty.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", recEmpty.Code)
	}
	bodyEmpty := recEmpty.Body.String()
	if strings.Contains(bodyEmpty, "mistral-large") || strings.Contains(bodyEmpty, "qwen3:8b") {
		t.Fatalf("expected no models for empty provider, got: %s", bodyEmpty)
	}

	// 3. Edit config form includes data-base-url and data-api-key attributes on provider options
	cfg, _ := configUC.Create(ctx, &domain.ChatConfig{
		ProjectID:      proj.ID,
		Name:           "Cfg 1",
		Provider:       "mistral",
		Model:          "mistral-large",
		FallbackModels: []domain.ModelChoice{{Provider: "mistral", Model: "mistral-small"}},
	})

	reqEdit := httptest.NewRequest("GET", "/projects/"+proj.ID+"/configs/"+cfg.ID+"/edit", nil)
	recEdit := httptest.NewRecorder()
	mux.ServeHTTP(recEdit, reqEdit)

	if recEdit.Code != http.StatusOK {
		t.Fatalf("expected 200 for edit, got %d", recEdit.Code)
	}
	bodyEdit := recEdit.Body.String()
	if !strings.Contains(bodyEdit, `data-api-key="mis-secret-key"`) {
		t.Fatalf("expected data-api-key on provider option, got: %s", bodyEdit)
	}
	if !strings.Contains(bodyEdit, `data-base-url="https://api.mistral.ai"`) {
		t.Fatalf("expected data-base-url on provider option, got: %s", bodyEdit)
	}
	if !strings.Contains(bodyEdit, `<select id="cfg-model" name="model" required>`) {
		t.Fatalf("expected select element for model in edit form, got: %s", bodyEdit)
	}
	if !strings.Contains(bodyEdit, `schema-constraints-dropdown`) {
		t.Fatalf("expected schema constraints dropdown in edit form, got: %s", bodyEdit)
	}
	if !strings.Contains(bodyEdit, `name="response_schema"`) {
		t.Fatalf("expected response_schema textarea in edit form, got: %s", bodyEdit)
	}
	if !strings.Contains(bodyEdit, `name="fallback_provider[]"`) {
		t.Fatalf("expected fallback provider select in edit form, got: %s", bodyEdit)
	}
	if !strings.Contains(bodyEdit, `value="mistral-small" selected`) {
		t.Fatalf("expected saved fallback model selected, got: %s", bodyEdit)
	}

	// 4. GET /projects/{pid}/configs/fallback-row returns a fresh selectable row
	reqRow := httptest.NewRequest("GET", "/projects/"+proj.ID+"/configs/fallback-row?provider=mistral", nil)
	recRow := httptest.NewRecorder()
	mux.ServeHTTP(recRow, reqRow)
	if recRow.Code != http.StatusOK {
		t.Fatalf("expected 200 for fallback row, got %d", recRow.Code)
	}
	rowBody := recRow.Body.String()
	if !strings.Contains(rowBody, `name="fallback_provider[]"`) || !strings.Contains(rowBody, `name="fallback_model[]"`) {
		t.Fatalf("expected provider and model selects in fallback row, got: %s", rowBody)
	}
	if !strings.Contains(rowBody, "mistral-large") {
		t.Fatalf("expected mistral models in fallback row, got: %s", rowBody)
	}

	// 5. POST /projects/{pid}/configs/{cid} saves fallback models
	form := strings.NewReader("name=Cfg+Updated&provider=mistral&model=mistral-large&fallback_provider%5B%5D=ollama&fallback_model%5B%5D=qwen3%3A8b")
	reqPost := httptest.NewRequest("POST", "/projects/"+proj.ID+"/configs/"+cfg.ID, form)
	reqPost.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	recPost := httptest.NewRecorder()
	mux.ServeHTTP(recPost, reqPost)
	if recPost.Code != http.StatusOK {
		t.Fatalf("expected 200 for update, got %d (body: %s)", recPost.Code, recPost.Body.String())
	}
	updatedCfg, err := configUC.Get(ctx, cfg.ID)
	if err != nil {
		t.Fatalf("get updated config: %v", err)
	}
	if len(updatedCfg.FallbackModels) != 1 {
		t.Fatalf("expected 1 fallback model saved, got %#v", updatedCfg.FallbackModels)
	}
	if updatedCfg.FallbackModels[0].Provider != "ollama" || updatedCfg.FallbackModels[0].Model != "qwen3:8b" {
		t.Errorf("unexpected fallback choice: %#v", updatedCfg.FallbackModels[0])
	}
}
