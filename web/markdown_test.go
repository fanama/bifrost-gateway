package web

import (
	"strings"
	"testing"
)

func TestRenderMarkdownRendersCommonMarkdown(t *testing.T) {
	got := string(renderMarkdown("## Réponse\n\nDu **gras**, de l'*italique* et du `code`.\n\n- un\n- deux\n\n```go\nfmt.Println(\"x\")\n```"))

	for _, want := range []string{
		"<h2>Réponse</h2>",
		"<strong>gras</strong>",
		"<em>italique</em>",
		"<code>code</code>",
		"<li>un</li>",
		"<li>deux</li>",
		"<pre><code class=\"language-go\">fmt.Println(&quot;x&quot;)",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in:\n%s", want, got)
		}
	}
}

func TestRenderMarkdownDropsRawHTML(t *testing.T) {
	got := string(renderMarkdown("<script>alert(1)</script>\n\n<img src=x onerror=alert(2)>"))
	if strings.Contains(got, "<script") || strings.Contains(got, "<img") {
		t.Fatalf("raw HTML leaked into the bubble:\n%s", got)
	}
}

func TestRenderMarkdownEmptySource(t *testing.T) {
	if got := renderMarkdown("  \n"); got != "" {
		t.Fatalf("expected empty output, got %q", got)
	}
}
