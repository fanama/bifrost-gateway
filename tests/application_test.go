package tests

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"bridge-gateway/application"
	"bridge-gateway/domain"
	"bridge-gateway/infrastructure"
)

func NewTestStore(t *testing.T) *infrastructure.FileConfigStore {
	t.Helper()
	store, err := infrastructure.NewFileConfigStore(t.TempDir() + "/configs.json")
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	return store
}

func NewTestProject(t *testing.T) string {
	t.Helper()
	store, err := infrastructure.NewFileProjectStore(t.TempDir() + "/projects.json")
	if err != nil {
		t.Fatalf("new project store: %v", err)
	}
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
	for _, want := range []string{"provider", "model", "cost_center"} {
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
		Name:       "Ollama local",
		ProjectID:  pid,
		Provider:   "ollama",
		Model:      "gemma4:e4b",
		BaseURL:    "http://localhost:11434",
		CostCenter: "CC-ING",
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
		Name:       "Ollama R2",
		Provider:   "Ollama",
		Model:      "llama3",
		CostCenter: "CC-ING",
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

	a, _ := uc.Create(ctx, &domain.ChatConfig{Name: "A", ProjectID: pid, Provider: "ollama", Model: "m1", CostCenter: "CC"})
	b, _ := uc.Create(ctx, &domain.ChatConfig{Name: "B", ProjectID: pid, Provider: "openai", Model: "m2", CostCenter: "CC"})

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

func (f *fakeLLM) Chat(_ context.Context, cfg *domain.ChatConfig, messages []domain.ChatMessage) (string, error) {
	f.called = true
	f.lastCfg = cfg
	f.lastMessages = messages
	return f.reply, nil
}

func TestChatSendCallsLLMWithEnrichedMessages(t *testing.T) {
	store := NewTestStore(t)
	cfg, _ := application.NewConfigUseCase(store).Create(context.Background(), &domain.ChatConfig{
		Name:         "Local",
		Provider:     "ollama",
		Model:        "gemma4:e4b",
		CostCenter:   "CC-ING",
		SystemPrompt: "You are helpful",
	})

	fake := &fakeLLM{reply: "ma réponse"}
	uc := application.NewChatUseCase(store, domain.NewEnrichmentService(), fake)
	ctx := context.Background()

	history := []domain.ChatMessage{{Role: "user", Content: "previous"}}
	messages, err := uc.Send(ctx, cfg.ID, history, "bonjour")
	if err != nil {
		t.Fatalf("send: %v", err)
	}

	if !fake.called {
		t.Fatal("expected LLM provider called")
	}
	if fake.lastCfg.ID != cfg.ID {
		t.Errorf("expected config %s, got %s", cfg.ID, fake.lastCfg.ID)
	}

	if len(messages) != 3 {
		t.Fatalf("expected 3 messages (user, user, assistant), got %d", len(messages))
	}
	last := messages[len(messages)-1]
	if last.Role != "assistant" || last.Content != "ma réponse" {
		t.Errorf("expected assistant reply, got %s:%s", last.Role, last.Content)
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
		CostCenter:       "CC-ING",
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
			return &domain.ChatConfig{Name: "x", Provider: "ollama", Model: "m", CostCenter: "CC", Temperature: &v}
		}, "temperature"},
		{"top_p>1", func() *domain.ChatConfig {
			v := 1.5
			return &domain.ChatConfig{Name: "x", Provider: "ollama", Model: "m", CostCenter: "CC", TopP: &v}
		}, "top_p"},
		{"max_tokens<1", func() *domain.ChatConfig {
			v := 0
			return &domain.ChatConfig{Name: "x", Provider: "ollama", Model: "m", CostCenter: "CC", MaxTokens: &v}
		}, "max_tokens"},
		{"bad_response_format", func() *domain.ChatConfig {
			return &domain.ChatConfig{Name: "x", Provider: "ollama", Model: "m", CostCenter: "CC", ResponseFormat: "xml"}
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
		CostCenter:  "CC-ING",
		Temperature: &temp,
		MaxTokens:   &maxTokens,
	})

	fake := &fakeLLM{reply: "ok"}
	_, err := application.NewChatUseCase(store, domain.NewEnrichmentService(), fake).Send(
		context.Background(), cfg.ID, nil, "bonjour",
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
		CostCenter:       "CC-ING",
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
		context.Background(), cfg.ID, nil, "qui es-tu ?",
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
		Name:       "Local",
		Provider:   "ollama",
		Model:      "gemma4:e4b",
		CostCenter: "CC-ING",
	})

	_, err := application.NewChatUseCase(
		store, domain.NewEnrichmentService(), &fakeLLM{reply: "x"},
	).Send(context.Background(), "cfg-inconnu", nil, "hi")
	if !errors.Is(err, domain.ErrConfigNotFound) {
		t.Fatalf("expected ErrConfigNotFound, got %v", err)
	}
}

func TestChatSendRejectsEmptyMessage(t *testing.T) {
	store := NewTestStore(t)
	cfg, _ := application.NewConfigUseCase(store).Create(context.Background(), &domain.ChatConfig{
		Name:       "Local",
		Provider:   "ollama",
		Model:      "gemma4:e4b",
		CostCenter: "CC-ING",
	})

	_, err := application.NewChatUseCase(
		store, domain.NewEnrichmentService(), &fakeLLM{},
	).Send(context.Background(), cfg.ID, nil, "   ")
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
		Name:       "Local",
		Provider:   "ollama",
		Model:      "gemma4:e4b",
		CostCenter: "CC-ING",
	})

	fake := &fakeLLM{reply: "ok"}
	uc := application.NewChatUseCase(store, domain.NewEnrichmentService(), fake)

	history := make([]domain.ChatMessage, 100)
	for i := range history {
		history[i] = domain.ChatMessage{Role: "user", Content: fmt.Sprintf("m%d", i)}
	}
	messages, err := uc.Send(context.Background(), cfg.ID, history, "fin")
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if len(messages) > 42 {
		t.Errorf("expected history truncated, got %d messages", len(messages))
	}
}

func TestModelUseCaseMergesGatewayAndConfigModels(t *testing.T) {
	store := NewTestStore(t)
	catalog, _ := infrastructure.NewFileModelCatalogStore(t.TempDir()+"/models.json", nil)
	uc := application.NewModelUseCase([]domain.ModelInfo{
		{Name: "gemma4:e4b", Provider: "ollama", Source: "config.yaml"},
	}, store, catalog)

	configUC := application.NewConfigUseCase(store)
	_, _ = configUC.Create(context.Background(), &domain.ChatConfig{
		Name: "Local", Provider: "ollama", Model: "gemma4:e4b", CostCenter: "CC",
	})
	_, _ = configUC.Create(context.Background(), &domain.ChatConfig{
		Name: "API", Provider: "openai", Model: "gpt-4o", CostCenter: "CC",
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
