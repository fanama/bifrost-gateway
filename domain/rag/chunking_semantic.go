package rag

import "strings"

// SemanticThreshold is the cosine similarity under which two adjacent sentence
// embeddings are considered to be about different topics.
const SemanticThreshold = 0.5

// GroupSemantic turns sentence embeddings into passages: a sentence joins the
// current passage while its vector stays close to the previous one and the rune
// budget allows it. Sentences are never repeated, so semantic passages carry no
// overlap. units and vectors must be aligned; otherwise nil is returned.
func GroupSemantic(units []string, vectors [][]float32, size int) []string {
	size, _ = normalizeWindow(size, 0)
	if len(units) == 0 || len(units) != len(vectors) {
		return nil
	}
	var chunks []string
	var current []string
	length := 0
	flush := func() {
		if len(current) > 0 {
			chunks = append(chunks, strings.Join(current, " "))
			current, length = nil, 0
		}
	}
	for i, unit := range units {
		width := len([]rune(unit))
		if width > size {
			flush()
			chunks = append(chunks, ChunkText(unit, size, 0)...)
			continue
		}
		if len(current) > 0 && (length+1+width > size || !adjacentSimilar(vectors, i)) {
			flush()
		}
		current = append(current, unit)
		length += width
		if len(current) > 1 {
			length++
		}
	}
	flush()
	return chunks
}

func adjacentSimilar(vectors [][]float32, i int) bool {
	return CosineSimilarity(vectors[i-1], vectors[i]) >= SemanticThreshold
}
