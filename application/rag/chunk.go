package rag

import (
	"context"

	"bridge-gateway/domain"
	domainrag "bridge-gateway/domain/rag"
)

// chunkPassages applies the project's chunking method to extracted text and
// rejects a document that yields no passage. Semantic chunking needs sentence
// embeddings before the passages exist, so it is orchestrated here while the
// text-only strategies stay in the domain.
func (s *Service) chunkPassages(ctx context.Context, projectID, text string, opts IngestOptions) ([]string, error) {
	passages, err := s.textPassages(ctx, projectID, text, opts)
	if err != nil {
		return nil, err
	}
	if len(passages) == 0 {
		return nil, &domain.InvalidRequestError{Param: "file", Reason: "document has no readable text"}
	}
	return passages, nil
}

// textPassages dispatches to the strategy itself, without the empty check, so
// each branch stays a single expression.
func (s *Service) textPassages(ctx context.Context, projectID, text string, opts IngestOptions) ([]string, error) {
	switch domainrag.NormalizeChunkingMethod(opts.ChunkingMethod) {
	case domainrag.ChunkingSemantic:
		return s.semanticPassages(ctx, projectID, text, opts)
	case domainrag.ChunkingSentence:
		return domainrag.ChunkSentences(text, opts.ChunkSize, opts.ChunkOverlap), nil
	case domainrag.ChunkingMarkdown:
		return domainrag.ChunkMarkdown(text, opts.ChunkSize, opts.ChunkOverlap), nil
	default:
		return domainrag.ChunkText(text, opts.ChunkSize, opts.ChunkOverlap), nil
	}
}

// semanticPassages embeds each sentence once, then groups the neighbours whose
// vectors are close. The extra batch is paid at ingestion only; the resulting
// passages are embedded again with the document, like every other method.
func (s *Service) semanticPassages(ctx context.Context, projectID, text string, opts IngestOptions) ([]string, error) {
	units := domainrag.SplitSentences(text)
	if len(units) == 0 {
		return nil, nil
	}
	vectors, _, err := s.embedTexts(ctx, projectID, units, opts.EmbeddingModel)
	if err != nil {
		return nil, err
	}
	return domainrag.GroupSemantic(units, vectors, opts.ChunkSize), nil
}
