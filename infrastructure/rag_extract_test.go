package infrastructure

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"

	"bridge-gateway/domain"
)

// stubImageReader records the project it was called for and returns fixed text.
type stubImageReader struct {
	projectID string
	fileName  string
}

func (s *stubImageReader) ReadImage(_ context.Context, projectID, fileName string, _ []byte) (string, error) {
	s.projectID, s.fileName = projectID, fileName
	return "Facture n° 42, total 120 €", nil
}

// progressive image bytes: only the JPEG magic matters to the sniffer.
func jpegBytes() []byte {
	return append([]byte{0xFF, 0xD8, 0xFF, 0xE0}, bytes.Repeat([]byte{0x00}, 20)...)
}

func TestRAGTextExtractorTranscribesImages(t *testing.T) {
	images := &stubImageReader{}
	extractor := NewRAGTextExtractor(0).WithImages(images)

	text, err := extractor.Extract(context.Background(), "p1", "facture.png", jpegBytes())
	if err != nil {
		t.Fatalf("extract image: %v", err)
	}
	if !strings.Contains(text, "Facture") {
		t.Fatalf("unexpected text: %q", text)
	}
	if images.projectID != "p1" || images.fileName != "facture.png" {
		t.Fatalf("extractor did not forward the upload: %#v", images)
	}
}

func TestRAGTextExtractorRejectsImagesWithoutOCR(t *testing.T) {
	_, err := NewRAGTextExtractor(0).Extract(context.Background(), "p1", "scan.png", jpegBytes())
	if err == nil || !strings.Contains(err.Error(), "OCR") {
		t.Fatalf("expected an explicit OCR error, got %v", err)
	}
}

func TestRAGImageOCRWithoutActiveConfig(t *testing.T) {
	ocr := NewRAGImageOCR(nil, nil)
	if _, err := ocr.ReadImage(context.Background(), "p1", "scan.png", jpegBytes()); err == nil {
		t.Fatal("expected an error when OCR is not wired")
	}
}

func TestRAGImageOCRNeedsActiveConfiguration(t *testing.T) {
	ocr := NewRAGImageOCR(&stubConfigRepo{}, stubVision{})
	_, err := ocr.ReadImage(context.Background(), "p1", "scan.png", jpegBytes())
	if err == nil || !strings.Contains(err.Error(), "vision model") {
		t.Fatalf("expected a configuration error, got %v", err)
	}
}

func TestRAGImageOCRSniffsTheRealFormat(t *testing.T) {
	if _, err := imageMime([]byte("not an image")); err == nil {
		t.Fatal("expected an unsupported format error")
	}
	if mime, err := imageMime(jpegBytes()); err != nil || mime != "image/jpeg" {
		t.Fatalf("sniff jpeg: %q %v", mime, err)
	}
}

func TestRAGPDFTextExtractsTheTextLayer(t *testing.T) {
	text, err := ragPDFMarkdown(minimalPDF("Orion is the alpha project"))
	if err != nil {
		t.Fatalf("pdf extract: %v", err)
	}
	if !strings.Contains(text, "Orion is the alpha project") {
		t.Fatalf("unexpected pdf text: %q", text)
	}
}

func TestRAGPDFTextRejectsCorruptFiles(t *testing.T) {
	_, err := ragPDFMarkdown([]byte("%PDF-1.7 not really a pdf"))
	if err == nil || !strings.Contains(err.Error(), "unreadable PDF") {
		t.Fatalf("expected an unreadable PDF error, got %v", err)
	}
}

func TestRAGTextExtractorRoutesPDFs(t *testing.T) {
	text, err := NewRAGTextExtractor(0).Extract(context.Background(), "p1", "Rapport.PDF",
		minimalPDF("Chiffre d'affaires 2026"))
	if err != nil {
		t.Fatalf("extract pdf: %v", err)
	}
	if !strings.Contains(text, "Chiffre d'affaires 2026") {
		t.Fatalf("unexpected text: %q", text)
	}
}

// widths returns a /Widths array for codes 32..126: glyphs advance half an em
// and the space a full em, so word boundaries are visible in the coordinates.
func widths() string {
	values := make([]string, 0, 95)
	for code := 32; code <= 126; code++ {
		value := "500"
		if code == 32 {
			value = "1000"
		}
		values = append(values, value)
	}
	return "[" + strings.Join(values, " ") + "]"
}

