package infrastructure

import (
	"context"
	"errors"
	"strings"
	"testing"

	"bridge-gateway/domain"
)

// chainCall enregistre le provider et les identifiants effectivement envoyes,
// pour prouver qu'un repli heberge ailleurs repart sans la cle du principal.
type chainCall struct {
	Provider string
	Model    string
	APIKey   string
	BaseURL  string
}

// chainLLM repond avec le provider/modele appele et permet de simuler une
// entree en panne, en synchrone comme en streaming.
type chainLLM struct {
	failing     map[string]error
	emptyStream map[string]bool
	calls       []chainCall
}

func (c *chainLLM) Chat(_ context.Context, cfg *domain.ChatConfig, _ []domain.ChatMessage) (domain.LLMResult, error) {
	c.calls = append(c.calls, callOf(cfg))
	if err, ok := c.failing[cfg.Provider+"/"+cfg.Model]; ok {
		return domain.LLMResult{}, err
	}
	return domain.LLMResult{Content: "reponse de " + cfg.Provider + "/" + cfg.Model, Model: cfg.Model}, nil
}

func (c *chainLLM) ChatStream(_ context.Context, cfg *domain.ChatConfig, _ []domain.ChatMessage) (<-chan domain.StreamEvent, error) {
	c.calls = append(c.calls, callOf(cfg))
	if err, ok := c.failing[cfg.Provider+"/"+cfg.Model]; ok {
		return nil, err
	}
	out := make(chan domain.StreamEvent, 2)
	if c.emptyStream[cfg.Provider+"/"+cfg.Model] {
		close(out)
		return out, nil
	}
	out <- domain.StreamEvent{Delta: "reponse de " + cfg.Provider + "/" + cfg.Model, Model: cfg.Model}
	out <- domain.StreamEvent{Done: true, Model: cfg.Model}
	close(out)
	return out, nil
}

func callOf(cfg *domain.ChatConfig) chainCall {
	return chainCall{Provider: cfg.Provider, Model: cfg.Model, APIKey: cfg.APIKey, BaseURL: cfg.BaseURL}
}

func chainConfig() *domain.ChatConfig {
	return &domain.ChatConfig{
		ID:       "cfg-1",
		Provider: "mistral",
		Model:    "m-principal",
		APIKey:   "mistral-key",
		BaseURL:  "https://api.mistral.ai",
		FallbackModels: []domain.ModelChoice{
			{Model: "m-repli"},
			{Provider: "openai", Model: "m-ailleurs"},
		},
	}
}

func TestModelFallbackServesPrimaryWhenAvailable(t *testing.T) {
	inner := &chainLLM{}
	provider := NewModelFallbackLLMProvider(inner)

	result, err := provider.Chat(context.Background(), chainConfig(), nil)
	if err != nil {
		t.Fatalf("chat: %v", err)
	}
	if result.Model != "m-principal" {
		t.Errorf("served model = %q, want m-principal", result.Model)
	}
	if len(inner.calls) != 1 {
		t.Errorf("expected a single attempt, got %#v", inner.calls)
	}
}

func TestModelFallbackUsesFirstAvailableModel(t *testing.T) {
	inner := &chainLLM{failing: map[string]error{"mistral/m-principal": errors.New("rate limited")}}
	provider := NewModelFallbackLLMProvider(inner)

	result, err := provider.Chat(context.Background(), chainConfig(), nil)
	if err != nil {
		t.Fatalf("chat: %v", err)
	}
	if result.Model != "m-repli" {
		t.Errorf("served model = %q, want m-repli", result.Model)
	}
	want := "mistral/m-principal,mistral/m-repli"
	if joinCalls(inner.calls) != want {
		t.Errorf("attempt order = %q, want %q", joinCalls(inner.calls), want)
	}
}

func TestModelFallbackReachesFallbackOnAnotherProvider(t *testing.T) {
	inner := &chainLLM{failing: map[string]error{
		"mistral/m-principal": errors.New("rate limited"),
		"mistral/m-repli":     errors.New("model retired"),
	}}
	provider := NewModelFallbackLLMProvider(inner)

	result, err := provider.Chat(context.Background(), chainConfig(), nil)
	if err != nil {
		t.Fatalf("chat: %v", err)
	}
	if result.Model != "m-ailleurs" {
		t.Errorf("served model = %q, want m-ailleurs", result.Model)
	}
	last := inner.calls[len(inner.calls)-1]
	if last.Provider != "openai" {
		t.Errorf("fallback provider = %q, want openai", last.Provider)
	}
	if last.APIKey != "" || last.BaseURL != "" {
		t.Errorf("cross-provider fallback kept primary credentials: %#v", last)
	}
}

func TestModelFallbackReportsEveryFailure(t *testing.T) {
	inner := &chainLLM{failing: map[string]error{
		"mistral/m-principal": errors.New("rate limited"),
		"mistral/m-repli":     errors.New("model retired"),
		"openai/m-ailleurs":   errors.New("provider down"),
	}}
	provider := NewModelFallbackLLMProvider(inner)

	_, err := provider.Chat(context.Background(), chainConfig(), nil)
	if err == nil {
		t.Fatal("expected an error when every model fails")
	}
	if !strings.Contains(err.Error(), "mistral/m-principal, mistral/m-repli, openai/m-ailleurs") {
		t.Errorf("error should name the full chain, got %v", err)
	}
	if len(inner.calls) != 3 {
		t.Errorf("expected three attempts, got %#v", inner.calls)
	}
}

func TestModelFallbackStreamsFirstAvailableModel(t *testing.T) {
	inner := &chainLLM{emptyStream: map[string]bool{"mistral/m-principal": true}}
	provider := NewModelFallbackLLMProvider(inner)

	stream, err := provider.ChatStream(context.Background(), chainConfig(), nil)
	if err != nil {
		t.Fatalf("stream: %v", err)
	}

	var text, served string
	for event := range stream {
		text += event.Delta
		if event.Model != "" {
			served = event.Model
		}
	}
	if text != "reponse de mistral/m-repli" {
		t.Errorf("streamed text = %q", text)
	}
	if served != "m-repli" {
		t.Errorf("served model = %q, want m-repli", served)
	}
	if joinCalls(inner.calls) != "mistral/m-principal,mistral/m-repli" {
		t.Errorf("attempt order = %q", joinCalls(inner.calls))
	}
}

func TestModelFallbackKeepsSingleModelUnchanged(t *testing.T) {
	inner := &chainLLM{}
	provider := NewModelFallbackLLMProvider(inner)
	cfg := &domain.ChatConfig{Provider: "mistral", Model: "seul", APIKey: "k"}

	if _, err := provider.Chat(context.Background(), cfg, nil); err != nil {
		t.Fatalf("chat: %v", err)
	}
	if len(inner.calls) != 1 || inner.calls[0].Model != "seul" || inner.calls[0].APIKey != "k" {
		t.Errorf("unexpected attempts %#v", inner.calls)
	}
}

func joinCalls(calls []chainCall) string {
	labels := make([]string, 0, len(calls))
	for _, call := range calls {
		labels = append(labels, call.Provider+"/"+call.Model)
	}
	return strings.Join(labels, ",")
}
