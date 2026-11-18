package rag

import (
	"strings"
	"unicode"
)

// SplitSentences cuts text into sentence-sized units. Paragraph breaks are hard
// separators; a period, question mark or exclamation mark ends a unit when it
// is followed by whitespace. Empty units are never returned.
func SplitSentences(text string) []string {
	text = strings.TrimSpace(strings.ReplaceAll(text, "\r\n", "\n"))
	if text == "" {
		return nil
	}
	var units []string
	for _, block := range strings.Split(text, "\n\n") {
		if block = strings.TrimSpace(block); block != "" {
			units = append(units, splitBlock(block)...)
		}
	}
	return units
}

// ChunkSentences groups sentence-sized units into passages of at most size
// runes. Sentences stay whole and the tail of a passage is repeated as the
// overlap of the next one.
func ChunkSentences(text string, size, overlap int) []string {
	size, overlap = normalizeWindow(size, overlap)
	return groupUnits(boundedUnits(SplitSentences(text), size), size, overlap)
}

// splitBlock cuts one paragraph at sentence endings, keeping list markers such
// as "1." attached to their item.
func splitBlock(block string) []string {
	runes := []rune(block)
	var units []string
	start, lineStart := 0, 0
	for i := 0; i < len(runes); i++ {
		if runes[i] == '\n' {
			lineStart = i + 1
			continue
		}
		if !endsSentence(runes, lineStart, i) {
			continue
		}
		if unit := strings.TrimSpace(string(runes[start : i+1])); unit != "" {
			units = append(units, unit)
		}
		start = i + 1
		for start < len(runes) && unicode.IsSpace(runes[start]) {
			start++
		}
		i = start - 1
	}
	if unit := strings.TrimSpace(string(runes[start:])); unit != "" {
		units = append(units, unit)
	}
	return units
}

func endsSentence(runes []rune, lineStart, i int) bool {
	if !strings.ContainsRune(".!?", runes[i]) {
		return false
	}
	if i+1 < len(runes) && !unicode.IsSpace(runes[i+1]) {
		return false
	}
	return !isListMarker(runes, lineStart, i)
}

func isListMarker(runes []rune, lineStart, i int) bool {
	for j := lineStart; j < i; j++ {
		if !unicode.IsDigit(runes[j]) {
			return false
		}
	}
	return i > lineStart
}

// boundedUnits splits any unit longer than size with the fixed window, so the
// grouping pass only sees building blocks that fit.
func boundedUnits(units []string, size int) []string {
	var out []string
	for _, unit := range units {
		if len([]rune(unit)) <= size {
			out = append(out, unit)
			continue
		}
		out = append(out, ChunkText(unit, size, 0)...)
	}
	return out
}

// groupUnits packs consecutive units into passages of at most size runes and
// keeps the tail of each passage as the overlap of the next one.
func groupUnits(units []string, size, overlap int) []string {
	size, overlap = normalizeWindow(size, overlap)
	var chunks []string
	for start := 0; start < len(units); {
		end := packUnits(units, start, size)
		if chunk := strings.TrimSpace(strings.Join(units[start:end], " ")); chunk != "" {
			chunks = append(chunks, chunk)
		}
		if end >= len(units) {
			break
		}
		start = nextUnitStart(units, start, end, overlap)
	}
	return chunks
}

// packUnits returns the end index of the passage beginning at start: units are
// added while the rune budget allows it, and a single oversized unit always
// forms a passage of its own.
func packUnits(units []string, start, size int) int {
	length, end := 0, start
	for end < len(units) {
		width := len([]rune(units[end]))
		if end > start && length+1+width > size {
			break
		}
		if end > start {
			length++
		}
		length += width
		end++
	}
	return end
}

// nextUnitStart returns the first unit of the next passage, repeating the tail
// units of the current one while they fit in the overlap budget.
func nextUnitStart(units []string, start, end, overlap int) int {
	if overlap <= 0 {
		return end
	}
	next := end
	for i := end - 1; i > start; i-- {
		width := len([]rune(units[i]))
		if width > overlap {
			break
		}
		overlap -= width
		next = i
	}
	if next <= start {
		return end
	}
	return next
}
