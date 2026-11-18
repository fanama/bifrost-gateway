package rag

import "strings"

// Retrieval strategies selectable per project. Dense keeps the historical
// vector ranking; hybrid fuses it with a lexical BM25 ranking of the same
// passages, so an exact identifier is not lost between near-synonyms.
const (
	RetrievalDense  = "dense"
	RetrievalHybrid = "hybrid"

	// DefaultRetrievalMode preserves the behaviour of existing projects.
	DefaultRetrievalMode = RetrievalDense
	// MaxRetrievalCandidates bounds the pool fetched before reranking.
	MaxRetrievalCandidates = 200
)

// Rerankers selectable per project. None keeps the retrieval order; mmr trades
// a little relevance for diversity, which keeps four near-duplicate passages
// from filling the prompt.
const (
	RerankNone = "none"
	RerankMMR  = "mmr"

	DefaultReranker  = RerankNone
	DefaultMMRLambda = 0.7
	MinMMRLambda     = 0.1
	MaxMMRLambda     = 1.0
)

// PipelineChoice is one selectable strategy with the label and hint shown by
// the settings page.
type PipelineChoice struct {
	Value string
	Label string
	Hint  string
}

// RetrievalChoices is the single source of truth for the retrieval strategies
// the UI offers and the domain accepts.
var RetrievalChoices = []PipelineChoice{
	{
		Value: RetrievalDense,
		Label: "Dense (vecteurs)",
		Hint:  "Classe les passages par similarité cosinus, via l'index HNSW.",
	},
	{
		Value: RetrievalHybrid,
		Label: "Hybride (dense + BM25)",
		Hint:  "Fusionne le classement dense et un classement lexical BM25 par Reciprocal Rank Fusion.",
	},
}

// RerankChoices is the single source of truth for the rerankers the UI offers
// and the domain accepts.
var RerankChoices = []PipelineChoice{
	{
		Value: RerankNone,
		Label: "Aucun",
		Hint:  "Conserve l'ordre produit par le retrieval.",
	},
	{
		Value: RerankMMR,
		Label: "MMR (diversité)",
		Hint:  "Pénalise les passages redondants : pertinence moins redondance, pondérées par lambda.",
	},
}

// NormalizeRetrievalMode trims and lowercases a mode, mapping an empty value to
// the default.
func NormalizeRetrievalMode(mode string) string {
	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode == "" {
		return DefaultRetrievalMode
	}
	return mode
}

// ValidRetrievalMode reports whether mode is one of RetrievalChoices.
func ValidRetrievalMode(mode string) bool {
	mode = NormalizeRetrievalMode(mode)
	for _, choice := range RetrievalChoices {
		if choice.Value == mode {
			return true
		}
	}
	return false
}

// RetrievalLabel returns the display label of a mode, or the value itself for
// an unknown one.
func RetrievalLabel(mode string) string {
	return choiceLabel(RetrievalChoices, NormalizeRetrievalMode(mode))
}

// NormalizeReranker trims and lowercases a reranker name, mapping an empty
// value to the default.
func NormalizeReranker(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return DefaultReranker
	}
	return name
}

// ValidReranker reports whether name is one of RerankChoices.
func ValidReranker(name string) bool {
	name = NormalizeReranker(name)
	for _, choice := range RerankChoices {
		if choice.Value == name {
			return true
		}
	}
	return false
}

// RerankLabel returns the display label of a reranker, or the value itself for
// an unknown one.
func RerankLabel(name string) string {
	return choiceLabel(RerankChoices, NormalizeReranker(name))
}

func choiceLabel(choices []PipelineChoice, value string) string {
	for _, choice := range choices {
		if choice.Value == value {
			return choice.Label
		}
	}
	return value
}
