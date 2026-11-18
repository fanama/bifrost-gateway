package rag

import "strings"

// Chunking methods selectable per project. Each one is a different way of
// turning an extracted document into the passages that get embedded.
const (
	// ChunkingFixed is the historical sliding window over raw runes.
	ChunkingFixed = "fixed"
	// ChunkingSentence groups whole sentences up to the target size.
	ChunkingSentence = "sentence"
	// ChunkingMarkdown splits on Markdown headings, then on sentences.
	ChunkingMarkdown = "markdown"
	// ChunkingSemantic groups adjacent sentences whose embeddings are close.
	ChunkingSemantic = "semantic"
	// DefaultChunkingMethod preserves the behaviour of existing projects.
	DefaultChunkingMethod = ChunkingFixed
)

// ChunkingChoice is one selectable method with the label and hint shown by the
// settings page.
type ChunkingChoice struct {
	Value string
	Label string
	Hint  string
}

// ChunkingChoices is the single source of truth for the methods the UI offers
// and the domain accepts. Hierarchical chunking is absent on purpose: it needs
// several indexes per document, which the single-index retrieval does not
// model yet.
var ChunkingChoices = []ChunkingChoice{
	{
		Value: ChunkingFixed,
		Label: "Fixe (fenêtre glissante)",
		Hint:  "Blocs de N caractères avec chevauchement, sans tenir compte des phrases.",
	},
	{
		Value: ChunkingSentence,
		Label: "Ponctuation (phrases et paragraphes)",
		Hint:  "Coupe aux fins de phrases et paragraphes, puis regroupe jusqu'à la taille cible.",
	},
	{
		Value: ChunkingMarkdown,
		Label: "Balises (titres Markdown)",
		Hint:  "Une section par titre ; chaque passage garde son titre en tête.",
	},
	{
		Value: ChunkingSemantic,
		Label: "Sémantique (embeddings de phrases)",
		Hint:  "Regroupe les phrases voisines dont les vecteurs sont proches ; un embedding supplémentaire à l'import.",
	},
}

// NormalizeChunkingMethod trims and lowercases a method, mapping an empty value
// to the default.
func NormalizeChunkingMethod(method string) string {
	method = strings.ToLower(strings.TrimSpace(method))
	if method == "" {
		return DefaultChunkingMethod
	}
	return method
}

// ValidChunkingMethod reports whether method is one of ChunkingChoices.
func ValidChunkingMethod(method string) bool {
	method = NormalizeChunkingMethod(method)
	for _, choice := range ChunkingChoices {
		if choice.Value == method {
			return true
		}
	}
	return false
}

// ChunkingLabel returns the display label of a method, or the value itself for
// an unknown one.
func ChunkingLabel(method string) string {
	method = NormalizeChunkingMethod(method)
	for _, choice := range ChunkingChoices {
		if choice.Value == method {
			return choice.Label
		}
	}
	return method
}
