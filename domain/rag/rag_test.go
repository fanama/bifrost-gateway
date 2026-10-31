package rag

import (
	"strings"
	"testing"
)

func TestChunkTextSplitsLongText(t *testing.T) {
	text := strings.Repeat("alpha beta gamma. ", 200)
	chunks := ChunkText(text, 100, 20)
	if len(chunks) < 2 {
		t.Fatalf("expected several chunks, got %d", len(chunks))
	}
	for _, chunk := range chunks {
		if strings.TrimSpace(chunk) == "" {
			t.Fatal("chunk must not be empty")
		}
	}
}

func TestChunkTextKeepsShortTextWhole(t *testing.T) {
	chunks := ChunkText("  short document  ", 100, 20)
	if len(chunks) != 1 || chunks[0] != "short document" {
		t.Fatalf("unexpected chunks: %#v", chunks)
	}
}

func TestChunkTextEmptyInput(t *testing.T) {
	if chunks := ChunkText("   ", 100, 20); chunks != nil {
		t.Fatalf("expected nil, got %#v", chunks)
	}
}

func TestRankChunksOrdersBySimilarity(t *testing.T) {
	chunks := []Chunk{
		{ID: "a", Vector: []float32{0, 1}},
		{ID: "b", Vector: []float32{1, 0}},
		{ID: "c", Vector: []float32{1, 0, 0}},
	}
	got := RankChunks([]float32{1, 0}, chunks, 2)
	if len(got) != 2 || got[0].Chunk.ID != "b" {
		t.Fatalf("unexpected ranking: %#v", got)
	}
	if got[0].Score != 1 {
		t.Fatalf("expected score 1, got %f", got[0].Score)
	}
}

func TestCosineSimilarityZeroVectors(t *testing.T) {
	if score := CosineSimilarity([]float32{0, 0}, []float32{1, 0}); score != 0 {
		t.Fatalf("expected 0, got %f", score)
	}
}

func TestBuildMessagesIncludesPassages(t *testing.T) {
	messages := BuildMessages("what is alpha?", []RetrievedChunk{{
		Chunk: Chunk{FileName: "doc.txt", Ordinal: 2, Text: "alpha is a project"},
		Score: 0.9,
	}}, "")
	if len(messages) != 2 || messages[0].Role != "system" || messages[1].Role != "user" {
		t.Fatalf("unexpected messages: %#v", messages)
	}
	if !strings.Contains(messages[1].Content, "[1] doc.txt") {
		t.Fatalf("passage header missing: %s", messages[1].Content)
	}
}

func TestBuildMessagesWithoutContext(t *testing.T) {
	messages := BuildMessages("unknown?", nil, "Answer in French.")
	if !strings.HasPrefix(messages[0].Content, "Answer in French.") {
		t.Fatalf("custom system prompt lost: %s", messages[0].Content)
	}
	if !strings.Contains(messages[1].Content, "No passage matched") {
		t.Fatalf("no-context rule missing: %s", messages[1].Content)
	}
}
