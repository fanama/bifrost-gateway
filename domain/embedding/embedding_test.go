package embedding

import (
	"testing"

	"bridge-gateway/domain/provider"
)

// EmbeddingChoiceFromModel accepts only models declared as embedding models,
// normalizes the provider to lowercase, and rejects incomplete entries.
func TestEmbeddingChoiceFromModel(t *testing.T) {
	choice, ok := EmbeddingChoiceFromModel(provider.ModelInfo{
		Name:     " bge-small-fr ",
		Provider: "OLLAMA",
		Kind:     provider.ModelKindEmbedding,
	})
	if !ok {
		t.Fatal("expected embedding model to produce a choice")
	}
	if choice.Model != "bge-small-fr" || choice.Provider != "ollama" {
		t.Errorf("expected normalized bge-small-fr/ollama, got %q/%q", choice.Model, choice.Provider)
	}
	if choice.Label == "" || choice.Hint == "" {
		t.Errorf("label and hint must be populated for display, got %+v", choice)
	}

	for name, m := range map[string]provider.ModelInfo{
		"chat kind":             {Name: "mistral-large", Provider: "mistral", Kind: provider.ModelKindChat},
		"empty kind":            {Name: "legacy-model", Provider: "ollama"},
		"embedding no provider": {Name: "orphan", Kind: provider.ModelKindEmbedding},
		"embedding no name":     {Provider: "ollama", Kind: provider.ModelKindEmbedding},
	} {
		if _, ok := EmbeddingChoiceFromModel(m); ok {
			t.Errorf("%s: must not produce a choice", name)
		}
	}
}
