package config

import (
	"reflect"
	"testing"
)

func TestCandidatesOrdersPrimaryThenFallbacks(t *testing.T) {
	cfg := &ChatConfig{
		Provider: "mistral",
		Model:    "principal",
		FallbackModels: []ModelChoice{
			{Model: "repli-a"},
			{Provider: "openai", Model: "repli-b"},
		},
	}
	want := []ModelChoice{
		{Provider: "mistral", Model: "principal"},
		{Provider: "mistral", Model: "repli-a"},
		{Provider: "openai", Model: "repli-b"},
	}
	if got := cfg.Candidates(); !reflect.DeepEqual(got, want) {
		t.Errorf("Candidates() = %#v, want %#v", got, want)
	}
}

func TestCandidatesSkipsEmptyAndDuplicateModels(t *testing.T) {
	cfg := &ChatConfig{
		Provider: "mistral",
		Model:    " Principal ",
		FallbackModels: []ModelChoice{
			{},
			{Model: "principal"},
			{Provider: "MISTRAL", Model: "Principal"},
			{Model: "repli"},
			{Provider: "openai", Model: "repli"},
		},
	}
	want := []ModelChoice{
		{Provider: "mistral", Model: "Principal"},
		{Provider: "mistral", Model: "repli"},
		{Provider: "openai", Model: "repli"},
	}
	if got := cfg.Candidates(); !reflect.DeepEqual(got, want) {
		t.Errorf("Candidates() = %#v, want %#v", got, want)
	}
}

func TestCandidatesOnNilConfig(t *testing.T) {
	var cfg *ChatConfig
	if got := cfg.Candidates(); got != nil {
		t.Errorf("Candidates() = %#v, want nil", got)
	}
}

func TestWithModelSwitchesProviderAndDropsPrimaryCredentials(t *testing.T) {
	cfg := &ChatConfig{
		Provider: "mistral",
		Model:    "a",
		APIKey:   "mistral-key",
		BaseURL:  "https://api.mistral.ai",
	}

	sameProvider := cfg.WithModel(ModelChoice{Provider: "mistral", Model: "b"})
	if sameProvider.Model != "b" || sameProvider.APIKey != "mistral-key" || sameProvider.BaseURL != "https://api.mistral.ai" {
		t.Errorf("same-provider attempt must keep credentials, got %#v", sameProvider)
	}

	otherProvider := cfg.WithModel(ModelChoice{Provider: "openai", Model: "gpt-4o-mini"})
	if otherProvider.Provider != "openai" || otherProvider.Model != "gpt-4o-mini" {
		t.Errorf("unexpected cross-provider target %#v", otherProvider)
	}
	if otherProvider.APIKey != "" || otherProvider.BaseURL != "" {
		t.Errorf("cross-provider attempt must resolve its own credentials, got %#v", otherProvider)
	}

	if cfg.Provider != "mistral" || cfg.Model != "a" {
		t.Errorf("WithModel mutated the source configuration: %#v", cfg)
	}
}

func TestModelChoiceLabel(t *testing.T) {
	if got := (ModelChoice{Provider: "openai", Model: "gpt-4o-mini"}).Label(); got != "openai/gpt-4o-mini" {
		t.Errorf("Label() = %q", got)
	}
	if got := (ModelChoice{Model: "seul"}).Label(); got != "seul" {
		t.Errorf("Label() = %q", got)
	}
}
