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

// remoteSpy joue le provider distant dans le routage : la page du testeur
// force le runtime local, donc il doit rester silencieux meme quand la
// configuration selectionnee pointe vers un provider distant.
type remoteSpy struct {
	t     *testing.T
	calls int
}

func (s *remoteSpy) Embed(context.Context, *domain.ChatConfig, *domain.EmbeddingRequest) (*domain.EmbeddingResponse, error) {
	s.calls++
	s.t.Errorf("remote embedder must not be called for a local config")
	return &domain.EmbeddingResponse{Object: "list"}, nil
}

// TestEmbeddingUILocalRuntime verifie le chemin complet demande : depuis le
// front HTMX (/embeddings-test/compute), la page Testeur d'Embeddings appelle
// le modele local d'embedding, y compris quand la configuration existante
// (ici mistral) pointe vers un provider distant — config_id est ignoree.
func TestEmbeddingUILocalRuntime(t *testing.T) {
	ctx := context.Background()
	db := newTestWebDB(t)

	projectStore := infrastructure.NewProjectStore(db)
	configStore := infrastructure.NewChatConfigStore(db)
	providerStore, _ := infrastructure.NewProviderStore(db, nil)
	keyStore := infrastructure.NewAPIKeyStore(db)
	catalogStore, _ := infrastructure.NewModelCatalogStore(db, nil)

	projectUC := application.NewProjectUseCase(projectStore)
	proj, _ := projectUC.Create(ctx, "Local Embedding Project", "")

	configUC := application.NewConfigUseCase(configStore)
	cfg, err := configUC.Create(ctx, &domain.ChatConfig{
		ProjectID: proj.ID,
		Name:      "Mistral Embed Config",
		Provider:  "mistral",
		Model:     "mistral-embed",
		Active:    true,
	})
	if err != nil {
		t.Fatalf("create config: %v", err)
	}

	keyUC := application.NewAPIKeyUseCase(keyStore, projectStore)
	providerUC := application.NewProviderUseCase(providerStore)
	catalogUC := application.NewModelCatalogUseCase(catalogStore)
	modelUC := application.NewModelUseCase(nil, configStore, catalogStore)

	spy := &remoteSpy{t: t}
	embeddingUC := application.NewEmbeddingUseCase(configStore,
		infrastructure.NewEmbeddingRouter(
			infrastructure.NewLocalEmbedder(infrastructure.LocalEmbedderOptions{}),
			spy,
		),
	)

	server := NewServer(nil, configUC, modelUC, projectUC, keyUC, providerUC, catalogUC, embeddingUC, "http://localhost:11434")
	mux := http.NewServeMux()
	server.Register(mux)

	// 1. La page du testeur charge avec la configuration locale.
	reqPage := httptest.NewRequest("GET", "/embeddings-test", nil)
	recPage := httptest.NewRecorder()
	mux.ServeHTTP(recPage, reqPage)
	if recPage.Code != http.StatusOK {
		t.Fatalf("page status: expected 200, got %d", recPage.Code)
	}
	bodyPage := recPage.Body.String()
	if strings.Contains(bodyPage, cfg.Name) {
		t.Fatalf("config selector must be gone from the tester page, found %q in: %s", cfg.Name, bodyPage)
	}
	if !strings.Contains(bodyPage, infrastructure.LocalEmbeddingModel) || !strings.Contains(bodyPage, "runtime local") {
		t.Fatalf("expected local runtime badge in page, got: %s", bodyPage)
	}

	// 2. Le compute en mode texte unique passe par le runtime local, malgre
	// la config mistral envoyee dans le formulaire (ignoree par le handler).
	formSingle := url.Values{
		"config_id":  {cfg.ID},
		"mode":       {"single"},
		"input_text": {"Bonjour le monde local"},
	}
	reqSingle := httptest.NewRequest("POST", "/embeddings-test/compute", strings.NewReader(formSingle.Encode()))
	reqSingle.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	recSingle := httptest.NewRecorder()
	mux.ServeHTTP(recSingle, reqSingle)

	if recSingle.Code != http.StatusOK {
		t.Fatalf("compute status: expected 200, got %d: %s", recSingle.Code, recSingle.Body)
	}
	bodySingle := recSingle.Body.String()
	if !strings.Contains(bodySingle, "Résultats de vectorisation") {
		t.Fatalf("expected result header, got: %s", bodySingle)
	}
	if !strings.Contains(bodySingle, infrastructure.LocalEmbeddingModel) {
		t.Fatalf("expected local model name in result, got: %s", bodySingle)
	}
	if spy.calls != 0 {
		t.Errorf("remote embedder called %d times", spy.calls)
	}

	// 3. Le mode comparaison (2 textes) fonctionne aussi, via le meme routage.
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
		t.Fatalf("compare status: expected 200, got %d: %s", recCompare.Code, recCompare.Body)
	}
	if !strings.Contains(recCompare.Body.String(), "Score de similarité cosinus") {
		t.Fatalf("expected similarity score, got: %s", recCompare.Body)
	}
	if spy.calls != 0 {
		t.Errorf("remote embedder called %d times", spy.calls)
	}
}
