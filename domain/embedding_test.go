package domain

import "testing"

// EmbeddingChoiceFromModel n'accepte que les modeles declares de type
// embedding, normalise le provider en minuscules et refuse les entrees
// incompletes : c'est la porte d'entree du select du testeur et du routage
// API depuis la page Modeles.
func TestEmbeddingChoiceFromModel(t *testing.T) {
	choice, ok := EmbeddingChoiceFromModel(ModelInfo{
		Name:     " bge-small-fr ",
		Provider: "OLLAMA",
		Kind:     ModelKindEmbedding,
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

	for name, m := range map[string]ModelInfo{
		"chat kind":             {Name: "mistral-large", Provider: "mistral", Kind: ModelKindChat},
		"empty kind":            {Name: "legacy-model", Provider: "ollama"},
		"embedding no provider": {Name: "orphan", Kind: ModelKindEmbedding},
		"embedding no name":     {Provider: "ollama", Kind: ModelKindEmbedding},
	} {
		if _, ok := EmbeddingChoiceFromModel(m); ok {
			t.Errorf("%s: must not produce a choice", name)
		}
	}
}
