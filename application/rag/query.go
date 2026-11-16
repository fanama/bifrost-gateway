package rag

import (
	"context"
	"log"
	"math/rand"
	"strings"

	"bridge-gateway/domain"
	"bridge-gateway/domain/errs"
	domainrag "bridge-gateway/domain/rag"
)

// Query retrieves passages and asks the configured chat model to answer from
// them. When no passage matches, the model still receives the explicit
// no-context instruction instead of the bare question.
func (s *Service) Query(ctx context.Context, projectID, question string, opts QueryOptions) (*domainrag.Answer, error) {
	projectID = trimProject(projectID)
	settings, err := s.resolvedSettings(ctx, projectID)
	if err != nil {
		return nil, err
	}
	opts = applyQuerySettings(settings, opts)
	result, err := s.searchWithSettings(ctx, projectID, question, opts, settings)
	if err != nil {
		return nil, err
	}
	cfg, err := s.activeConfig(ctx, projectID)
	if err != nil {
		return nil, err
	}
	if s.llm == nil {
		return nil, errs.New(errs.CodeInternal, "chat provider is not configured")
	}
	messages := domainrag.BuildMessages(question, result.Chunks, opts.SystemPrompt)
	reply, err := s.llm.Chat(ctx, cfg, messages)
	if err != nil {
		return nil, err
	}
	return &domainrag.Answer{
		Answer:     reply.Content,
		Sources:    domainrag.Sources(result.Chunks),
		HasContext: len(result.Chunks) > 0,
		Model:      reply.Model,
		Degraded:   result.Degraded,
		IndexState: result.IndexState,
	}, nil
}

// Search returns the top passages for a question without calling the model.
func (s *Service) Search(ctx context.Context, projectID, question string, opts QueryOptions) (*domainrag.SearchResult, error) {
	projectID = trimProject(projectID)
	settings, err := s.resolvedSettings(ctx, projectID)
	if err != nil {
		return nil, err
	}
	opts = applyQuerySettings(settings, opts)
	return s.searchWithSettings(ctx, projectID, question, opts, settings)
}

// searchWithSettings is the single retrieval path shared by Query and Search.
// It asks the index first, then falls back to the exact scan whenever the graph
// cannot answer completely. The fallback is always visible to the caller.
func (s *Service) searchWithSettings(
	ctx context.Context,
	projectID, question string,
	opts QueryOptions,
	settings *domainrag.Settings,
) (*domainrag.SearchResult, error) {
	if projectID == "" {
		return nil, &domain.ValidationError{Fields: []string{"project_id"}}
	}
	question = strings.TrimSpace(question)
	if question == "" {
		return nil, &domain.ValidationError{Fields: []string{"question"}}
	}
	if len([]rune(question)) > domainrag.MaxQuestionChars {
		return nil, &domain.InvalidRequestError{Param: "question", Reason: "question is too long"}
	}

	vector, model, err := s.embedTexts(ctx, projectID, []string{question}, opts.EmbeddingModel)
	if err != nil {
		return nil, err
	}
	k := normalizeTopK(opts.TopK)

	if s.index != nil {
		hits, status, ok := s.index.Search(ctx, projectID, model, vector[0], k, settings.IndexOptions())
		live := status.Nodes - status.Tombstones
		if ok && (len(hits) >= k || live < k) {
			s.maybeSampleRecall(ctx, projectID, model, vector[0], k, hits, settings)
			return &domainrag.SearchResult{Chunks: hits, IndexState: status.State}, nil
		}
		log.Printf("RAG index fallback: project=%s state=%s", projectID, status.State)
		return s.exactSearch(ctx, projectID, model, vector[0], k, true, status.State)
	}

	return s.exactSearch(ctx, projectID, model, vector[0], k, false, domainrag.IndexDisabled)
}

// exactSearch loads every project chunk and ranks it with the historical cosine
// scan. It is both the fallback and the reference implementation used by tests.
func (s *Service) exactSearch(
	ctx context.Context,
	projectID, model string,
	query []float32,
	k int,
	degraded bool,
	state domainrag.IndexState,
) (*domainrag.SearchResult, error) {
	chunks, err := s.chunks.ListByProject(ctx, projectID)
	if err != nil {
		return nil, err
	}
	return &domainrag.SearchResult{
		Chunks:     domainrag.RankChunks(query, compatible(chunks, model, len(query)), k),
		Degraded:   degraded,
		IndexState: state,
	}, nil
}

// maybeSampleRecall optionally compares the ANN answer with the exact scan and
// logs the overlap. It is diagnostic only: the response is never changed and
// the rate defaults to zero.
func (s *Service) maybeSampleRecall(
	ctx context.Context,
	projectID, model string,
	query []float32,
	k int,
	hits []domainrag.RetrievedChunk,
	settings *domainrag.Settings,
) {
	if settings == nil || settings.HNSWRecallSampleRate <= 0 || rand.Float64() >= settings.HNSWRecallSampleRate {
		return
	}
	exact, err := s.exactSearch(ctx, projectID, model, query, k, false, domainrag.IndexReady)
	if err != nil || len(exact.Chunks) == 0 {
		return
	}
	found := map[string]bool{}
	for _, hit := range hits {
		found[hit.Chunk.ID] = true
	}
	matched := 0
	for _, hit := range exact.Chunks {
		if found[hit.Chunk.ID] {
			matched++
		}
	}
	log.Printf("RAG index recall sample: project=%s matched=%d/%d", projectID, matched, len(exact.Chunks))
}

func normalizeTopK(k int) int {
	if k <= 0 {
		return domainrag.DefaultTopK
	}
	if k > domainrag.MaxTopK {
		return domainrag.MaxTopK
	}
	return k
}

// compatible drops chunks embedded with another model or width: mixing those
// vectors would compare unrelated spaces.
func compatible(chunks []domainrag.Chunk, model string, width int) []domainrag.Chunk {
	out := make([]domainrag.Chunk, 0, len(chunks))
	for _, chunk := range chunks {
		if chunk.EmbeddingModel == model && len(chunk.Vector) == width {
			out = append(out, chunk)
		}
	}
	return out
}

func (s *Service) activeConfig(ctx context.Context, projectID string) (*domain.ChatConfig, error) {
	if s.configs == nil {
		return nil, domain.ErrNoActiveConfig
	}
	return s.configs.GetActiveByProject(ctx, projectID)
}
