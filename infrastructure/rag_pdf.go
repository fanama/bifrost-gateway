package infrastructure

import (
	"bytes"
	"fmt"

	"bridge-gateway/domain"
	"github.com/ledongthuc/pdf"
)

// ragPDFMarkdown extracts the text layer of a PDF and renders it as Markdown, so
// the chunker and retrieval work on structured text. A page without a text layer
// (a scan) simply contributes nothing: the caller then receives the standard
// "no readable text" error. Encrypted or corrupt files fail with an explicit
// reason instead of a mojibake document.
//
// The PDF reader reports a malformed stream by panicking rather than by
// returning an error, so the whole read turns a panic into the same validation
// error: an upload never crashes the HTTP handler.
func ragPDFMarkdown(content []byte) (text string, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			text = ""
			err = &domain.InvalidRequestError{Param: "file", Reason: fmt.Sprintf("unreadable PDF: %v", recovered)}
		}
	}()
	doc, err := pdf.NewReader(bytes.NewReader(content), int64(len(content)))
	if err != nil {
		return "", &domain.InvalidRequestError{Param: "file", Reason: "unreadable PDF: " + err.Error()}
	}
	return pdfMarkdown(doc), nil
}

// newLine reports whether a baseline move starts a new visual line.
func newLine(y, lastY float64) bool {
	if y < lastY {
		y, lastY = lastY, y
	}
	return y-lastY > 1
}
