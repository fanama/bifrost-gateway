package web

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"bridge-gateway/application"
	"bridge-gateway/domain"
	"bridge-gateway/infrastructure"
)

// readAll consomme la reponse et rend son corps texte.
func readAll(t *testing.T, resp *http.Response) string {
	t.Helper()
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return string(body)
}

// recordingEmbedder joue un moteur distant et conserve la configuration recue,
// pour verifier que le modele ajoute depuis la page Modeles arrive bien au
// routage avec le provider enregistre.
type recordingEmbedder struct {
	calls int
	cfg   *domain.ChatConfig
}

func (d *recordingEmbedder) Embed(_ context.Context, cfg *domain.ChatConfig, _ *domain.EmbeddingRequest) (*domain.EmbeddingResponse, error) {
	d.calls++
	d.cfg = cfg
	return &domain.EmbeddingResponse{
		Object: "list",
		Model:  cfg.Model,
		Data: []domain.EmbeddingItem{
			{Index: 0, Object: "embedding", Embedding: []float32{0.1, 0.2, -0.3, 0.4}},
		},
		Usage: domain.EmbeddingUsage{PromptTokens: 4, TotalTokens: 4},
	}, nil
}

// newModelKindTestServer monte un serveur complet (catalogue rel, routeur
// branche sur le catalogue comme main.go, config activee openai) pour
// observer le cycle de vie complet d'un modele ajoute depuis la page Modeles.
func newModelKindTestServer(t *testing.T, ctx context.Context, spy domain.EmbeddingProvider) (*httptest.Server, *application.ModelCatalogUseCase) {
	t.Helper()
	db := newTestWebDB(t)

	configStore := infrastructure.NewChatConfigStore(db)
	providerStore, _ := infrastructure.NewProviderStore(db, nil)
	keyStore := infrastructure.NewAPIKeyStore(db)
	catalogStore, _ := infrastructure.NewModelCatalogStore(db, nil)

	configUC := application.NewConfigUseCase(configStore)
	if _, err := configUC.Create(ctx, &domain.ChatConfig{
		ProjectID: "proj-kind",
		Name:      "Active OpenAI",
		Provider:  "openai",
		Model:     "text-embedding-3-small",
		Active:    true,
	}); err != nil {
		t.Fatalf("create config: %v", err)
	}

	keyUC := application.NewAPIKeyUseCase(keyStore, infrastructure.NewProjectStore(db))
	providerUC := application.NewProviderUseCase(providerStore)
	projectUC := application.NewProjectUseCase(infrastructure.NewProjectStore(db))
	catalogUC := application.NewModelCatalogUseCase(catalogStore)
	modelUC := application.NewModelUseCase(nil, configStore, catalogStore)

	router := infrastructure.NewEmbeddingRouter(
		infrastructure.NewLocalEmbedder(infrastructure.LocalEmbedderOptions{}),
		nil,
		spy,
	).WithCatalog(catalogStore)
	embeddingUC := application.NewEmbeddingUseCase(configStore, router)

	server := NewServer(nil, configUC, modelUC, projectUC, keyUC, providerUC, catalogUC, embeddingUC, "http://localhost:11434")
	mux := http.NewServeMux()
	server.Register(mux)
	ts := httptest.NewServer(mux)
	t.Cleanup(func() {
		ts.Close()
		db.Close()
	})
	return ts, catalogUC
}

