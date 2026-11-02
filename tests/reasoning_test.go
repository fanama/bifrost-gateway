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
