package tests

import (
	"testing"

	"bridge-gateway/domain"
)

// La surcharge reasoning_effort doit gagner sur la configuration sans la
// modifier, et une valeur absente doit laisser la configuration trancher.
func TestReasoningEffortOverridesConfig(t *testing.T) {
	stored := &domain.ChatConfig{Model: "gpt-5", ReasoningEffort: "medium"}

	off := "none"
	effective, err := (&domain.RequestOptions{ReasoningEffort: &off}).Apply(stored)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if effective.ReasoningEffort != "none" {
		t.Errorf("reasoning_effort: got %q", effective.ReasoningEffort)
	}
	if stored.ReasoningEffort != "medium" {
		t.Errorf("the stored config must not be mutated, got %q", stored.ReasoningEffort)
	}

	effective, err = (*domain.RequestOptions)(nil).Apply(stored)
	if err != nil {
		t.Fatalf("apply nil: %v", err)
	}
	if effective.ReasoningEffort != "medium" {
		t.Errorf("nil options must keep the config, got %q", effective.ReasoningEffort)
	}
}

func TestReasoningEffortRejectedForUnsupportedProvider(t *testing.T) {
	cfg := &domain.ChatConfig{Name: "x", Provider: "mistral", Model: "ministral-14b-latest", ReasoningEffort: "minimal"}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected config validation to reject reasoning for mistral")
	}
	if cfg.EffectiveReasoningEffort() != "" {
		t.Fatalf("mistral must fall back to its default, got %q", cfg.EffectiveReasoningEffort())
	}

	effort := "minimal"
	if _, err := (&domain.RequestOptions{ReasoningEffort: &effort}).Apply(&domain.ChatConfig{Provider: "mistral", Model: "m"}); err == nil {
		t.Fatal("expected the request override to be rejected for mistral")
	}

	// Desactiver le thinking reste un no-op accepte, meme sans support.
	none := "none"
	if _, err := (&domain.RequestOptions{ReasoningEffort: &none}).Apply(&domain.ChatConfig{Provider: "mistral", Model: "m"}); err != nil {
		t.Fatalf("disabling reasoning must stay accepted: %v", err)
	}

	// A legacy stored record must stay usable: the runtime drops the level
	// instead of sending a field the provider rejects.
	stored := &domain.ChatConfig{Provider: "mistral", Model: "m", ReasoningEffort: "minimal"}
	effective, err := (*domain.RequestOptions)(nil).Apply(stored)
	if err != nil {
		t.Fatalf("apply nil: %v", err)
	}
	if effective.EffectiveReasoningEffort() != "" {
		t.Fatalf("legacy reasoning must be dropped at runtime, got %q", effective.EffectiveReasoningEffort())
	}
}

func TestReasoningEffortRejectsUnknownLevel(t *testing.T) {
	bad := "turbo"
	if _, err := (&domain.RequestOptions{ReasoningEffort: &bad}).Apply(&domain.ChatConfig{}); err == nil {
		t.Fatal("expected an error for an unknown reasoning level")
	}

	cfg := &domain.ChatConfig{Name: "x", Provider: "openai", Model: "gpt-5", ReasoningEffort: "turbo"}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected config validation to reject an unknown reasoning level")
	}
}
