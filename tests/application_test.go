package tests

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"testing"

	"bridge-gateway/application"
	"bridge-gateway/domain"
	"bridge-gateway/infrastructure"
)

// newTestDB cree une base SQLite isolee par test (repertoire temporaire).
func newTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := infrastructure.OpenDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := infrastructure.Migrate(context.Background(), db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func NewTestStore(t *testing.T) *infrastructure.ChatConfigStore {
	t.Helper()
	return infrastructure.NewChatConfigStore(newTestDB(t))
}

func NewTestProject(t *testing.T) string {
	t.Helper()
	store := infrastructure.NewProjectStore(newTestDB(t))
	p, err := application.NewProjectUseCase(store).Create(context.Background(), "Test", "")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	return p.ID
}

func TestConfigCreateRequiresRequiredFields(t *testing.T) {
	uc := application.NewConfigUseCase(NewTestStore(t))

	_, err := uc.Create(context.Background(), &domain.ChatConfig{Name: "Local"})
	if err == nil {
		t.Fatal("expected validation error, got nil")
	}
	var verr *domain.ValidationError
	if !errors.As(err, &verr) {
		t.Fatalf("expected *ValidationError, got %T", err)
	}
	for _, want := range []string{"provider", "model"} {
		if !contains(verr.Fields, want) {
			t.Errorf("expected field %s in %v", want, verr.Fields)
		}
	}
}

func TestConfigCRUDLifecycle(t *testing.T) {
	pid := NewTestProject(t)
	uc := application.NewConfigUseCase(NewTestStore(t))
	ctx := context.Background()

	created, err := uc.Create(ctx, &domain.ChatConfig{
		Name:      "Ollama local",
		ProjectID: pid,
		Provider:  "ollama",
		Model:     "gemma4:e4b",
		BaseURL:   "http://localhost:11434",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if created.ID == "" {
		t.Fatal("expected generated ID")
	}

	got, err := uc.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Name != "Ollama local" {
		t.Errorf("expected name Ollama local, got %s", got.Name)
	}

	updated, err := uc.Update(ctx, created.ID, &domain.ChatConfig{
		Name:     "Ollama R2",
		Provider: "Ollama",
		Model:    "llama3",
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.Provider != "ollama" {
		t.Errorf("expected provider lowercased ollama, got %s", updated.Provider)
	}
	if updated.ID != created.ID {
		t.Errorf("expected same ID, got %s", updated.ID)
	}

	if _, err := uc.SetActive(ctx, pid, created.ID); err != nil {
		t.Fatalf("set active: %v", err)
	}
	active, _ := uc.Get(ctx, created.ID)
	if !active.Active {
		t.Error("expected config active")
	}

	if err := uc.Delete(ctx, created.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := uc.Get(ctx, created.ID); !errors.Is(err, domain.ErrConfigNotFound) {
		t.Fatalf("expected ErrConfigNotFound after delete, got %v", err)
	}
}

func TestConfigActiveExclusive(t *testing.T) {
	pid := NewTestProject(t)
	uc := application.NewConfigUseCase(NewTestStore(t))
	ctx := context.Background()

	a, _ := uc.Create(ctx, &domain.ChatConfig{Name: "A", ProjectID: pid, Provider: "ollama", Model: "m1"})
	b, _ := uc.Create(ctx, &domain.ChatConfig{Name: "B", ProjectID: pid, Provider: "openai", Model: "m2"})

	if _, err := uc.SetActive(ctx, pid, b.ID); err != nil {
		t.Fatalf("set active B: %v", err)
	}
	configs, _ := uc.List(ctx)
	activeCount := 0
	for _, c := range configs {
		if c.Active {
			activeCount++
			if c.ID != b.ID {
				t.Errorf("expected B active, got %s", c.ID)
			}
		}
	}
	if activeCount != 1 {
		t.Errorf("expected exactly 1 active, got %d", activeCount)
	}
	_ = a
}

type fakeLLM struct {
	reply        string
	called       bool
	lastCfg      *domain.ChatConfig
	lastMessages []domain.ChatMessage
}

func (f *fakeLLM) Chat(_ context.Context, cfg *domain.ChatConfig, messages []domain.ChatMessage) (domain.LLMResult, error) {
	f.called = true
	f.lastCfg = cfg
	f.lastMessages = messages
	return domain.LLMResult{Content: f.reply, Model: cfg.Model}, nil
}

func TestChatSendCallsLLMWithEnrichedMessages(t *testing.T) {
	store := NewTestStore(t)
	cfg, _ := application.NewConfigUseCase(store).Create(context.Background(), &domain.ChatConfig{
		Name:         "Local",
		Provider:     "ollama",
		Model:        "gemma4:e4b",
		SystemPrompt: "You are helpful",
	})

	fake := &fakeLLM{reply: "ma réponse"}
	uc := application.NewChatUseCase(store, domain.NewEnrichmentService(), fake)
	ctx := context.Background()

	history := []domain.ChatMessage{{Role: "user", Content: "previous"}}
	result, err := uc.Send(ctx, cfg.ID, history, "bonjour", nil)
	if err != nil {
		t.Fatalf("send: %v", err)
	}

	if !fake.called {
		t.Fatal("expected LLM provider called")
	}
	if fake.lastCfg.ID != cfg.ID {
		t.Errorf("expected config %s, got %s", cfg.ID, fake.lastCfg.ID)
	}

	if len(result.Messages) != 3 {
		t.Fatalf("expected 3 messages (user, user, assistant), got %d", len(result.Messages))
	}
	last := result.Messages[len(result.Messages)-1]
	if last.Role != "assistant" || last.Content != "ma réponse" {
		t.Errorf("expected assistant reply, got %s:%s", last.Role, last.Content)
	}
	if result.Model != cfg.Model {
		t.Errorf("expected served model %s, got %s", cfg.Model, result.Model)
	}
}

func TestConfigCreatesAndStoresGenerationParams(t *testing.T) {
	uc := application.NewConfigUseCase(NewTestStore(t))
	ctx := context.Background()

	temp := 0.7
	topP := 0.9
	maxTokens := 512
	freq := -0.2
	pres := 0.5

	created, err := uc.Create(ctx, &domain.ChatConfig{
		Name:             "Local tuné",
		Provider:         "ollama",
		Model:            "gemma4:e4b",
		SystemPrompt:     "Sois concis",
		Temperature:      &temp,
		TopP:             &topP,
		MaxTokens:        &maxTokens,
		FrequencyPenalty: &freq,
		PresencePenalty:  &pres,
		ResponseFormat:   "json_object",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	got, err := uc.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Temperature == nil || *got.Temperature != 0.7 {
		t.Errorf("expected temperature 0.7, got %v", got.Temperature)
	}
	if got.MaxTokens == nil || *got.MaxTokens != 512 {
		t.Errorf("expected max_tokens 512, got %v", got.MaxTokens)
	}
	if got.TopP == nil || *got.TopP != 0.9 {
		t.Errorf("expected top_p 0.9, got %v", got.TopP)
	}
	if got.FrequencyPenalty == nil || *got.FrequencyPenalty != -0.2 {
		t.Errorf("expected frequency_penalty -0.2, got %v", got.FrequencyPenalty)
	}
	if got.ResponseFormat != "json_object" {
		t.Errorf("expected response_format json_object, got %s", got.ResponseFormat)
	}
	if rf := got.ResponseFormatMap(); rf["type"] != "json_object" {
		t.Errorf("expected ResponseFormatMap json_object, got %v", rf)
	}
}

func TestConfigValidationRejectsOutOfRangeParams(t *testing.T) {
	uc := application.NewConfigUseCase(NewTestStore(t))
	ctx := context.Background()

	cases := []struct {
		name    string
		ptr     func() *domain.ChatConfig
		wantErr string
	}{
		{"temperature>2", func() *domain.ChatConfig {
			v := 3.0
			return &domain.ChatConfig{Name: "x", Provider: "ollama", Model: "m", Temperature: &v}
		}, "temperature"},
		{"top_p>1", func() *domain.ChatConfig {
			v := 1.5
			return &domain.ChatConfig{Name: "x", Provider: "ollama", Model: "m", TopP: &v}
		}, "top_p"},
		{"max_tokens<1", func() *domain.ChatConfig {
			v := 0
			return &domain.ChatConfig{Name: "x", Provider: "ollama", Model: "m", MaxTokens: &v}
		}, "max_tokens"},
		{"bad_response_format", func() *domain.ChatConfig {
			return &domain.ChatConfig{Name: "x", Provider: "ollama", Model: "m", ResponseFormat: "xml"}
		}, "response_format"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := uc.Create(ctx, tc.ptr())
			if err == nil {
				t.Fatal("expected validation error, got nil")
			}
			var verr *domain.ValidationError
			if !errors.As(err, &verr) {
				t.Fatalf("expected *ValidationError, got %T", err)
			}
		})
	}
}

func TestChatSendGivesConfigParamsToLLM(t *testing.T) {
	store := NewTestStore(t)
	temp := 0.3
	maxTokens := 128

	cfg, _ := application.NewConfigUseCase(store).Create(context.Background(), &domain.ChatConfig{
		Name:        "Froid",
		Provider:    "openai",
		Model:       "gpt-4o",
		Temperature: &temp,
		MaxTokens:   &maxTokens,
	})

	fake := &fakeLLM{reply: "ok"}
	_, err := application.NewChatUseCase(store, domain.NewEnrichmentService(), fake).Send(
		context.Background(), cfg.ID, nil, "bonjour", nil,
	)
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if fake.lastCfg.Temperature == nil || *fake.lastCfg.Temperature != 0.3 {
		t.Errorf("expected temperature 0.3 passed to LLM, got %v", fake.lastCfg.Temperature)
	}
	if fake.lastCfg.MaxTokens == nil || *fake.lastCfg.MaxTokens != 128 {
		t.Errorf("expected max_tokens 128 passed to LLM, got %v", fake.lastCfg.MaxTokens)
	}
}

func TestChatSendUsesSystemPromptAndFullParamsFromConfig(t *testing.T) {
	store := NewTestStore(t)
	temp := 0.4
	topP := 0.85
	maxTokens := 77
	freq := -0.2
	pres := 0.5

	cfg, _ := application.NewConfigUseCase(store).Create(context.Background(), &domain.ChatConfig{
		Name:             "Drum",
		Provider:         "ollama",
		Model:            "gemma4:e4b",
		SystemPrompt:     "Tu t'appelles Drum et tu cites le chiffre 42.",
		Temperature:      &temp,
		TopP:             &topP,
		MaxTokens:        &maxTokens,
		FrequencyPenalty: &freq,
		PresencePenalty:  &pres,
		ResponseFormat:   "json_object",
	})

	fake := &fakeLLM{reply: `{"ok":true}`}
	_, err := application.NewChatUseCase(store, domain.NewEnrichmentService(), fake).Send(
		context.Background(), cfg.ID, nil, "qui es-tu ?", nil,
	)
	if err != nil {
		t.Fatalf("send: %v", err)
	}

	if !fake.called {
		t.Fatal("expected LLM provider called")
	}
	if len(fake.lastMessages) == 0 || fake.lastMessages[0].Role != "system" {
		t.Fatalf("expected leading system message, got %#v", fake.lastMessages)
	}
	if fake.lastMessages[0].Content != "Tu t'appelles Drum et tu cites le chiffre 42." {
		t.Errorf("system prompt mismatch: %q", fake.lastMessages[0].Content)
	}

	if fake.lastCfg.Temperature == nil || *fake.lastCfg.Temperature != 0.4 {
		t.Errorf("expected temperature 0.4 passed to LLM, got %v", fake.lastCfg.Temperature)
	}
	if fake.lastCfg.TopP == nil || *fake.lastCfg.TopP != 0.85 {
		t.Errorf("expected top_p 0.85 passed to LLM, got %v", fake.lastCfg.TopP)
	}
	if fake.lastCfg.MaxTokens == nil || *fake.lastCfg.MaxTokens != 77 {
		t.Errorf("expected max_tokens 77 passed to LLM, got %v", fake.lastCfg.MaxTokens)
	}
	if fake.lastCfg.FrequencyPenalty == nil || *fake.lastCfg.FrequencyPenalty != -0.2 {
		t.Errorf("expected frequency_penalty -0.2 passed to LLM, got %v", fake.lastCfg.FrequencyPenalty)
	}
	if fake.lastCfg.PresencePenalty == nil || *fake.lastCfg.PresencePenalty != 0.5 {
		t.Errorf("expected presence_penalty 0.5 passed to LLM, got %v", fake.lastCfg.PresencePenalty)
	}
	if fake.lastCfg.ResponseFormat != "json_object" {
		t.Errorf("expected response_format json_object passed to LLM, got %s", fake.lastCfg.ResponseFormat)
	}
}

func TestChatSendUnknownConfigReturnsNotFound(t *testing.T) {
	store := NewTestStore(t)
	application.NewConfigUseCase(store).Create(context.Background(), &domain.ChatConfig{
		Name:     "Local",
		Provider: "ollama",
		Model:    "gemma4:e4b",
	})

	_, err := application.NewChatUseCase(
		store, domain.NewEnrichmentService(), &fakeLLM{reply: "x"},
	).Send(context.Background(), "cfg-inconnu", nil, "hi", nil)
	if !errors.Is(err, domain.ErrConfigNotFound) {
		t.Fatalf("expected ErrConfigNotFound, got %v", err)
	}
}

func TestChatSendRejectsEmptyMessage(t *testing.T) {
	store := NewTestStore(t)
	cfg, _ := application.NewConfigUseCase(store).Create(context.Background(), &domain.ChatConfig{
		Name:     "Local",
		Provider: "ollama",
		Model:    "gemma4:e4b",
	})

	_, err := application.NewChatUseCase(
		store, domain.NewEnrichmentService(), &fakeLLM{},
	).Send(context.Background(), cfg.ID, nil, "   ", nil)
	var verr *domain.ValidationError
	if !errors.As(err, &verr) {
		t.Fatalf("expected *ValidationError, got %T", err)
	}
	if !contains(verr.Fields, "message") {
		t.Errorf("expected message field, got %v", verr.Fields)
	}
}

func TestChatSendTrimsOversizedHistory(t *testing.T) {
	store := NewTestStore(t)
	cfg, _ := application.NewConfigUseCase(store).Create(context.Background(), &domain.ChatConfig{
		Name:     "Local",
		Provider: "ollama",
		Model:    "gemma4:e4b",
	})

	fake := &fakeLLM{reply: "ok"}
	uc := application.NewChatUseCase(store, domain.NewEnrichmentService(), fake)

	history := make([]domain.ChatMessage, 100)
	for i := range history {
		history[i] = domain.ChatMessage{Role: "user", Content: fmt.Sprintf("m%d", i)}
	}
	result, err := uc.Send(context.Background(), cfg.ID, history, "fin", nil)
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if len(result.Messages) > 42 {
		t.Errorf("expected history truncated, got %d messages", len(result.Messages))
	}
}

func TestModelUseCaseMergesGatewayAndConfigModels(t *testing.T) {
	store := NewTestStore(t)
	catalog, _ := infrastructure.NewModelCatalogStore(newTestDB(t), nil)
	uc := application.NewModelUseCase([]domain.ModelInfo{
		{Name: "gemma4:e4b", Provider: "ollama", Source: "config.yaml"},
	}, store, catalog)

	configUC := application.NewConfigUseCase(store)
	_, _ = configUC.Create(context.Background(), &domain.ChatConfig{
		Name: "Local", Provider: "ollama", Model: "gemma4:e4b",
	})
	_, _ = configUC.Create(context.Background(), &domain.ChatConfig{
		Name: "API", Provider: "openai", Model: "gpt-4o",
	})

	models, err := uc.List(context.Background())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(models) != 2 {
		t.Fatalf("expected 2 unique models, got %d", len(models))
	}
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

var _ = infrastructure.IsNotFound

// SetActive reecrit toute la table, ce qui regenere les rowid. La liste des
// configurations d'un projet doit rester dans le meme ordre avant et apres une
// activation, faute de quoi l'interface fait sauter les lignes et l'utilisateur
// ne retrouve plus celle qu'il vient d'activer.
func TestListByProjectOrderSurvivesActivation(t *testing.T) {
	ctx := context.Background()
	store := NewTestStore(t)
	uc := application.NewConfigUseCase(store)

	projectID := NewTestProject(t)

	var ids []string
	// Quatre configurations : c'est a ce volume que l'ordre du plan JSON diverge
	// de l'ordre des rowid et que le desordre devient observable.
	for _, name := range []string{"Un", "Deux", "Trois", "Quatre"} {
		cfg, err := uc.Create(ctx, &domain.ChatConfig{
			ProjectID: projectID,
			Name:      name,
			Provider:  "ollama",
			Model:     "gemma4:e4b",
		})
		if err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
		ids = append(ids, cfg.ID)
	}

	before, err := store.ListByProject(ctx, projectID)
	if err != nil {
		t.Fatalf("list before: %v", err)
	}
	if len(before) != 4 {
		t.Fatalf("expected 4 configs, got %d", len(before))
	}

	// Activer la premiere : c'est l'ecriture qui reecrit la table, et c'est le
	// cas ou la ligne active atterrit en dernier sans tri.
	if _, err := uc.SetActive(ctx, projectID, ids[0]); err != nil {
		t.Fatalf("set active: %v", err)
	}

	after, err := store.ListByProject(ctx, projectID)
	if err != nil {
		t.Fatalf("list after: %v", err)
	}
	if len(after) != 4 {
		t.Fatalf("expected 4 configs, got %d", len(after))
	}

	for i := range before {
		if before[i].ID != after[i].ID {
			t.Fatalf("order changed at index %d: %q became %q (full before=%v after=%v)",
				i, before[i].ID, after[i].ID, configIDs(before), configIDs(after))
		}
	}

	// La ligne activee reste a sa place : c'est ce qui permet a l'utilisateur
	// de retrouver ce qu'il vient de cliquer.
	if !after[0].Active {
		t.Errorf("the activated config must stay first and be active, got %+v", after[0])
	}
}

func configIDs(items []domain.ChatConfig) []string {
	out := make([]string, 0, len(items))
	for _, c := range items {
		out = append(out, c.Name)
	}
	return out
}
