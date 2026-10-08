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

func TestChatConfigEditFlow(t *testing.T) {
	db := newTestWebDB(t)
	ctx := context.Background()

	projectStore := infrastructure.NewProjectStore(db)
	configStore := infrastructure.NewChatConfigStore(db)
	keyStore := infrastructure.NewAPIKeyStore(db)
	providerStore, _ := infrastructure.NewProviderStore(db, nil)
	catalogStore, _ := infrastructure.NewModelCatalogStore(db, []domain.CatalogModel{
		{ID: "m1", Name: "mistral-large", Provider: "mistral"},
		{ID: "m2", Name: "mistral-small", Provider: "mistral"},
	})

	_ = providerStore.Create(ctx, &domain.Provider{
		ID:      "p1",
		Name:    "mistral",
		BaseURL: "https://api.mistral.ai",
		APIKey:  "mis-secret-key",
	})

	projectUC := application.NewProjectUseCase(projectStore)
	proj, err := projectUC.Create(ctx, "Chat Project", "")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}

	configUC := application.NewConfigUseCase(configStore)
	keyUC := application.NewAPIKeyUseCase(keyStore, projectStore)
	providerUC := application.NewProviderUseCase(providerStore)
	catalogUC := application.NewModelCatalogUseCase(catalogStore)
	modelUC := application.NewModelUseCase(nil, configStore, catalogStore)

	cfg, err := configUC.Create(ctx, &domain.ChatConfig{
		ProjectID:    proj.ID,
		Name:         "Config Alpha",
		Provider:     "mistral",
		Model:        "mistral-large",
		SystemPrompt: "Tu es un assistant utile",
		Active:       true,
	})
	if err != nil {
		t.Fatalf("create config: %v", err)
	}

	server := NewServer(nil, configUC, modelUC, projectUC, keyUC, providerUC, catalogUC, nil, "http://localhost:11434")
	mux := http.NewServeMux()
	server.Register(mux)

	// 1. GET / - Chat page contains the config selector and edit button
	reqPage := httptest.NewRequest("GET", "/", nil)
	recPage := httptest.NewRecorder()
	mux.ServeHTTP(recPage, reqPage)

	if recPage.Code != http.StatusOK {
		t.Fatalf("page_chat status: expected 200, got %d", recPage.Code)
	}
	bodyPage := recPage.Body.String()
	if !strings.Contains(bodyPage, `id="chat-config-selector"`) {
		t.Fatalf("expected #chat-config-selector in page, got: %s", bodyPage)
	}
	if !strings.Contains(bodyPage, `id="chat-edit-config-btn"`) {
		t.Fatalf("expected #chat-edit-config-btn in page, got: %s", bodyPage)
	}
	if !strings.Contains(bodyPage, `Config Alpha`) {
		t.Fatalf("expected Config Alpha in options, got: %s", bodyPage)
	}

	// 2. GET /chat/configs/{cid}/edit - returns the modal form
	reqEdit := httptest.NewRequest("GET", "/chat/configs/"+cfg.ID+"/edit", nil)
	recEdit := httptest.NewRecorder()
	mux.ServeHTTP(recEdit, reqEdit)

	if recEdit.Code != http.StatusOK {
		t.Fatalf("edit form status: expected 200, got %d", recEdit.Code)
	}
	bodyEdit := recEdit.Body.String()
	if !strings.Contains(bodyEdit, `id="config-form-zone"`) {
		t.Fatalf("expected #config-form-zone dialog, got: %s", bodyEdit)
	}
	if !strings.Contains(bodyEdit, `Modifier : Config Alpha`) {
		t.Fatalf("expected modal title with config name, got: %s", bodyEdit)
	}
	if !strings.Contains(bodyEdit, `hx-post="/chat/configs/`+cfg.ID+`"`) {
		t.Fatalf("expected form action /chat/configs/{cid}, got: %s", bodyEdit)
	}
	if !strings.Contains(bodyEdit, `hx-target="#chat-config-selector"`) {
		t.Fatalf("expected form target #chat-config-selector, got: %s", bodyEdit)
	}
	if !strings.Contains(bodyEdit, `Tu es un assistant utile`) {
		t.Fatalf("expected system prompt prefilled, got: %s", bodyEdit)
	}

	// 3. POST /chat/configs/{cid} - updates the config and returns updated selector
	form := url.Values{}
	form.Set("name", "Config Alpha Modifiée")
	form.Set("provider", "mistral")
	form.Set("model", "mistral-small")
	form.Set("system_prompt", "Nouveau prompt")
	form.Set("temperature", "0.5")

	reqUpdate := httptest.NewRequest("POST", "/chat/configs/"+cfg.ID, strings.NewReader(form.Encode()))
	reqUpdate.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	recUpdate := httptest.NewRecorder()
	mux.ServeHTTP(recUpdate, reqUpdate)

	if recUpdate.Code != http.StatusOK {
		t.Fatalf("update status: expected 200, got %d", recUpdate.Code)
	}
	bodyUpdate := recUpdate.Body.String()
	if !strings.Contains(bodyUpdate, "Config Alpha Modifiée — mistral-small (mistral)") {
		t.Fatalf("expected updated config label in select options, got: %s", bodyUpdate)
	}
	if !strings.Contains(bodyUpdate, `value="`+cfg.ID+`" selected`) {
		t.Fatalf("expected updated config to be selected, got: %s", bodyUpdate)
	}

	// Verify database was updated
	updatedCfg, err := configUC.Get(ctx, cfg.ID)
	if err != nil {
		t.Fatalf("get updated config: %v", err)
	}
	if updatedCfg.Name != "Config Alpha Modifiée" {
		t.Fatalf("expected name 'Config Alpha Modifiée', got '%s'", updatedCfg.Name)
	}
	if updatedCfg.Model != "mistral-small" {
		t.Fatalf("expected model 'mistral-small', got '%s'", updatedCfg.Model)
	}
	if updatedCfg.SystemPrompt != "Nouveau prompt" {
		t.Fatalf("expected system prompt 'Nouveau prompt', got '%s'", updatedCfg.SystemPrompt)
	}
	if updatedCfg.Temperature == nil || *updatedCfg.Temperature != 0.5 {
		t.Fatalf("expected temperature 0.5, got %v", updatedCfg.Temperature)
	}
}
