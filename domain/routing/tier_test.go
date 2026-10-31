package routing

import (
	"testing"

	"bridge-gateway/domain/config"
)

func TestMergeTierConfigKeepsActiveGenerationSettings(t *testing.T) {
	temp := 0.3
	maxTokens := 256
	active := &config.ChatConfig{
		Provider:         "ollama",
		Model:            "m-balanced",
		SystemPrompt:     "tu es un assistant strict",
		Temperature:      &temp,
		MaxTokens:        &maxTokens,
		ResponseFormat:   "json_object",
		ResponseSchema:   `{"type":"object"}`,
		FrequencyPenalty: &temp,
		PresencePenalty:  &temp,
	}
	topP := 0.9
	active.TopP = &topP
	target := &config.ChatConfig{
		Provider:     "mistral",
		Model:        "m-frontier",
		SystemPrompt: "autre prompt",
		APIKey:       "target-key",
	}

	merged := MergeTierConfig(active, target)
	if merged == nil {
		t.Fatal("MergeTierConfig returned nil")
	}
	if merged.Provider != "mistral" || merged.Model != "m-frontier" {
		t.Errorf("target identity lost: %s/%s", merged.Provider, merged.Model)
	}
	if merged.APIKey != "target-key" {
		t.Errorf("target credentials must be kept, got %q", merged.APIKey)
	}
	if merged.SystemPrompt != active.SystemPrompt {
		t.Errorf("system prompt = %q, want active", merged.SystemPrompt)
	}
	if merged.Temperature == nil || *merged.Temperature != 0.3 {
		t.Error("temperature must come from the active configuration")
	}
	if merged.TopP == nil || *merged.TopP != 0.9 {
		t.Error("top_p must come from the active configuration")
	}
	if merged.MaxTokens == nil || *merged.MaxTokens != 256 {
		t.Error("max_tokens must come from the active configuration")
	}
	if merged.ResponseFormat != "json_object" || merged.ResponseSchema != `{"type":"object"}` {
		t.Error("response format and schema must come from the active configuration")
	}
}

func TestMergeTierConfigSameTargetReturnsActive(t *testing.T) {
	active := &config.ChatConfig{Provider: "ollama", Model: "m"}
	target := &config.ChatConfig{Provider: "ollama", Model: "m", SystemPrompt: "target"}
	if got := MergeTierConfig(active, target); got != active {
		t.Fatal("the same provider/model must reuse the active configuration")
	}
}

func TestMergeTierConfigNilInputs(t *testing.T) {
	if MergeTierConfig(&config.ChatConfig{}, nil) != nil {
		t.Error("a nil target has no effective configuration")
	}
	target := &config.ChatConfig{Provider: "ollama", Model: "m"}
	merged := MergeTierConfig(nil, target)
	if merged == nil || merged.Model != "m" {
		t.Fatal("without an active configuration the target is copied")
	}
	if merged == target {
		t.Error("the target must be copied, not aliased")
	}
}
