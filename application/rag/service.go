// Package rag orchestrates document ingestion and grounded answers. It depends
// only on domain ports: persistence, extraction, embeddings and chat are
// injected by the composition root.
package rag

import (
	"strings"
	"time"

	"bridge-gateway/domain"
	domainrag "bridge-gateway/domain/rag"
)

// IngestOptions tunes one ingestion call. Zero values fall back to the domain
// defaults.
type IngestOptions struct {
	EmbeddingModel string
	ChunkSize      int
	ChunkOverlap   int
}

// QueryOptions tunes retrieval and generation for one question.
type QueryOptions struct {
	EmbeddingModel string
	TopK           int
	SystemPrompt   string
}

// Service is the RAG application service. ids and now are injectable so tests
// can assert deterministic entities without touching the clock.
type Service struct {
	docs      domainrag.DocumentRepository
	chunks    domainrag.ChunkRepository
	extractor domainrag.TextExtractor
	embedder  domain.EmbeddingProvider
	configs   domain.ChatConfigRepository
	settings  domainrag.SettingsRepository
	llm       domain.LLMProvider
	ids       func() string
	now       func() time.Time
}

// NewService wires the use case with its outbound ports.
func NewService(
	docs domainrag.DocumentRepository,
	chunks domainrag.ChunkRepository,
	extractor domainrag.TextExtractor,
	embedder domain.EmbeddingProvider,
	configs domain.ChatConfigRepository,
	settings domainrag.SettingsRepository,
	llm domain.LLMProvider,
) *Service {
	return &Service{
		docs:      docs,
		chunks:    chunks,
		extractor: extractor,
		embedder:  embedder,
		configs:   configs,
		settings:  settings,
		llm:       llm,
		ids:       domain.NewID,
		now:       time.Now,
	}
}

func trimProject(projectID string) string {
	return strings.TrimSpace(projectID)
}
