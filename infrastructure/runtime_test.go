package infrastructure

import (
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
