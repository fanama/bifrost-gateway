package infrastructure

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"bridge-gateway/domain"
	"github.com/maximhq/bifrost/core/schemas"
)

func TestChatParamsMapConfigToChatParameters(t *testing.T) {
	temp := 0.4
	topP := 0.85
	maxTokens := 77
	freq := -0.2
	pres := 0.5

	cfg := &domain.ChatConfig{
		Provider:         "ollama",
		Model:            "gemma4:e4b",
		Temperature:      &temp,
		TopP:             &topP,
		MaxTokens:        &maxTokens,
		FrequencyPenalty: &freq,
		PresencePenalty:  &pres,
		ResponseFormat:   "json_object",
	}

	params := chatParams(cfg)
	if params == nil {
		t.Fatal("expected non-nil params")
	}
	if params.Temperature == nil || *params.Temperature != 0.4 {
		t.Errorf("temperature: got %v", params.Temperature)
	}
	if params.TopP == nil || *params.TopP != 0.85 {
		t.Errorf("top_p: got %v", params.TopP)
	}
	if params.MaxCompletionTokens == nil || *params.MaxCompletionTokens != 77 {
		t.Errorf("max_tokens: got %v", params.MaxCompletionTokens)
	}
	if params.FrequencyPenalty == nil || *params.FrequencyPenalty != -0.2 {
		t.Errorf("frequency_penalty: got %v", params.FrequencyPenalty)
	}
	if params.PresencePenalty == nil || *params.PresencePenalty != 0.5 {
		t.Errorf("presence_penalty: got %v", params.PresencePenalty)
	}
	if params.ResponseFormat == nil {
		t.Fatal("expected response_format set")
	}
	rf, ok := (*params.ResponseFormat).(map[string]any)
	if !ok || rf["type"] != "json_object" {
		t.Errorf("response_format: got %v", *params.ResponseFormat)
	}
}

func TestChatParamsNilWhenNoParamConfigured(t *testing.T) {
	cfg := &domain.ChatConfig{Provider: "ollama", Model: "gemma4:e4b"}
	if params := chatParams(cfg); params != nil {
		t.Errorf("expected nil params for unconfigured config, got %#v", params)
	}
}

func TestChatParamsSingleFieldStillEmitted(t *testing.T) {
	temp := 0.4
	cfg := &domain.ChatConfig{Provider: "ollama", Model: "gemma4:e4b", Temperature: &temp}
	params := chatParams(cfg)
	if params == nil {
		t.Fatal("expected params when only temperature is set")
	}
	if params.Temperature == nil || *params.Temperature != 0.4 {
		t.Errorf("temperature: got %v", params.Temperature)
	}
}

func TestDynamicAccountDefaultsOllamaBaseURL(t *testing.T) {
	acc := NewDynamicAccount(&domain.ChatConfig{
		Provider: "ollama",
		Model:    "gemma4:e4b",
		APIKey:   "local",
	})
	pc, err := acc.GetConfigForProvider(schemas.Ollama)
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	if pc.NetworkConfig.BaseURL != "http://localhost:11434" {
		t.Errorf("expected default ollama base URL, got %q", pc.NetworkConfig.BaseURL)
	}
}

func TestToModelProviderSupported(t *testing.T) {
	cases := []struct {
		input string
		want  schemas.ModelProvider
	}{
		{"google", schemas.Gemini},
		{"gemini", schemas.Gemini},
		{"mistral", schemas.Mistral},
		{"mistralai", schemas.Mistral},
		{"openai", schemas.OpenAI},
		{"ollama", schemas.Ollama},
		{"anthropic", schemas.Anthropic},
		{"claude", schemas.Anthropic},
		{"groq", schemas.Groq},
		{"azure", schemas.Azure},
		{"vertex", schemas.Vertex},
		{"my-custom-gateway", schemas.OpenAI},
		{"custom-proxy", schemas.OpenAI},
	}

	for _, tc := range cases {
		p, err := ToModelProvider(tc.input)
		if err != nil {
			t.Errorf("ToModelProvider(%q) unexpected err: %v", tc.input, err)
		}
		if p != tc.want {
			t.Errorf("ToModelProvider(%q) = %v, want %v", tc.input, p, tc.want)
		}
	}
}

