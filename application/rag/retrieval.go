package rag

import (
	"context"
	"log"

	domainrag "bridge-gateway/domain/rag"
)

// retrieve runs the configured pipeline: fetch a candidate pool from the dense
// index, optionally fuse it with a BM25 ranking, rerank it, then keep top_k.
func (s *Service) retrieve(
	ctx context.Context,
	projectID, question, model string,
	query []float32,
	k int,
	settings *domainrag.Settings,
) ([]domainrag.RetrievedChunk, domainrag.IndexState, bool, error) {
	limit := candidateLimit(k, settings)
	hits, state, degraded, err := s.denseCandidates(ctx, projectID, model, query, limit, settings)
	if err != nil {
		return nil, state, degraded, err
	}
	if settings.RetrievalMode == domainrag.RetrievalHybrid {
		hits, err = s.fuseLexical(ctx, projectID, question, model, len(query), hits, limit)
		if err != nil {
			return nil, state, degraded, err
		}
	}
	if settings.Reranker == domainrag.RerankMMR {
		return domainrag.MaximalMarginalRelevance(query, hits, k, settings.MMRLambda), state, degraded, nil
	}
	return firstHits(hits, k), state, degraded, nil
}

// denseCandidates asks the graph for the pool and falls back to the exact scan
// whenever the graph cannot answer completely. The fallback stays visible.
func (s *Service) denseCandidates(
	ctx context.Context,
	projectID, model string,
	query []float32,
	k int,
	settings *domainrag.Settings,
) ([]domainrag.RetrievedChunk, domainrag.IndexState, bool, error) {
	if s.index == nil {
		result, err := s.exactSearch(ctx, projectID, model, query, k, false, domainrag.IndexDisabled)
		if err != nil {
			return nil, domainrag.IndexDisabled, false, err
		}
		return result.Chunks, result.IndexState, result.Degraded, nil
	}
	hits, status, ok := s.index.Search(ctx, projectID, model, query, k, settings.IndexOptions())
	live := status.Nodes - status.Tombstones
	if ok && (len(hits) >= k || live < k) {
		s.maybeSampleRecall(ctx, projectID, model, query, k, hits, settings)
		return hits, status.State, false, nil
	}
	log.Printf("RAG index fallback: project=%s state=%s", projectID, status.State)
	exact, err := s.exactSearch(ctx, projectID, model, query, k, true, status.State)
	if err != nil {
		return nil, status.State, true, err
	}
	return exact.Chunks, exact.IndexState, exact.Degraded, nil
}

// fuseLexical ranks the same compatible passages with BM25 and merges the two
// rankings. The lexical side reads storage because it needs every passage's
// text, not only the graph candidates.
func (s *Service) fuseLexical(
	ctx context.Context,
	projectID, question, model string,
	width int,
	dense []domainrag.RetrievedChunk,
	limit int,
) ([]domainrag.RetrievedChunk, error) {
	chunks, err := s.chunks.ListByProject(ctx, projectID)
	if err != nil {
		return nil, err
	}
	lexical := domainrag.LexicalSearch(question, compatible(chunks, model, width), limit)
	return domainrag.ReciprocalRankFusion(dense, lexical), nil
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
	if settings == nil || settings.HNSWRecallSampleRate <= 0 {
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

// candidateLimit sizes the pool fetched before the rerank stage. A project that
// never configured it fetches exactly top_k, which is the historical behaviour.
func candidateLimit(k int, settings *domainrag.Settings) int {
	limit := k
	if settings != nil && settings.RetrievalCandidates > limit {
		limit = settings.RetrievalCandidates
	}
	if limit > domainrag.MaxRetrievalCandidates {
		limit = domainrag.MaxRetrievalCandidates
	}
	return limit
}

func firstHits(hits []domainrag.RetrievedChunk, k int) []domainrag.RetrievedChunk {
	if len(hits) > k {
		return hits[:k]
	}
	return hits
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
