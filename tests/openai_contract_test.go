package tests

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"bridge-gateway/application"
	"bridge-gateway/domain"
	"bridge-gateway/handlers"
	"bridge-gateway/infrastructure"
)

// Ces tests verrouillent le contrat de la surface /v1 face a l'API standard
// OpenAI : chemins, enveloppes de reponse et enveloppes d'erreur. Ils passent
// par handlers.RegisterRoutes, donc par le meme routage que la production.

// stubProvider repond un texte fixe : le contrat HTTP ne doit pas dependre
// d'un vrai appel reseau.
type stubProvider struct{ reply string }

func (s stubProvider) Chat(context.Context, *domain.ChatConfig, []domain.ChatMessage) (domain.LLMResult, error) {
	return domain.LLMResult{Content: s.reply, Model: "stub-model"}, nil
}

func (s stubProvider) Embed(ctx context.Context, cfg *domain.ChatConfig, req *domain.EmbeddingRequest) (*domain.EmbeddingResponse, error) {
	return &domain.EmbeddingResponse{
		Object: "list",
		Model:  cfg.Model,
		Data: []domain.EmbeddingItem{
			{
				Index:     0,
				Object:    "embedding",
				Embedding: []float32{0.0023, -0.0093, 0.015},
			},
		},
		Usage: domain.EmbeddingUsage{
			PromptTokens: 8,
			TotalTokens:  8,
		},
	}, nil
}

// newOpenAIMux cree un projet, une cle et une configuration active, puis
// renvoie un mux equipe comme la production et la cle en clair, seule fois ou
// elle existe.
func newOpenAIMux(t *testing.T, masterKey string) (*http.ServeMux, string) {
	t.Helper()
	return newMuxWithProvider(t, masterKey, stubProvider{reply: "bonjour"})
}

// newMuxWithProvider monte le mux avec un provider choisi, ce qui permet de
// verifier le streaming et la reponse unique sur la meme configuration.
func newMuxWithProvider(t *testing.T, masterKey string, provider domain.LLMProvider) (*http.ServeMux, string) {
	t.Helper()
	ctx := context.Background()
	db := newTestDB(t)

	projects := infrastructure.NewProjectStore(db)
	project, err := application.NewProjectUseCase(projects).Create(ctx, "Contract", "")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}

	keys := infrastructure.NewAPIKeyStore(db)
	_, plaintext, err := application.NewAPIKeyUseCase(keys, projects).Create(ctx, project.ID, "test-key")
	if err != nil {
		t.Fatalf("create api key: %v", err)
	}

	catalog, err := infrastructure.NewModelCatalogStore(db, nil)
	if err != nil {
		t.Fatalf("catalog store: %v", err)
	}

	configs := infrastructure.NewChatConfigStore(db)
	configUseCase := application.NewConfigUseCase(configs)
	cfg, err := configUseCase.Create(ctx, &domain.ChatConfig{
		ProjectID: project.ID, Name: "Local", Provider: "ollama", Model: "gemma4:e4b",
	})
	if err != nil {
		t.Fatalf("create config: %v", err)
	}
	if _, err := configUseCase.SetActive(ctx, project.ID, cfg.ID); err != nil {
		t.Fatalf("activate config: %v", err)
	}

	enrichment := domain.NewEnrichmentService()
	auth := application.NewAuthUseCase(masterKey, keys, projects)

	chat := handlers.NewChatHandler(
		enrichment,
		auth,
		application.NewChatUseCase(configs, enrichment, provider),
		configUseCase,
	)
	models := handlers.NewModelsHandler(auth, application.NewModelUseCase(
		[]domain.ModelInfo{{Name: "gemma4:e4b", Provider: "ollama", Source: "config.yaml"}},
		configs,
		catalog,
	))

	var embedProvider domain.EmbeddingProvider
	if ep, ok := provider.(domain.EmbeddingProvider); ok {
		embedProvider = ep
	}
	embeddingUseCase := application.NewEmbeddingUseCase(configs, embedProvider)
	embeddings := handlers.NewEmbeddingHandler(auth, embeddingUseCase, configUseCase)

	mux := http.NewServeMux()
	handlers.RegisterRoutes(mux, chat, models, embeddings)
	mux.HandleFunc("/chat/completions", chat.HandleChatCompletion)
	if embeddings != nil {
		mux.HandleFunc("/embeddings", embeddings.HandleEmbedding)
	}
	return mux, plaintext
}

