package web

import (
	"bytes"
	"html/template"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/renderer/html"
)

// markdownRenderer converts the Markdown models and users type in the chat.
// GFM covers the tables, task lists and strikethrough that models commonly
// emit; hard wraps keep single newlines readable in a bubble.
var markdownRenderer = goldmark.New(
	goldmark.WithExtensions(extension.GFM),
	goldmark.WithRendererOptions(html.WithHardWraps()),
)

// renderMarkdown renders src as HTML for a chat bubble.
//
// The unsafe renderer is deliberately left off: raw HTML coming from a model
// is dropped instead of being injected into the page, so a reply can never run
// a script. The result is safe to hand to html/template as trusted HTML.
func renderMarkdown(src string) template.HTML {
	if strings.TrimSpace(src) == "" {
		return ""
	}
	var buf bytes.Buffer
	if err := markdownRenderer.Convert([]byte(src), &buf); err != nil {
		// A failed render must not hide the reply: show it as plain text.
		return template.HTML(template.HTMLEscapeString(src))
	}
	return template.HTML(buf.String())
}
