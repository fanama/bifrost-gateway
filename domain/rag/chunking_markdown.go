package rag

import "strings"

// ChunkMarkdown splits a document on its Markdown headings so a passage never
// mixes two sections. A section larger than the target size is re-split on
// sentence boundaries, and every resulting passage keeps its heading as the
// first line, preserving the structure the extractors produce.
func ChunkMarkdown(text string, size, overlap int) []string {
	size, overlap = normalizeWindow(size, overlap)
	sections := markdownSections(text)
	if len(sections) == 0 {
		return ChunkSentences(text, size, overlap)
	}
	var chunks []string
	for _, section := range sections {
		heading, body := splitHeading(section)
		parts := ChunkSentences(body, size, overlap)
		if len(parts) == 0 {
			parts = []string{""}
		}
		for _, part := range parts {
			chunks = append(chunks, joinHeading(heading, part))
		}
	}
	return chunks
}

// markdownSections cuts the text at heading lines, leaving fenced code blocks
// untouched: a "#" inside a fence is code, not a heading.
func markdownSections(text string) []string {
	var sections []string
	var current []string
	fence := ""
	flush := func() {
		if section := strings.TrimSpace(strings.Join(current, "\n")); section != "" {
			sections = append(sections, section)
		}
		current = nil
	}
	for _, line := range strings.Split(text, "\n") {
		fence = fenceAfter(line, fence)
		if fence == "" && isHeadingLine(line) {
			flush()
		}
		current = append(current, line)
	}
	flush()
	return sections
}

// fenceAfter returns the fence state after line: empty outside a fenced block,
// the fence marker while inside one.
func fenceAfter(line, fence string) string {
	trimmed := strings.TrimSpace(line)
	if fence == "" {
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			return trimmed[:3]
		}
		return ""
	}
	if strings.HasPrefix(trimmed, fence) {
		return ""
	}
	return fence
}

func isHeadingLine(line string) bool {
	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(trimmed, "#") {
		return false
	}
	level := 0
	for level < len(trimmed) && trimmed[level] == '#' {
		level++
	}
	return level <= 6 && (level == len(trimmed) || trimmed[level] == ' ')
}

func splitHeading(section string) (string, string) {
	line, rest, _ := strings.Cut(section, "\n")
	if isHeadingLine(line) {
		return strings.TrimSpace(line), strings.TrimSpace(rest)
	}
	return "", strings.TrimSpace(section)
}

func joinHeading(heading, body string) string {
	body = strings.TrimSpace(body)
	switch {
	case heading == "":
		return body
	case body == "":
		return heading
	default:
		return heading + "\n\n" + body
	}
}