func TestResolveEnv(t *testing.T) {
	t.Setenv("CUSTOM_API_KEY", "secret-token-123")

	cases := []struct {
		input string
		want  string
	}{
		{"{env:CUSTOM_API_KEY}", "secret-token-123"},
		{"env:CUSTOM_API_KEY", "secret-token-123"},
		{"${CUSTOM_API_KEY}", "secret-token-123"},
		{"$CUSTOM_API_KEY", "secret-token-123"},
		{"plain-secret", "plain-secret"},
		{"", ""},
	}

	for _, tc := range cases {
		got := ResolveEnv(tc.input)
		if got != tc.want {
			t.Errorf("ResolveEnv(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestDefaultBaseURLFor(t *testing.T) {
	// Bifrost appends "/v1/chat/completions" to these, so the defaults must not
	// carry the version segment themselves.
	cases := map[schemas.ModelProvider]string{
		schemas.Mistral:   "https://api.mistral.ai",
		schemas.OpenAI:    "https://api.openai.com",
		schemas.Anthropic: "https://api.anthropic.com",
		schemas.Groq:      "https://api.groq.com/openai",
		schemas.Ollama:    "http://localhost:11434",
		// Gemini is the exception: Bifrost appends a version-less "/models" path.
		schemas.Gemini: "https://generativelanguage.googleapis.com/v1beta",
	}
	for provider, want := range cases {
		if got := defaultBaseURLFor(provider); got != want {
			t.Errorf("defaultBaseURLFor(%v) = %q, want %q", provider, got, want)
		}
	}
}

// --- Cache en memoire ---

// clockFake permet de faire expirer des entrees sans attendre.
type clockFake struct{ now time.Time }

func (c *clockFake) Now() time.Time          { return c.now }
func (c *clockFake) advance(d time.Duration) { c.now = c.now.Add(d) }

func TestMemoryCacheStoresAndReturns(t *testing.T) {
	c := NewMemoryCache(MemoryCacheOptions{})
	defer c.Stop()

	c.Set("k", []byte("v"), time.Minute)
	got, ok := c.Get("k")
	if !ok || string(got) != "v" {
		t.Fatalf("expected miss false and value v, got %q/%v", got, ok)
	}

	if _, ok := c.Get("absent"); ok {
		t.Error("expected miss for unknown key")
	}
}

// Une valeur rendue par Get ne doit pas pouvoir muter l'entree : deux
// lectures successives doivent voir la meme valeur.
func TestMemoryCacheReturnsCopy(t *testing.T) {
	c := NewMemoryCache(MemoryCacheOptions{})
	defer c.Stop()

	c.Set("k", []byte("original"), time.Minute)
	first, _ := c.Get("k")
	first[0] = 'X'

	second, _ := c.Get("k")
	if string(second) != "original" {
		t.Errorf("cache aliased caller buffer: got %q", second)
	}
}

func TestMemoryCacheExpiresAfterTTL(t *testing.T) {
	clock := &clockFake{now: time.Unix(0, 0)}
	c := NewMemoryCache(MemoryCacheOptions{Now: clock.Now})
	defer c.Stop()

	c.Set("k", []byte("v"), 30*time.Second)

	clock.advance(29 * time.Second)
	if _, ok := c.Get("k"); !ok {
		t.Fatal("entry expired too early")
	}

	clock.advance(2 * time.Second)
	if _, ok := c.Get("k"); ok {
		t.Error("expected entry to be expired")
	}
}

// Un TTL invalide desactive l'entree au lieu de la stocker sans limite.
func TestMemoryCacheIgnoresNonPositiveTTL(t *testing.T) {
	c := NewMemoryCache(MemoryCacheOptions{})
	defer c.Stop()

	c.Set("zero", []byte("v"), 0)
	c.Set("negative", []byte("v"), -time.Second)

	if _, ok := c.Get("zero"); ok {
		t.Error("ttl=0 must not cache")
	}
	if _, ok := c.Get("negative"); ok {
		t.Error("negative ttl must not cache")
	}
}

func TestMemoryCacheEvictsLeastRecentlyUsed(t *testing.T) {
	c := NewMemoryCache(MemoryCacheOptions{MaxEntries: 2})
	defer c.Stop()

	c.Set("a", []byte("1"), time.Minute)
	c.Set("b", []byte("2"), time.Minute)
	// "a" devient le plus recemment utilise.
	c.Get("a")
	c.Set("c", []byte("3"), time.Minute)

	if _, ok := c.Get("b"); ok {
		t.Error("expected b to be evicted")
	}
	if _, ok := c.Get("a"); !ok {
		t.Error("expected a to be retained")
	}
	if _, ok := c.Get("c"); !ok {
		t.Error("expected c to be cached")
	}
}

func TestMemoryCacheDeleteAndPrefix(t *testing.T) {
	c := NewMemoryCache(MemoryCacheOptions{})
	defer c.Stop()

	keys := domain.NewCacheKeyFactory("config")
	c.Set(keys.Key("p1"), []byte("1"), time.Minute)
	c.Set(keys.Key("p2"), []byte("2"), time.Minute)

	// Delete sur une cle absente ne doit pas paniquer.
	c.Delete("missing")

	c.Delete(keys.Key("p1"))
	if _, ok := c.Get(keys.Key("p1")); ok {
		t.Error("expected explicit delete to remove entry")
	}
	if _, ok := c.Get(keys.Key("p2")); !ok {
		t.Error("unrelated key must survive")
	}

	c.DeletePrefix(keys.Prefix())
	if _, ok := c.Get(keys.Key("p2")); ok {
		t.Error("expected prefix invalidation to clear namespace")
	}
}

// Le sweep doit liberer les entrees expirees jamais relues.
func TestMemoryCacheSweepRemovesExpired(t *testing.T) {
	clock := &clockFake{now: time.Unix(0, 0)}
	c := NewMemoryCache(MemoryCacheOptions{Now: clock.Now, SweepInterval: time.Millisecond})
	defer c.Stop()

	c.Set("k", []byte("v"), 20*time.Millisecond)
	clock.advance(time.Hour)

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		c.mu.Lock()
		remaining := len(c.entries)
		c.mu.Unlock()
		if remaining == 0 {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatal("expired entry was never swept")
}

// --- Decorateurs de cache des repositories ---

// fakeConfigStore compte les appels pour prouver que le decorateur evite
// reellement de toucher la base. Chaque methode du port est implementee : les
// assertions de compilation verifient le contrat, et surtout les methodes non
// implementees neTimeouts paniquent pas sur un embed d'interface nil.
type fakeConfigStore struct {
	calls  int
	active *domain.ChatConfig
	stored map[string]*domain.ChatConfig
}

func newFakeConfigStore() *fakeConfigStore {
	return &fakeConfigStore{stored: map[string]*domain.ChatConfig{}}
}

func (f *fakeConfigStore) List(ctx context.Context) ([]domain.ChatConfig, error) {
	f.calls++
	out := make([]domain.ChatConfig, 0, len(f.stored))
	for _, cfg := range f.stored {
		out = append(out, *cfg)
	}
	return out, nil
}

func (f *fakeConfigStore) Get(ctx context.Context, id string) (*domain.ChatConfig, error) {
	f.calls++
	cfg, ok := f.stored[id]
	if !ok {
		return nil, domain.ErrConfigNotFound
	}
	return cfg, nil
}

func (f *fakeConfigStore) Create(ctx context.Context, cfg *domain.ChatConfig) error {
	f.calls++
	f.stored[cfg.ID] = cfg
	return nil
}

func (f *fakeConfigStore) Update(ctx context.Context, cfg *domain.ChatConfig) error {
	f.calls++
	f.stored[cfg.ID] = cfg
	return nil
}

func (f *fakeConfigStore) Delete(ctx context.Context, id string) error {
	f.calls++
	delete(f.stored, id)
	return nil
}

func (f *fakeConfigStore) ListByProject(ctx context.Context, projectID string) ([]domain.ChatConfig, error) {
	f.calls++
	out := []domain.ChatConfig{}
	for _, cfg := range f.stored {
		if cfg.ProjectID == projectID {
			out = append(out, *cfg)
		}
	}
	return out, nil
}

func (f *fakeConfigStore) GetActiveByProject(ctx context.Context, projectID string) (*domain.ChatConfig, error) {
	f.calls++
	if f.active == nil {
		return nil, domain.ErrNoActiveConfig
	}
	return f.active, nil
}

func (f *fakeConfigStore) SetActive(ctx context.Context, projectID string, id string) error {
	f.calls++
	f.active = f.stored[id]
	return nil
}

func TestCachedConfigStoreServesSecondGetFromCache(t *testing.T) {
	inner := newFakeConfigStore()
	cache := NewMemoryCache(MemoryCacheOptions{})
	defer cache.Stop()
	store := NewCachedConfigStore(inner, cache, time.Minute)
	ctx := context.Background()

	if _, err := store.Get(ctx, "cfg-1"); !errors.Is(err, domain.ErrConfigNotFound) {
		t.Fatalf("warm cache: expected not found, got %v", err)
	}
	if inner.calls != 1 {
		t.Fatalf("expected one store call, got %d", inner.calls)
	}

	inner.stored["cfg-1"] = &domain.ChatConfig{ID: "cfg-1", Model: "gemma4:e4b"}
	first, err := store.Get(ctx, "cfg-1")
	if err != nil {
		t.Fatalf("first get: %v", err)
	}
	if first.Model != "gemma4:e4b" {
		t.Fatalf("unexpected model %q", first.Model)
	}

	second, err := store.Get(ctx, "cfg-1")
	if err != nil {
		t.Fatalf("second get: %v", err)
	}
	if second.Model != "gemma4:e4b" {
		t.Errorf("cached value corrupted: %q", second.Model)
	}
	if inner.calls != 2 {
		t.Errorf("expected the second get to be served from cache, got %d calls", inner.calls)
	}
}

// Une update doit invalider l'entree : sinon l'UI continuerait de servir
// l'ancienne version de la configuration.
func TestCachedConfigStoreInvalidatesOnUpdate(t *testing.T) {
	inner := newFakeConfigStore()
	inner.stored["cfg-1"] = &domain.ChatConfig{ID: "cfg-1", ProjectID: "p1", Model: "ancien"}
	cache := NewMemoryCache(MemoryCacheOptions{})
	defer cache.Stop()
	store := NewCachedConfigStore(inner, cache, time.Minute)
	ctx := context.Background()

	if _, err := store.Get(ctx, "cfg-1"); err != nil {
		t.Fatalf("warm cache: %v", err)
	}

	updated := &domain.ChatConfig{ID: "cfg-1", ProjectID: "p1", Model: "nouveau"}
	if err := store.Update(ctx, updated); err != nil {
		t.Fatalf("update: %v", err)
	}

	got, err := store.Get(ctx, "cfg-1")
	if err != nil {
		t.Fatalf("get after update: %v", err)
	}
	if got.Model != "nouveau" {
		t.Errorf("expected the updated value, got %q", got.Model)
	}
}

// Idem pour la config active d'un projet : SetActive invalide le namespace.
func TestCachedConfigStoreInvalidatesActiveOnSetActive(t *testing.T) {
	inner := newFakeConfigStore()
	inner.stored["cfg-1"] = &domain.ChatConfig{ID: "cfg-1", ProjectID: "p1"}
	inner.stored["cfg-2"] = &domain.ChatConfig{ID: "cfg-2", ProjectID: "p1"}
	inner.active = inner.stored["cfg-1"]

	cache := NewMemoryCache(MemoryCacheOptions{})
	defer cache.Stop()
	store := NewCachedConfigStore(inner, cache, time.Minute)
	ctx := context.Background()

	if _, err := store.GetActiveByProject(ctx, "p1"); err != nil {
		t.Fatalf("warm active cache: %v", err)
	}
	if err := store.SetActive(ctx, "p1", "cfg-2"); err != nil {
		t.Fatalf("set active: %v", err)
	}

	got, err := store.GetActiveByProject(ctx, "p1")
	if err != nil {
		t.Fatalf("get active after switch: %v", err)
	}
	if got.ID != "cfg-2" {
		t.Errorf("expected cfg-2 after SetActive, got %s", got.ID)
	}
}

func TestCachedConfigStoreDoesNotCacheMissingActive(t *testing.T) {
	inner := newFakeConfigStore()
	cache := NewMemoryCache(MemoryCacheOptions{})
	defer cache.Stop()
	store := NewCachedConfigStore(inner, cache, time.Minute)
	ctx := context.Background()

	if _, err := store.GetActiveByProject(ctx, "p1"); !errors.Is(err, domain.ErrNoActiveConfig) {
		t.Fatalf("expected ErrNoActiveConfig, got %v", err)
	}

	// L'UI active une config entre les deux appels : la deuxieme lecture doit
	// la voir, donc l'absence ne doit pas etre memorisee.
	inner.active = &domain.ChatConfig{ID: "cfg-1", ProjectID: "p1"}
	got, err := store.GetActiveByProject(ctx, "p1")
	if err != nil {
		t.Fatalf("expected config to appear, got %v", err)
	}
	if got.ID != "cfg-1" {
		t.Errorf("unexpected config %q", got.ID)
	}
}

// --- Cache des reponses LLM ---

type countingLLM struct {
	calls    int
	reply    string
	failWith error
}

func (c *countingLLM) Chat(ctx context.Context, cfg *domain.ChatConfig, messages []domain.ChatMessage) (string, error) {
	c.calls++
	if c.failWith != nil {
		return "", c.failWith
	}
	return c.reply, nil
}

func TestCachedLLMProviderCachesIdenticalRequests(t *testing.T) {
	inner := &countingLLM{reply: "hello"}
	cache := NewMemoryCache(MemoryCacheOptions{})
	defer cache.Stop()
	provider := NewCachedLLMProvider(inner, cache, 5*time.Minute)

	cfg := &domain.ChatConfig{Provider: "ollama", Model: "gemma4:e4b"}
	msgs := []domain.ChatMessage{{Role: "user", Content: "bonjour"}}

	for i := 0; i < 3; i++ {
		reply, err := provider.Chat(context.Background(), cfg, msgs)
		if err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
		if reply != "hello" {
			t.Errorf("call %d: unexpected reply %q", i, reply)
		}
	}
	if inner.calls != 1 {
		t.Errorf("expected one provider call, got %d", inner.calls)
	}
}

// Un message different doit produire une cle differente.
func TestCachedLLMProviderKeysOnMessages(t *testing.T) {
	inner := &countingLLM{reply: "ok"}
	cache := NewMemoryCache(MemoryCacheOptions{})
	defer cache.Stop()
	provider := NewCachedLLMProvider(inner, cache, 5*time.Minute)

	cfg := &domain.ChatConfig{Provider: "ollama", Model: "gemma4:e4b"}
	if _, err := provider.Chat(context.Background(), cfg, []domain.ChatMessage{{Role: "user", Content: "a"}}); err != nil {
		t.Fatalf("first: %v", err)
	}
	if _, err := provider.Chat(context.Background(), cfg, []domain.ChatMessage{{Role: "user", Content: "b"}}); err != nil {
		t.Fatalf("second: %v", err)
	}
	if inner.calls != 2 {
		t.Errorf("expected two provider calls, got %d", inner.calls)
	}
}

// Un changement de temperature change la cle : la reponse d'un appel non
// deterministe ne doit pas servir une requete configuree differemment.
func TestCachedLLMProviderKeysOnSamplingParams(t *testing.T) {
	inner := &countingLLM{reply: "ok"}
	cache := NewMemoryCache(MemoryCacheOptions{})
	defer cache.Stop()
	provider := NewCachedLLMProvider(inner, cache, 5*time.Minute)

	msgs := []domain.ChatMessage{{Role: "user", Content: "a"}}
	cold := 0.5
	if _, err := provider.Chat(context.Background(),
		&domain.ChatConfig{Provider: "ollama", Model: "m", Temperature: &cold}, msgs); err != nil {
		t.Fatalf("first: %v", err)
	}
	if _, err := provider.Chat(context.Background(),
		&domain.ChatConfig{Provider: "ollama", Model: "m"}, msgs); err != nil {
		t.Fatalf("second: %v", err)
	}
	if inner.calls != 2 {
		t.Errorf("expected two provider calls, got %d", inner.calls)
	}
}

// Une erreur ne doit jamais etre memorisee : un rate limit transitoire ne peut
// pas etre rejoue pendant toute la duree du TTL.
func TestCachedLLMProviderDoesNotCacheErrors(t *testing.T) {
	inner := &countingLLM{failWith: errors.New("rate limited")}
	cache := NewMemoryCache(MemoryCacheOptions{})
	defer cache.Stop()
	provider := NewCachedLLMProvider(inner, cache, 5*time.Minute)

	cfg := &domain.ChatConfig{Provider: "ollama", Model: "m"}
	msgs := []domain.ChatMessage{{Role: "user", Content: "a"}}

	for i := 0; i < 2; i++ {
		if _, err := provider.Chat(context.Background(), cfg, msgs); err == nil {
			t.Fatalf("call %d: expected error", i)
		}
	}
	if inner.calls != 2 {
		t.Errorf("errors must not be cached, got %d provider calls", inner.calls)
	}
}

// Un TTL nul desactive le cache : le provider est alors appele a chaque fois.
func TestCachedLLMProviderDisabledWhenTTLIsZero(t *testing.T) {
	inner := &countingLLM{reply: "ok"}
	cache := NewMemoryCache(MemoryCacheOptions{})
	defer cache.Stop()
	provider := NewCachedLLMProvider(inner, cache, 0)

	cfg := &domain.ChatConfig{Provider: "ollama", Model: "m"}
	msgs := []domain.ChatMessage{{Role: "user", Content: "a"}}
	for i := 0; i < 2; i++ {
		if _, err := provider.Chat(context.Background(), cfg, msgs); err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
	}
	if inner.calls != 2 {
		t.Errorf("expected two provider calls, got %d", inner.calls)
	}
}

// --- SQLite : schema et requetes ---

func TestMigrateIsIdempotent(t *testing.T) {
	db, err := OpenDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	if err := Migrate(ctx, db); err != nil {
		t.Fatalf("first migrate: %v", err)
	}
	if err := Migrate(ctx, db); err != nil {
		t.Fatalf("second migrate must be a no-op, got %v", err)
	}
}

// Les predicats pousses jusqu'a SQLite doivent utiliser json_extract, donc les
// filtres par project_id et digest doivent fonctionner comme en memoire.
func TestSQLStoresFilterOnJSONColumns(t *testing.T) {
	ctx := context.Background()
	db, err := OpenDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	if err := Migrate(ctx, db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	keys := NewAPIKeyStore(db)
	project := "project-a"
	secret, prefix, digest := domain.NewAPIKey()
	for i, pid := range []string{"project-a", "project-a", "project-b"} {
		d := digest
		if i == 2 {
			d = domain.HashAPIKey("autre")
		}
		key := &domain.APIKey{
			ID:        "key-" + prefix + string(rune('0'+i)),
			ProjectID: pid,
			Name:      "k",
			Prefix:    prefix,
			Digest:    d,
		}
		if err := keys.Create(ctx, key); err != nil {
			t.Fatalf("create key: %v", err)
		}
	}

	byProject, err := keys.ListByProject(ctx, project)
	if err != nil {
		t.Fatalf("list by project: %v", err)
	}
	if len(byProject) != 2 {
		t.Fatalf("expected 2 keys for %s, got %d", project, len(byProject))
	}

	found, err := keys.FindByDigest(ctx, digest)
	if err != nil {
		t.Fatalf("find by digest: %v", err)
	}
	if found.ProjectID != project {
		t.Errorf("unexpected project %q", found.ProjectID)
	}
	if !found.Matches(secret) {
		t.Error("stored key must match the original secret")
	}
}

func TestSQLConfigStoreActiveSelection(t *testing.T) {
	ctx := context.Background()
	db, err := OpenDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	if err := Migrate(ctx, db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	store := NewChatConfigStore(db)
	mk := func(id string, active bool) *domain.ChatConfig {
		return &domain.ChatConfig{
			ID: id, ProjectID: "p1", Name: id, Provider: "ollama",
			Model: "gemma4:e4b", Active: active,
		}
	}
	if err := store.Create(ctx, mk("cfg-1", true)); err != nil {
		t.Fatalf("create cfg-1: %v", err)
	}
	if err := store.Create(ctx, mk("cfg-2", false)); err != nil {
		t.Fatalf("create cfg-2: %v", err)
	}

	active, err := store.GetActiveByProject(ctx, "p1")
	if err != nil {
		t.Fatalf("get active: %v", err)
	}
	if active.ID != "cfg-1" {
		t.Fatalf("expected cfg-1 active, got %s", active.ID)
	}

	if err := store.SetActive(ctx, "p1", "cfg-2"); err != nil {
		t.Fatalf("set active: %v", err)
	}

	active, err = store.GetActiveByProject(ctx, "p1")
	if err != nil {
		t.Fatalf("get active after switch: %v", err)
	}
	if active.ID != "cfg-2" {
		t.Fatalf("expected cfg-2 active after switch, got %s", active.ID)
	}

	// SetActive sur un identifiant inconnu ne doit pas laisser deux configs
	// actives ni ecraser l'etat existant.
	if err := store.SetActive(ctx, "p1", "absent"); !errors.Is(err, domain.ErrConfigNotFound) {
		t.Fatalf("expected ErrConfigNotFound, got %v", err)
	}
	active, err = store.GetActiveByProject(ctx, "p1")
	if err != nil {
		t.Fatalf("get active after failed switch: %v", err)
	}
	if active.ID != "cfg-2" {
		t.Errorf("failed SetActive must not change the selection, got %s", active.ID)
	}
}

func TestSQLStoreGetMissingReturnsNotFound(t *testing.T) {
	ctx := context.Background()
	db, err := OpenDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	if err := Migrate(ctx, db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	projects := NewProjectStore(db)
	if _, err := projects.Get(ctx, "absent"); !errors.Is(err, domain.ErrProjectNotFound) {
		t.Errorf("expected ErrProjectNotFound, got %v", err)
	}
	if err := projects.Update(ctx, &domain.Project{ID: "absent"}); !errors.Is(err, domain.ErrProjectNotFound) {
		t.Errorf("update of missing project: expected ErrProjectNotFound, got %v", err)
	}
	if err := projects.Delete(ctx, "absent"); !errors.Is(err, domain.ErrProjectNotFound) {
		t.Errorf("delete of missing project: expected ErrProjectNotFound, got %v", err)
	}
}

func TestCatalogStoreListByProvider(t *testing.T) {
	ctx := context.Background()
	db, err := OpenDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	if err := Migrate(ctx, db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	catalog, err := NewModelCatalogStore(db, nil)
	if err != nil {
		t.Fatalf("new catalog store: %v", err)
	}
	for _, m := range []domain.CatalogModel{
		{ID: "m1", Name: "gemma4:e4b", Provider: "ollama"},
		{ID: "m2", Name: "gpt-4o", Provider: "openai"},
	} {
		if err := catalog.Create(ctx, &m); err != nil {
			t.Fatalf("create %s: %v", m.ID, err)
		}
	}

	ollama, err := catalog.ListByProvider(ctx, "ollama")
	if err != nil {
		t.Fatalf("list by provider: %v", err)
	}
	if len(ollama) != 1 || ollama[0].Name != "gemma4:e4b" {
		t.Fatalf("unexpected ollama models: %#v", ollama)
	}
}

// --- Import des anciens fichiers JSON ---

func TestImportLegacyJSONIntoSQLite(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	projects := []domain.Project{{ID: "p1", Name: "Data"}}
	writeJSON(t, filepath.Join(dir, "projects.json"), projects)

	keys := []domain.APIKey{{ID: "k1", ProjectID: "p1", Name: "key", Digest: "abc"}}
	writeJSON(t, filepath.Join(dir, "apikeys.json"), keys)

	db, err := OpenDB(filepath.Join(t.TempDir(), "bridge.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	if err := Migrate(ctx, db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	counts, err := ImportLegacyStoreData(ctx, db, dir)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if counts["projects"] != 1 || counts["apikeys"] != 1 {
		t.Fatalf("unexpected import counts: %#v", counts)
	}

	store := NewProjectStore(db)
	project, err := store.Get(ctx, "p1")
	if err != nil {
		t.Fatalf("get imported project: %v", err)
	}
	if project.Name != "Data" {
		t.Errorf("unexpected imported name %q", project.Name)
	}

	keyStore := NewAPIKeyStore(db)
	found, err := keyStore.FindByDigest(ctx, "abc")
	if err != nil {
		t.Fatalf("find imported key: %v", err)
	}
	if found.ID != "k1" {
		t.Errorf("unexpected imported key %q", found.ID)
	}

	// Relancer l'import ne doit rien dupliquer.
	if _, err := ImportLegacyStoreData(ctx, db, dir); err != nil {
		t.Fatalf("second import: %v", err)
	}
	all, err := store.List(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(all) != 1 {
		t.Fatalf("expected 1 project after re-import, got %d", len(all))
	}
}

func TestImportIgnoresMissingDirectory(t *testing.T) {
	ctx := context.Background()
	db, err := OpenDB(filepath.Join(t.TempDir(), "bridge.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	if err := Migrate(ctx, db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	counts, err := ImportLegacyStoreData(ctx, db, filepath.Join(t.TempDir(), "absent"))
	if err != nil {
		t.Fatalf("import from absent dir must be a no-op, got %v", err)
	}
	if len(counts) != 0 {
		t.Errorf("expected no counts, got %#v", counts)
	}
}

func writeJSON[T any](t *testing.T, path string, value T) {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestNormalizeBaseURL(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		// The reported bug: "/v1" duplicated into "/v1/v1/chat/completions".
		{"https://opencode.ai/zen/v1", "https://opencode.ai/zen"},
		{"https://api.mistral.ai/v1", "https://api.mistral.ai"},
		{"https://api.groq.com/openai/v1", "https://api.groq.com/openai"},
		{"https://openrouter.ai/api/v1", "https://openrouter.ai/api"},
		{"https://api.x.ai/v1/", "https://api.x.ai"},
		// Users may paste the full endpoint instead of the base.
		{"https://api.mistral.ai/v1/chat/completions", "https://api.mistral.ai"},
		// Already-correct bases are left untouched.
		{"https://opencode.ai/zen", "https://opencode.ai/zen"},
		{"http://localhost:11434", "http://localhost:11434"},
		{"https://impact-code-assist-dev.pfv.private.sfr.com", "https://impact-code-assist-dev.pfv.private.sfr.com"},
		// Version segments Bifrost does not append itself are preserved.
		{"https://generativelanguage.googleapis.com/v1beta", "https://generativelanguage.googleapis.com/v1beta"},
		// A host whose only segment is a version must not be emptied.
		{"https://v1", "https://v1"},
		{"", ""},
	}

	for _, tc := range cases {
		if got := normalizeBaseURL(tc.input); got != tc.want {
			t.Errorf("normalizeBaseURL(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestGetConfigForProviderNormalizesBaseURL(t *testing.T) {
	acc := NewDynamicAccount(&domain.ChatConfig{
		Provider: "open code",
		Model:    "big-pickle",
		APIKey:   "sk-test",
	}, "sk-test", "https://opencode.ai/zen/v1")

	pc, err := acc.GetConfigForProvider(schemas.OpenAI)
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	if pc.NetworkConfig.BaseURL != "https://opencode.ai/zen" {
		t.Errorf("base URL = %q, want %q", pc.NetworkConfig.BaseURL, "https://opencode.ai/zen")
	}
}

type fakeProviderRepo struct {
	providers []domain.Provider
}

func (f *fakeProviderRepo) List(_ context.Context) ([]domain.Provider, error) {
	return f.providers, nil
}
func (f *fakeProviderRepo) Get(_ context.Context, id string) (*domain.Provider, error) {
	for _, p := range f.providers {
		if p.ID == id || p.Name == id {
			return &p, nil
		}
	}
	return nil, domain.ErrProviderNotFound
}
func (f *fakeProviderRepo) Create(_ context.Context, _ *domain.Provider) error { return nil }
func (f *fakeProviderRepo) Update(_ context.Context, _ *domain.Provider) error { return nil }
func (f *fakeProviderRepo) Delete(_ context.Context, _ string) error           { return nil }

func TestDynamicAccountWithFallbackProviderKey(t *testing.T) {
	repo := &fakeProviderRepo{
		providers: []domain.Provider{
			{ID: "p1", Name: "mistral", BaseURL: "https://api.mistral.ai/v1", APIKey: "sk-mistral-key"},
		},
	}
	llm := NewBifrostLLMProvider(repo)
	cfg := &domain.ChatConfig{
		ID:       "cfg-1",
		Provider: "mistral",
		Model:    "mistral-large-latest",
	}

	p := llm.resolveProvider(context.Background(), cfg.Provider)
	if p == nil || p.APIKey != "sk-mistral-key" {
		t.Fatalf("expected resolved provider with API key, got %#v", p)
	}
}
