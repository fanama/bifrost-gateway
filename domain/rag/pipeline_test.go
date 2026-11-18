package rag

import "testing"

func TestLexicalSearchRanksExactTerm(t *testing.T) {
	chunks := []Chunk{
		{ID: "a", Text: "alpha beta gamma"},
		{ID: "b", Text: "the ORION-42 reference is exact"},
	}
	hits := LexicalSearch("orion 42", chunks, 2)
	if len(hits) != 1 || hits[0].Chunk.ID != "b" {
		t.Fatalf("lexical search: %#v", hits)
	}
}

func TestLexicalSearchDropsPassagesWithoutEvidence(t *testing.T) {
	chunks := []Chunk{{ID: "a", Text: "alpha beta"}}
	if hits := LexicalSearch("zeta", chunks, 5); len(hits) != 0 {
		t.Fatalf("unrelated passage kept: %#v", hits)
	}
}

func TestReciprocalRankFusionMergesLists(t *testing.T) {
	dense := []RetrievedChunk{{Chunk: Chunk{ID: "a"}}, {Chunk: Chunk{ID: "b"}}}
	lexical := []RetrievedChunk{{Chunk: Chunk{ID: "b"}}, {Chunk: Chunk{ID: "c"}}}
	fused := ReciprocalRankFusion(dense, lexical)
	if len(fused) != 3 {
		t.Fatalf("fused size = %d, want 3", len(fused))
	}
	if fused[0].Chunk.ID != "b" {
		t.Fatalf("shared passage should lead: %#v", fused)
	}
	if fused[0].Score <= fused[1].Score {
		t.Fatalf("fused scores not ordered: %#v", fused)
	}
}

func TestMMRPrefersDiversePassage(t *testing.T) {
	query := []float32{1, 0}
	hits := []RetrievedChunk{
		{Chunk: Chunk{ID: "a", Vector: []float32{1, 0}}, Score: 1},
		{Chunk: Chunk{ID: "b", Vector: []float32{0.99, 0.05}}, Score: 0.98},
		{Chunk: Chunk{ID: "c", Vector: []float32{0, 1}}, Score: 0.5},
	}
	reranked := MaximalMarginalRelevance(query, hits, 2, 0.5)
	if len(reranked) != 2 || reranked[0].Chunk.ID != "a" || reranked[1].Chunk.ID != "c" {
		t.Fatalf("MMR kept redundant passage: %#v", reranked)
	}
}

func TestPipelineSettingsNormalizeAndValidate(t *testing.T) {
	settings := DefaultSettings("p1")
	if settings.RetrievalMode != RetrievalDense || settings.RetrievalCandidates != DefaultTopK {
		t.Fatalf("unexpected pipeline defaults: %#v", settings)
	}
	if settings.Reranker != RerankNone || settings.MMRLambda != DefaultMMRLambda {
		t.Fatalf("unexpected rerank defaults: %#v", settings)
	}
	settings.Normalize()
	if err := settings.Validate(); err != nil {
		t.Fatalf("defaults rejected: %v", err)
	}

	bad := DefaultSettings("p1")
	bad.Normalize()
	bad.RetrievalMode = "bogus"
	if err := bad.Validate(); err == nil {
		t.Fatal("expected retrieval mode validation error")
	}

	bad = DefaultSettings("p1")
	bad.Normalize()
	bad.TopK = 4
	bad.RetrievalCandidates = 2
	if err := bad.Validate(); err == nil {
		t.Fatal("expected candidates validation error")
	}

	bad = DefaultSettings("p1")
	bad.Normalize()
	bad.MMRLambda = 2
	if err := bad.Validate(); err == nil {
		t.Fatal("expected lambda validation error")
	}
}