// request construit une requete authentifiee, eventuellement avec un corps.
func request(t *testing.T, method, target, secret string, body any) *http.Request {
	t.Helper()
	var raw []byte
	if body != nil {
		var err error
		if raw, err = json.Marshal(body); err != nil {
			t.Fatalf("marshal body: %v", err)
		}
	}
	req := httptest.NewRequest(method, target, bytes.NewReader(raw))
	if secret != "" {
		req.Header.Set("Authorization", "Bearer "+secret)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return req
}

func chatBody(content string) map[string]any {
	return map[string]any{
		"model":    "gemma4:e4b",
		"messages": []map[string]string{{"role": "user", "content": content}},
	}
}

// decodeError relit l'enveloppe d'erreur OpenAI et verifie que les quatre
// champs attendus sont presents, param et code pouvant valoir null.
func decodeError(t *testing.T, body []byte) map[string]any {
	t.Helper()
	var outer struct {
		Error map[string]any `json:"error"`
	}
	if err := json.Unmarshal(body, &outer); err != nil {
		t.Fatalf("error body is not JSON: %v (%s)", err, body)
	}
	if outer.Error == nil {
		t.Fatalf(`missing "error" object: %s`, body)
	}
	for _, field := range []string{"message", "type", "param", "code"} {
		if _, ok := outer.Error[field]; !ok {
			t.Errorf("error object missing %q: %s", field, body)
		}
	}
	return outer.Error
}

func TestChatCompletionMatchesOpenAIShape(t *testing.T) {
	mux, secret := newOpenAIMux(t, "")

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, request(t, http.MethodPost, "/v1/chat/completions", secret, chatBody("salut")))

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("expected application/json, got %q", ct)
	}

	var resp struct {
		ID      string `json:"id"`
		Object  string `json:"object"`
		Created int64  `json:"created"`
		Model   string `json:"model"`
		Choices []struct {
			Index   int `json:"index"`
			Message struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
			TotalTokens      int `json:"total_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode completion: %v (%s)", err, rec.Body)
	}

	if !strings.HasPrefix(resp.ID, "chatcmpl-") {
		t.Errorf("expected a chatcmpl- id, got %q", resp.ID)
	}
	if resp.Object != "chat.completion" {
		t.Errorf(`expected object "chat.completion", got %q`, resp.Object)
	}
	if resp.Created <= 0 {
		t.Errorf("expected a real unix timestamp, got %d", resp.Created)
	}
	if resp.Model == "" {
		t.Error("model must not be empty")
	}
	if len(resp.Choices) != 1 {
		t.Fatalf("expected 1 choice, got %d", len(resp.Choices))
	}
	c := resp.Choices[0]
	if c.Index != 0 {
		t.Errorf("expected index 0, got %d", c.Index)
	}
	if c.Message.Role != "assistant" {
		t.Errorf(`expected role "assistant", got %q`, c.Message.Role)
	}
	if c.Message.Content != "bonjour" {
		t.Errorf("expected the provider reply, got %q", c.Message.Content)
	}
	if c.FinishReason == "" {
		t.Error("finish_reason must not be empty")
	}
	if resp.Usage.TotalTokens != resp.Usage.PromptTokens+resp.Usage.CompletionTokens {
		t.Errorf("total_tokens must be the sum of the two, got %+v", resp.Usage)
	}
}

// Deux appels doivent produire deux identifiants : un id constant casse les
// clients qui indexent leurs traces dessus.
func TestChatCompletionIDsAreUnique(t *testing.T) {
	mux, secret := newOpenAIMux(t, "")

	send := func() string {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, request(t, http.MethodPost, "/v1/chat/completions", secret, chatBody("salut")))
		var resp struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode completion: %v (%s)", err, rec.Body)
		}
		return resp.ID
	}

	if first, second := send(), send(); first == second {
		t.Errorf("completion ids must differ, both were %q", first)
	}
}

func TestListModelsMatchesOpenAIShape(t *testing.T) {
	mux, secret := newOpenAIMux(t, "")

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, request(t, http.MethodGet, "/v1/models", secret, nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body)
	}

	var list struct {
		Object string `json:"object"`
		Data   []struct {
			ID      string `json:"id"`
			Object  string `json:"object"`
			Created int64  `json:"created"`
			OwnedBy string `json:"owned_by"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode list: %v (%s)", err, rec.Body)
	}
	if list.Object != "list" {
		t.Errorf(`expected object "list", got %q`, list.Object)
	}
	if len(list.Data) == 0 {
		t.Fatal("expected at least one model")
	}
	for _, m := range list.Data {
		if m.ID == "" {
			t.Error("model entry missing id")
		}
		if m.Object != "model" {
			t.Errorf(`expected object "model", got %q`, m.Object)
		}
		if m.Created <= 0 {
			t.Errorf("expected a real unix timestamp, got %d", m.Created)
		}
		if m.OwnedBy == "" {
			t.Error("model entry missing owned_by")
		}
	}
}

