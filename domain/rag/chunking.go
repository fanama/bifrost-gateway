package rag

import "strings"

const (
	// DefaultChunkSize is the target number of runes per passage.
	DefaultChunkSize = 1000
	// DefaultChunkOverlap keeps some prose between adjacent passages.
	DefaultChunkOverlap = 200
	// DefaultTopK is the number of passages sent to the model.
	DefaultTopK = 4
	// MaxTopK bounds a caller-provided top_k.
	MaxTopK = 20
	// MaxQuestionChars bounds the question forwarded to the model.
	MaxQuestionChars = 8000
	// MaxChunkChars bounds each passage flattened into the prompt.
	MaxChunkChars = 1500
)

// ChunkText splits text into overlapping windows, preferring paragraph and
// word boundaries so a passage is never cut mid-word when avoidable.
func ChunkText(text string, size, overlap int) []string {
	text = strings.TrimSpace(strings.ReplaceAll(text, "\r\n", "\n"))
	if text == "" {
		return nil
	}
	runes := []rune(text)
	size, overlap = normalizeWindow(size, overlap)
	var chunks []string
	for start := 0; start < len(runes); {
		end := windowEnd(runes, start, size)
		if chunk := strings.TrimSpace(string(runes[start:end])); chunk != "" {
			chunks = append(chunks, chunk)
		}
		if end >= len(runes) {
			break
		}
		start = nextStart(start, end, overlap)
	}
	return chunks
}

func normalizeWindow(size, overlap int) (int, int) {
	if size <= 0 {
		size = DefaultChunkSize
	}
	if overlap < 0 || overlap >= size {
		overlap = 0
	}
	return size, overlap
}

// windowEnd returns the end of the window beginning at start, snapped back to
// the nearest paragraph, newline or space in the second half of the window.
func windowEnd(runes []rune, start, size int) int {
	end := start + size
	if end >= len(runes) {
		return len(runes)
	}
	floor := start + size/2
	for i := end; i > floor; i-- {
		if runes[i-1] == '\n' || runes[i-1] == ' ' {
			return i
		}
	}
	return end
}

func nextStart(start, end, overlap int) int {
	next := end - overlap
	if next <= start {
		return end
	}
	return next
}