func TestRAGPDFTextSurvivesMalformedContentStream(t *testing.T) {
	// A declared /Length far beyond the real stream used to make the PDF reader
	// panic while reading tokens, which reached the HTTP handler. Whether the
	// library now salvages nothing or rejects the stream, the upload path must
	// return normally: a panic here fails the test by crashing the goroutine.
	defer func() {
		if recovered := recover(); recovered != nil {
			t.Fatalf("extractor panicked on a malformed stream: %v", recovered)
		}
	}()
	corrupt := buildPDF("BT /F1 12 Tf 20 100 Td (alpha) Tj ET", 999999)
	_, err := ragPDFMarkdown(corrupt)
	if err != nil && !strings.Contains(err.Error(), "unreadable PDF") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRAGPDFMarkdownRendersStructure(t *testing.T) {
	content := strings.Join([]string{
		"BT /F1 20 Tf 20 180 Td (Introduction) Tj ET",
		"BT /F1 10 Tf 20 150 Td (Le projet alpha est nomme Orion et il) Tj ET",
		"BT /F1 10 Tf 20 138 Td (couvre la recherche documentaire.) Tj ET",
		"BT /F1 10 Tf 20 110 Td (- Premier point) Tj ET",
		"BT /F1 10 Tf 20 98 Td (1. Second point) Tj ET",
	}, "\n")
	text, err := ragPDFMarkdown(pdfFromContent(content))
	if err != nil {
		t.Fatalf("pdf extract: %v", err)
	}
	for _, want := range []string{
		"# Introduction",
		"Le projet alpha est nomme Orion et il couvre la recherche documentaire.",
		"- Premier point",
		"1. Second point",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q in markdown:\n%s", want, text)
		}
	}
}

func TestRAGPDFMarkdownReattachesHyphenatedWords(t *testing.T) {
	content := strings.Join([]string{
		"BT /F1 10 Tf 20 150 Td (La recherche docu-) Tj ET",
		"BT /F1 10 Tf 20 138 Td (mentaire est couverte.) Tj ET",
	}, "\n")
	text, err := ragPDFMarkdown(pdfFromContent(content))
	if err != nil {
		t.Fatalf("pdf extract: %v", err)
	}
	if !strings.Contains(text, "documentaire est couverte.") {
		t.Fatalf("hyphenation not reattached: %q", text)
	}
}

// minimalPDF builds a valid one-page PDF whose content stream draws text with
// the base-14 Helvetica font.
func minimalPDF(text string) []byte {
	return pdfFromContent(fmt.Sprintf("BT /F1 12 Tf 20 100 Td (%s) Tj ET", text))
}

// pdfFromContent wraps a raw content stream in a one-page document.
func pdfFromContent(content string) []byte {
	return buildPDF(content, len(content))
}

// buildPDF assembles the document. Offsets are computed while writing, so the
// xref table stays correct whatever the declared stream length.
func buildPDF(content string, declaredLength int) []byte {
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Resources << /Font << /F1 5 0 R >> >> /Contents 4 0 R >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", declaredLength, content),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /FirstChar 32 /LastChar 126 /Widths " + widths() + " >>",
	}

	var buf bytes.Buffer
	buf.WriteString("%PDF-1.4\n")
	offsets := make([]int, len(objects))
	for i, object := range objects {
		offsets[i] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", i+1, object)
	}
	xref := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f \n", len(objects)+1)
	for _, offset := range offsets {
		fmt.Fprintf(&buf, "%010d 00000 n \n", offset)
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objects)+1, xref)
	return buf.Bytes()
}

// stubConfigRepo and stubVision keep the OCR adapter tests offline.

type stubConfigRepo struct{}

func (stubConfigRepo) List(context.Context) ([]domain.ChatConfig, error) { return nil, nil }
func (stubConfigRepo) Get(context.Context, string) (*domain.ChatConfig, error) {
	return nil, nil
}
func (stubConfigRepo) Create(context.Context, *domain.ChatConfig) error { return nil }
func (stubConfigRepo) Delete(context.Context, string) error             { return nil }
func (stubConfigRepo) Update(context.Context, *domain.ChatConfig) error { return nil }
func (stubConfigRepo) ListByProject(context.Context, string) ([]domain.ChatConfig, error) {
	return nil, nil
}
func (stubConfigRepo) GetActiveByProject(context.Context, string) (*domain.ChatConfig, error) {
	return nil, nil
}
func (stubConfigRepo) SetActive(context.Context, string, string) error { return nil }

type stubVision struct{}

func (stubVision) DescribeImage(context.Context, *domain.ChatConfig, string, []byte, string) (string, error) {
	return "", nil
}