func postForm(t *testing.T, client *http.Client, endpoint string, form url.Values) *http.Response {
	t.Helper()
	resp, err := client.PostForm(endpoint, form)
	if err != nil {
		t.Fatalf("POST %s: %v", endpoint, err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

// TestModelsPageAddsEmbeddingModel couvre le cycle de vie demande : ajouter un
// modele d'embedding depuis la page Modeles, le voir propose dans le select du
// testeur, et l'executer — routant vers le moteur correspondant au provider
// enregistre, jamais vers la config activee (openai ici).
func TestModelsPageAddsEmbeddingModel(t *testing.T) {
	ctx := context.Background()
	spy := &recordingEmbedder{}
	ts, catalogUC := newModelKindTestServer(t, ctx, spy)
	client := ts.Client()

	// 1. Ajout depuis la page Modeles, type embedding.
	resp := postForm(t, client, ts.URL+"/models", url.Values{
		"name":     {"bge-tiny-fr"},
		"provider": {"ollama"},
		"kind":     {"embedding"},
	})
	body := readAll(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST /models status: got %d: %s", resp.StatusCode, body)
	}
	if !strings.Contains(body, "bge-tiny-fr") || !strings.Contains(body, "embedding") {
		t.Fatalf("models list must show the model with its kind, got: %s", body)
	}

	// 2. Le modele figure au catalogue avec le bon kind (contrat stockage).
	models, err := catalogUC.List(ctx)
	if err != nil || len(models) != 1 {
		t.Fatalf("catalog list: %v (len=%d)", err, len(models))
	}
	if models[0].Kind != domain.ModelKindEmbedding || models[0].Provider != "ollama" {
		t.Fatalf("stored model: %+v", models[0])
	}

	// 3. Il est propose dans le select du testeur.
	page, err := client.Get(ts.URL + "/embeddings-test")
	if err != nil {
		t.Fatalf("GET /embeddings-test: %v", err)
	}
	pageBody := readAll(t, page)
	if !strings.Contains(pageBody, `value="bge-tiny-fr"`) {
		t.Fatalf("tester select must offer the catalog model, got: %s", pageBody)
	}

	// 4. Le calcul le route vers le moteur ollama (spy), avec substitution :
	// la config activee openai ne doit pas fuiter dans le appel.
	compute := postForm(t, client, ts.URL+"/embeddings-test/compute", url.Values{
		"mode":        {"single"},
		"input_text":  {"Bonjour le monde"},
		"embed_model": {"bge-tiny-fr"},
	})
	computeBody := readAll(t, compute)
	if compute.StatusCode != http.StatusOK || !strings.Contains(computeBody, "Résultats de vectorisation") {
		t.Fatalf("compute status: %d, body: %s", compute.StatusCode, computeBody)
	}
	if spy.calls != 1 {
		t.Fatalf("expected exactly 1 engine call, got %d", spy.calls)
	}
	if spy.cfg.Provider != "ollama" || spy.cfg.Model != "bge-tiny-fr" {
		t.Errorf("expected ollama/bge-tiny-fr, got %s/%s", spy.cfg.Provider, spy.cfg.Model)
	}
	if spy.cfg.APIKey != "" || spy.cfg.BaseURL != "" {
		t.Errorf("active config credentials must not leak, got %+v", spy.cfg)
	}

	// 5. La page Modeles affiche le type dans son tableau.
	modelsPage, err := client.Get(ts.URL + "/models")
	if err != nil {
		t.Fatalf("GET /models: %v", err)
	}
	modelsBody := readAll(t, modelsPage)
	if !strings.Contains(modelsBody, "<th>Type</th>") {
		t.Error("models table must expose a Type column")
	}
}

// TestModelsPageChatModelStaysOutOfTester : un modele ajoute sans type (defaut
// chat) alimente le datalist des configurations mais ne detourne jamais le
// testeur — et un calcul qui le nomme est rejete avant tout appel de moteur.
func TestModelsPageChatModelStaysOutOfTester(t *testing.T) {
	spy := &recordingEmbedder{}
	ts, _ := newModelKindTestServer(t, context.Background(), spy)
	client := ts.Client()

	resp := postForm(t, client, ts.URL+"/models", url.Values{
		"name":     {"qwen3:8b"},
		"provider": {"ollama"},
		// kind volontairement absent : le defaut chat s'applique.
	})
	if code := resp.StatusCode; code != http.StatusOK {
		t.Fatalf("POST /models status: got %d", code)
	}

	page, err := client.Get(ts.URL + "/embeddings-test")
	if err != nil {
		t.Fatalf("GET /embeddings-test: %v", err)
	}
	pageBody := readAll(t, page)
	if strings.Contains(pageBody, `value="qwen3:8b"`) {
		t.Fatalf("chat model must not appear in the tester select, got: %s", pageBody)
	}

	compute := postForm(t, client, ts.URL+"/embeddings-test/compute", url.Values{
		"mode":        {"single"},
		"input_text":  {"Bonjour le monde"},
		"embed_model": {"qwen3:8b"},
	})
	computeBody := readAll(t, compute)
	if !strings.Contains(computeBody, "inconnu") || !strings.Contains(computeBody, "qwen3:8b") {
		t.Fatalf("expected unknown-model toast, got: %s", computeBody)
	}
	if spy.calls != 0 {
		t.Fatalf("no engine must be called for a chat model, got %d calls", spy.calls)
	}
}
