package infrastructure

import (
	"context"
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
	ragSupportedTypes  = "text, markdown, csv, json, log, yaml, xml, html, pdf, png, jpeg, webp, gif, bmp, tiff"
)

var (
	ragHTMLMarkup   = regexp.MustCompile(`(?is)<script.*?</script>|<style.*?</style>|<[^>]+>`)
	ragBlankLines   = regexp.MustCompile(`\n{3,}`)
	ragTextSuffixes = map[string]bool{
		".txt": true, ".md": true, ".markdown": true, ".csv": true,
		".json": true, ".log": true, ".yaml": true, ".yml": true, ".xml": true,
	}
	ragImageSuffixes = map[string]bool{
		".png": true, ".jpg": true, ".jpeg": true, ".webp": true,
		".gif": true, ".bmp": true, ".tif": true, ".tiff": true,
	}
)

// RAGTextExtractor turns an upload into plain text: it decodes the text
// formats, strips HTML markup, renders the text layer of PDFs as Markdown and,
// when an OCR reader is attached, transcribes images. Binary Office formats are
// rejected explicitly rather than producing mojibake.
type RAGTextExtractor struct {
	maxBytes int
	images   domainrag.ImageReader
}

// NewRAGTextExtractor builds an extractor; a non-positive limit uses 25 MiB.
func NewRAGTextExtractor(maxBytes int) *RAGTextExtractor {
	if maxBytes <= 0 {
		maxBytes = ragMaxExtractBytes
	}
	return &RAGTextExtractor{maxBytes: maxBytes}
}

// WithImages attaches the image OCR port. Without it, image uploads are
// rejected with an explicit message instead of being indexed as garbage.
func (e *RAGTextExtractor) WithImages(reader domainrag.ImageReader) *RAGTextExtractor {
	e.images = reader
	return e
}

var _ domainrag.TextExtractor = (*RAGTextExtractor)(nil)

// Extract decodes one upload into normalized plain text.
func (e *RAGTextExtractor) Extract(ctx context.Context, projectID, fileName string, content []byte) (string, error) {
	if len(content) == 0 {
		return "", &domain.InvalidRequestError{Param: "file", Reason: "file is empty"}
	}
	if len(content) > e.maxBytes {
		return "", &domain.InvalidRequestError{Param: "file", Reason: "file exceeds the extractor size limit"}
	}
	text, err := e.decode(ctx, projectID, fileName, content)
	if err != nil {
		return "", err
	}
	return finalizeRAGText(text)
}

// decode routes one upload to the reader matching its extension.
func (e *RAGTextExtractor) decode(ctx context.Context, projectID, fileName string, content []byte) (string, error) {
	ext := strings.ToLower(filepath.Ext(fileName))
	switch {
	case ragTextSuffixes[ext]:
		return string(content), nil
	case ext == ".html" || ext == ".htm":
		return stripHTML(string(content)), nil
	case ext == ".pdf":
		return ragPDFMarkdown(content)
	case ragImageSuffixes[ext]:
		return e.readImage(ctx, projectID, fileName, content)
	default:
		return "", &domain.InvalidRequestError{
			Param:  "file",
			Reason: "unsupported file type " + ext + "; supported: " + ragSupportedTypes,
		}
	}
}

// readImage delegates to the OCR port. The project is forwarded so the adapter
// can resolve the project's vision model.
func (e *RAGTextExtractor) readImage(ctx context.Context, projectID, fileName string, content []byte) (string, error) {
	if e.images == nil {
		return "", &domain.InvalidRequestError{Param: "file", Reason: "image OCR is not configured"}
	}
	return e.images.ReadImage(ctx, projectID, fileName, content)
}

// finalizeRAGText normalizes the text, rejects empty results and applies the
// character cap so one huge document cannot exhaust memory downstream.
func finalizeRAGText(text string) (string, error) {
	text = normalizeRAGText(text)
	if text == "" {
		return "", &domain.InvalidRequestError{Param: "file", Reason: "document has no readable text"}
	}
	if runes := []rune(text); len(runes) > ragMaxExtractChars {
		text = string(runes[:ragMaxExtractChars])
	}
	return text, nil
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
