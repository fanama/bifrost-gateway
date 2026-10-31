package rag

import (
	"math"
	"sort"
)

// CosineSimilarity returns the cosine of the angle between two vectors. It
// uses the common prefix when widths differ, and returns 0 for an empty or
// zero vector rather than dividing by zero.
func CosineSimilarity(a, b []float32) float64 {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	if n == 0 {
		return 0
	}
	var dot, normA, normB float64
	for i := 0; i < n; i++ {
		x := float64(a[i])
		y := float64(b[i])
		dot += x * y
		normA += x * x
		normB += y * y
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return dot / math.Sqrt(normA*normB)
}

// RankChunks scores every chunk sharing the query width, orders them by
// descending similarity and keeps at most topK results.
func RankChunks(query []float32, chunks []Chunk, topK int) []RetrievedChunk {
	if topK <= 0 {
		topK = DefaultTopK
	}
	if topK > MaxTopK {
		topK = MaxTopK
	}
	scored := make([]RetrievedChunk, 0, len(chunks))
	for _, chunk := range chunks {
		if len(chunk.Vector) == 0 || len(chunk.Vector) != len(query) {
			continue
		}
		scored = append(scored, RetrievedChunk{
			Chunk: chunk,
			Score: CosineSimilarity(query, chunk.Vector),
		})
	}
	sort.SliceStable(scored, func(i, j int) bool {
		return scored[i].Score > scored[j].Score
	})
	if len(scored) > topK {
		scored = scored[:topK]
	}
	return scored
}

// Sources projects retrieved passages into citations, one per passage.
func Sources(retrieved []RetrievedChunk) []Source {
	sources := make([]Source, 0, len(retrieved))
	for _, item := range retrieved {
		sources = append(sources, Source{
			DocumentID: item.Chunk.DocumentID,
			FileName:   item.Chunk.FileName,
			ChunkIndex: item.Chunk.Ordinal,
			Score:      item.Score,
		})
	}
	return sources
}