func TestRetrieveModelIsCaseInsensitive(t *testing.T) {
	mux, secret := newOpenAIMux(t, "")

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, request(t, http.MethodGet, "/v1/models/GEMMA4:E4B", secret, nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body)
	}
	var entry struct {
		ID     string `json:"id"`
		Object string `json:"object"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &entry); err != nil {
		t.Fatalf("decode entry: %v (%s)", err, rec.Body)
	}
	if entry.ID != "gemma4:e4b" || entry.Object != "model" {
		t.Errorf("unexpected entry: %+v", entry)
	}
}

func TestUnknownModelReturnsOpenAIDetailError(t *testing.T) {
	mux, secret := newOpenAIMux(t, "")

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, request(t, http.MethodGet, "/v1/models/does-not-exist", secret, nil))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body)
	}
	e := decodeError(t, rec.Body.Bytes())
	if e["type"] != "not_found_error" {
		t.Errorf(`expected type "not_found_error", got %v`, e["type"])
	}
	if e["param"] != "model" {
		t.Errorf(`expected param "model", got %v`, e["param"])
	}
	if msg, _ := e["message"].(string); !strings.Contains(msg, "does-not-exist") {
		t.Errorf("message should name the missing model, got %q", msg)
	}
}

func TestOpenAIEndpointsRequireAuthentication(t *testing.T) {
	mux, _ := newOpenAIMux(t, "master-key")

	calls := map[string]*http.Request{
		"chat":   request(t, http.MethodPost, "/v1/chat/completions", "", chatBody("salut")),
		"list":   request(t, http.MethodGet, "/v1/models", "", nil),
		"detail": request(t, http.MethodGet, "/v1/models/gemma4:e4b", "", nil),
	}
	for name, req := range calls {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s: expected 401, got %d (%s)", name, rec.Code, rec.Body)
			continue
		}
		e := decodeError(t, rec.Body.Bytes())
		if e["type"] != "authentication_error" {
			t.Errorf("%s: expected authentication_error, got %v", name, e["type"])
		}
		if e["code"] != "invalid_api_key" {
			t.Errorf("%s: expected code invalid_api_key, got %v", name, e["code"])
		}
	}
}

// Une methode non supportee doit repondre 405, et non le 404 du repli "/v1/".
func TestOpenAIEndpointsRejectWrongMethodWith405(t *testing.T) {
	mux, secret := newOpenAIMux(t, "")

	calls := map[string]*http.Request{
		"chat":   request(t, http.MethodGet, "/v1/chat/completions", secret, nil),
		"list":   request(t, http.MethodPost, "/v1/models", secret, nil),
		"detail": request(t, http.MethodDelete, "/v1/models/gemma4:e4b", secret, nil),
	}
	for name, req := range calls {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s: expected 405, got %d (%s)", name, rec.Code, rec.Body)
		}
	}
}

// streamingProvider repond fragment par fragment et sait aussi repondre en une
// fois, pour que les deux modes restent verifiables sur le meme jeu de tests.
type streamingProvider struct{}

func (streamingProvider) Chat(context.Context, *domain.ChatConfig, []domain.ChatMessage) (domain.LLMResult, error) {
	return domain.LLMResult{Content: "bonjour", Model: "stub-model"}, nil
}

func (streamingProvider) ChatStream(ctx context.Context, _ *domain.ChatConfig, _ []domain.ChatMessage) (<-chan domain.StreamEvent, error) {
	out := make(chan domain.StreamEvent)
	go func() {
		defer close(out)
		for _, delta := range []string{"bon", "jour"} {
			select {
			case <-ctx.Done():
				return
			case out <- domain.StreamEvent{Delta: delta, Model: "stub-model"}:
			}
		}
		out <- domain.StreamEvent{Model: "stub-model", Done: true}
	}()
	return out, nil
}

func newStreamingMux(t *testing.T, masterKey string) (*http.ServeMux, string) {
	t.Helper()
	return newMuxWithProvider(t, masterKey, streamingProvider{})
}

// sseChunk est le corps JSON d'une trame, aux champs de l'API OpenAI.
type sseChunk struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	Model   string `json:"model"`
	Choices []struct {
		Index        int      `json:"index"`
		Delta        sseDelta `json:"delta"`
		FinishReason *string  `json:"finish_reason"`
	} `json:"choices"`
	Usage *struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}

type sseDelta struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// sseFrame est une trame SSE decortiquee. sseChunk est embarquee sans nom pour
// que ses champs soient promus a la racine : un champ nomme chercherait une cle
// "Chunk", absente du JSON, et resterait toujours vide.
type sseFrame struct {
	Raw string
	sseChunk
}

// parseSSE decortique un flux en trames, en verifiant au passage le format des
// separateurs : un bloc vide au milieu du flux ne serait pas du SSE valide.
func parseSSE(t *testing.T, body string) []sseFrame {
	t.Helper()

	var frames []sseFrame
	for _, block := range strings.Split(strings.TrimSuffix(body, "\n\n"), "\n\n") {
		if block == "" {
			t.Fatalf("empty SSE block, the stream is not well formed:\n%s", body)
		}
		if !strings.HasPrefix(block, "data: ") {
			t.Fatalf("SSE block must start with \"data: \", got %q", block)
		}
		payload := strings.TrimPrefix(block, "data: ")
		if payload == "[DONE]" {
			frames = append(frames, sseFrame{Raw: "[DONE]"})
			continue
		}
		var frame sseFrame
		if err := json.Unmarshal([]byte(payload), &frame); err != nil {
			t.Fatalf("SSE payload is not JSON: %v (%s)", err, payload)
		}
		frame.Raw = payload
		frames = append(frames, frame)
	}
	return frames
}

func TestStreamEmitsOpenAISSEFrames(t *testing.T) {
	mux, secret := newStreamingMux(t, "")

	body := chatBody("salut")
	body["stream"] = true

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, request(t, http.MethodPost, "/v1/chat/completions", secret, body))

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("expected text/event-stream, got %q", ct)
	}
	for header, want := range map[string]string{
		"Cache-Control":     "no-cache",
		"X-Accel-Buffering": "no",
	} {
		if got := rec.Header().Get(header); got != want {
			t.Errorf("expected %s: %q, got %q", header, want, got)
		}
	}

	frames := parseSSE(t, rec.Body.String())
	if len(frames) < 4 {
		t.Fatalf("expected at least role + 2 deltas + finish, got %d frames: %s", len(frames), rec.Body)
	}

	// Le flux doit se terminer par [DONE].
	if last := frames[len(frames)-1]; last.Raw != "[DONE]" {
		t.Fatalf("the stream must end with [DONE], got %q", last.Raw)
	}
	data := frames[:len(frames)-1]

	// Premiere trame : le role assistant, sans texte.
	if data[0].Choices[0].Delta.Role != "assistant" {
		t.Errorf("first frame must carry the assistant role, got %q", data[0].Choices[0].Delta.Role)
	}
	if data[0].Choices[0].Delta.Content != "" {
		t.Errorf("first frame must not carry content, got %q", data[0].Choices[0].Delta.Content)
	}

	// Toutes les trames de contenu partagent le meme id et le meme objet.
	var assembled strings.Builder
	for i, frame := range data {
		if frame.Object != "chat.completion.chunk" {
			t.Errorf("frame %d: expected chat.completion.chunk, got %q", i, frame.Object)
		}
		if frame.ID != data[0].ID {
			t.Errorf("frame %d: id must stay constant, got %q vs %q", i, frame.ID, data[0].ID)
		}
		if frame.Created != data[0].Created {
			t.Errorf("frame %d: created must stay constant, got %d vs %d", i, frame.Created, data[0].Created)
		}
		if frame.Usage != nil {
			t.Errorf("frame %d: usage must be absent when not requested", i)
		}
		assembled.WriteString(frame.Choices[0].Delta.Content)
	}

	if assembled.String() != "bonjour" {
		t.Errorf("deltas must reassemble the provider reply, got %q", assembled.String())
	}

	// Derniere trame de contenu : finish_reason.
	finish := data[len(data)-1].Choices[0]
	if finish.FinishReason == nil || *finish.FinishReason != "stop" {
		t.Errorf("last frame must carry finish_reason stop, got %v", finish.FinishReason)
	}
}

func TestStreamIncludeUsageSendsFinalUsageFrame(t *testing.T) {
	mux, secret := newStreamingMux(t, "")

	body := chatBody("salut")
	body["stream"] = true
	body["stream_options"] = map[string]any{"include_usage": true}

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, request(t, http.MethodPost, "/v1/chat/completions", secret, body))

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body)
	}

	frames := parseSSE(t, rec.Body.String())
	data := frames[:len(frames)-1]

	usage := data[len(data)-1].Usage
	if usage == nil {
		t.Fatalf("last frame must carry usage when stream_options.include_usage is set: %s", rec.Body)
	}
	if len(data[len(data)-1].Choices) != 0 {
		t.Errorf("the usage frame must have an empty choices array, got %d", len(data[len(data)-1].Choices))
	}
	if usage.TotalTokens != usage.PromptTokens+usage.CompletionTokens {
		t.Errorf("total_tokens must be the sum of the two, got %+v", usage)
	}
}

func TestStreamWithoutIncludeUsageSendsNoUsageFrame(t *testing.T) {
	mux, secret := newStreamingMux(t, "")

	body := chatBody("salut")
	body["stream"] = true

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, request(t, http.MethodPost, "/v1/chat/completions", secret, body))

	for i, frame := range parseSSE(t, rec.Body.String()) {
		if frame.Usage != nil {
			t.Errorf("frame %d: usage must be absent by default", i)
		}
	}
}

// Une cle invalide doit produire un 401 JSON, jamais un flux : l'authentification
// est verifiee avant l'ouverture du flux.
func TestStreamRequiresAuthentication(t *testing.T) {
	mux, _ := newStreamingMux(t, "master-key")

	body := chatBody("salut")
	body["stream"] = true

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, request(t, http.MethodPost, "/v1/chat/completions", "", body))

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d: %s", rec.Code, rec.Body)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("an unauthenticated request must not open a stream, got %q", ct)
	}
	e := decodeError(t, rec.Body.Bytes())
	if e["type"] != "authentication_error" {
		t.Errorf(`expected authentication_error, got %v`, e["type"])
	}
}

func TestStreamWithoutMessagesReturnsInvalidRequestError(t *testing.T) {
	mux, secret := newStreamingMux(t, "")

	body := map[string]any{"model": "gemma4:e4b", "messages": []any{}, "stream": true}

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, request(t, http.MethodPost, "/v1/chat/completions", secret, body))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body)
	}
	e := decodeError(t, rec.Body.Bytes())
	if e["type"] != "invalid_request_error" {
		t.Errorf(`expected invalid_request_error, got %v`, e["type"])
	}
	if e["param"] != "messages" {
		t.Errorf(`expected param "messages", got %v`, e["param"])
	}
}

// Le repli "/v1/" doit rester le 404 JSON d'OpenAI et non le texte brut de
// net/http, que les SDK ne savent pas lire.
func TestUnknownV1RouteReturnsJSONError(t *testing.T) {
	mux, _ := newOpenAIMux(t, "")

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, request(t, http.MethodPost, "/v1/unknown_endpoint", "master-key", map[string]any{}))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("unknown /v1 route must answer JSON, got %q", ct)
	}
	e := decodeError(t, rec.Body.Bytes())
	if e["code"] != "invalid_url" {
		t.Errorf("expected code invalid_url, got %v", e["code"])
	}
	if msg, _ := e["message"].(string); !strings.Contains(msg, "/v1/unknown_endpoint") {
		t.Errorf("message should echo the requested URL, got %q", msg)
	}
}

// Le repli ne doit pas masque les routes declarees, y compris avec un
// segment de modele absent.
func TestFallbackDoesNotShadowDeclaredRoutes(t *testing.T) {
	mux, secret := newOpenAIMux(t, "")

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, request(t, http.MethodGet, "/v1/models/", secret, nil))

	if rec.Code == http.StatusOK {
		t.Fatalf("/v1/models/ must not be served as the model list: %s", rec.Body)
	}
	if !strings.Contains(rec.Body.String(), `"error"`) {
		t.Errorf("expected an error envelope, got %s", rec.Body)
	}
}

// Le chemin master doit refuser un corps vide comme le chemin cle de projet,
// en streaming comme hors streaming. Sans cela il renvoyait 200 et un flux SSE
// sans texte, indiscernable d'une reponse legitement vide.
func TestEmptyMessagesRejectedOnEveryPathAndMode(t *testing.T) {
	const master = "master-key"

	cases := []struct {
		name string
		auth string
	}{
		{"master", master},
		{"projet", ""},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			mux, secret := newMuxWithProvider(t, master, streamingProvider{})
			key := secret
			if c.auth == master {
				key = master
			}

			for _, stream := range []bool{false, true} {
				body := map[string]any{"model": "gemma4:e4b", "messages": []any{}}
				if stream {
					body["stream"] = true
				}

				rec := httptest.NewRecorder()
				mux.ServeHTTP(rec, request(t, http.MethodPost, "/v1/chat/completions", key, body))

				if rec.Code != http.StatusBadRequest {
					t.Fatalf("stream=%v: expected 400, got %d (%s)", stream, rec.Code, rec.Body)
				}
				if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
					t.Errorf("stream=%v: expected a JSON error, got %q", stream, ct)
				}
				e := decodeError(t, rec.Body.Bytes())
				if e["type"] != "invalid_request_error" {
					t.Errorf("stream=%v: expected invalid_request_error, got %v", stream, e["type"])
				}
				if e["param"] != "messages" {
					t.Errorf("stream=%v: expected param messages, got %v", stream, e["param"])
				}
			}
		})
	}
}

func TestEmbeddingsContract(t *testing.T) {
	mux, secret := newOpenAIMux(t, "")

	// 1. Success on /v1/embeddings
	body := map[string]any{
		"model": "text-embedding-3-small",
		"input": "The quick brown fox",
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, request(t, http.MethodPost, "/v1/embeddings", secret, body))

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("expected Content-Type application/json, got %q", ct)
	}

	var resp struct {
		Object string `json:"object"`
		Data   []struct {
			Object    string    `json:"object"`
			Embedding []float32 `json:"embedding"`
			Index     int       `json:"index"`
		} `json:"data"`
		Model string `json:"model"`
		Usage struct {
			PromptTokens int `json:"prompt_tokens"`
			TotalTokens  int `json:"total_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode embeddings response: %v", err)
	}
	if resp.Object != "list" {
		t.Errorf("expected object 'list', got %q", resp.Object)
	}
	if len(resp.Data) == 0 {
		t.Fatalf("expected at least 1 embedding data item")
	}
	if resp.Data[0].Object != "embedding" {
		t.Errorf("expected data[0].object 'embedding', got %q", resp.Data[0].Object)
	}
	if len(resp.Data[0].Embedding) == 0 {
		t.Errorf("expected non-empty float embedding")
	}
	if resp.Usage.TotalTokens == 0 {
		t.Errorf("expected non-zero total tokens")
	}

	// 2. Success on /embeddings alias
	recAlias := httptest.NewRecorder()
	mux.ServeHTTP(recAlias, request(t, http.MethodPost, "/embeddings", secret, body))
	if recAlias.Code != http.StatusOK {
		t.Fatalf("expected 200 on /embeddings alias, got %d: %s", recAlias.Code, recAlias.Body)
	}

	// 3. Unauthorized when missing or wrong key
	recUnauth := httptest.NewRecorder()
	mux.ServeHTTP(recUnauth, request(t, http.MethodPost, "/v1/embeddings", "sk-invalid", body))
	if recUnauth.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 on invalid key, got %d", recUnauth.Code)
	}
	errUnauth := decodeError(t, recUnauth.Body.Bytes())
	if errUnauth["type"] != "authentication_error" {
		t.Errorf("expected authentication_error, got %v", errUnauth["type"])
	}

	// 4. Missing input returns 400
	recMissing := httptest.NewRecorder()
	mux.ServeHTTP(recMissing, request(t, http.MethodPost, "/v1/embeddings", secret, map[string]any{"model": "test"}))
	if recMissing.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 on missing input, got %d", recMissing.Code)
	}
	errMissing := decodeError(t, recMissing.Body.Bytes())
	if errMissing["type"] != "invalid_request_error" {
		t.Errorf("expected invalid_request_error, got %v", errMissing["type"])
	}
	if errMissing["param"] != "input" {
		t.Errorf("expected param 'input', got %v", errMissing["param"])
	}

	// 5. Method not allowed on GET
	recMethod := httptest.NewRecorder()
	mux.ServeHTTP(recMethod, request(t, http.MethodGet, "/v1/embeddings", secret, nil))
	if recMethod.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405 on GET, got %d", recMethod.Code)
	}
}
