package infrastructure

import (
	"context"
	"testing"

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
	if defaultBaseURLFor(schemas.Mistral) != "https://api.mistral.ai/v1" {
		t.Errorf("unexpected mistral baseURL: %s", defaultBaseURLFor(schemas.Mistral))
	}
	if defaultBaseURLFor(schemas.OpenAI) != "https://api.openai.com/v1" {
		t.Errorf("unexpected openai baseURL: %s", defaultBaseURLFor(schemas.OpenAI))
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
