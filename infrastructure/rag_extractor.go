package infrastructure

import (
	"html"
	"path/filepath"
	"regexp"
	"strings"

	"bridge-gateway/domain"
	domainrag "bridge-gateway/domain/rag"
)

const (
	ragMaxExtractBytes = 25 << 20
	ragMaxExtractChars = 5_000_000
)

var (
	ragHTMLMarkup   = regexp.MustCompile(`(?is)<script.*?</script>|<style.*?</style>|<[^>]+>`)
	ragBlankLines   = regexp.MustCompile(`\n{3,}`)
	ragTextSuffixes = map[string]bool{
		".txt": true, ".md": true, ".markdown": true, ".csv": true,
		".json": true, ".log": true, ".yaml": true, ".yml": true, ".xml": true,
	}
)

// RAGTextExtractor reads plain-text formats and strips HTML markup. Binary
// formats (PDF, Office) are rejected explicitly rather than producing mojibake.
type RAGTextExtractor struct {
	maxBytes int
}

// NewRAGTextExtractor builds an extractor; a non-positive limit uses 25 MiB.
func NewRAGTextExtractor(maxBytes int) *RAGTextExtractor {
	if maxBytes <= 0 {
		maxBytes = ragMaxExtractBytes
	}
	return &RAGTextExtractor{maxBytes: maxBytes}
}

var _ domainrag.TextExtractor = (*RAGTextExtractor)(nil)

// Extract decodes one upload into normalized plain text.
func (e *RAGTextExtractor) Extract(fileName string, content []byte) (string, error) {
	if len(content) == 0 {
		return "", &domain.InvalidRequestError{Param: "file", Reason: "file is empty"}
	}
	if len(content) > e.maxBytes {
		return "", &domain.InvalidRequestError{Param: "file", Reason: "file exceeds the extractor size limit"}
	}
	text, err := decodeText(fileName, content)
	if err != nil {
		return "", err
	}
	text = normalizeRAGText(text)
	if text == "" {
		return "", &domain.InvalidRequestError{Param: "file", Reason: "document has no readable text"}
	}
	if len([]rune(text)) > ragMaxExtractChars {
		text = string([]rune(text)[:ragMaxExtractChars])
	}
	return text, nil
}

func decodeText(fileName string, content []byte) (string, error) {
	ext := strings.ToLower(filepath.Ext(fileName))
	switch {
	case ragTextSuffixes[ext]:
		return string(content), nil
	case ext == ".html" || ext == ".htm":
		return stripHTML(string(content)), nil
	default:
		return "", &domain.InvalidRequestError{
			Param:  "file",
			Reason: "unsupported file type " + ext + "; supported: text, markdown, csv, json, log, yaml, xml, html",
		}
	}
}

func stripHTML(raw string) string {
	raw = ragHTMLMarkup.ReplaceAllString(raw, " ")
	return html.UnescapeString(raw)
}

func normalizeRAGText(text string) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	text = ragBlankLines.ReplaceAllString(text, "\n\n")
	return strings.TrimSpace(text)
}
