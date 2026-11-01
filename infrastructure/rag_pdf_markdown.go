package infrastructure

import (
	"regexp"
	"strings"

	"github.com/ledongthuc/pdf"
)

// pdfMarkdown converts the text layer of a PDF into Markdown. Glyphs are
// grouped into visual lines, then into blocks: headings (by font size, or a
// short bold line), list items (bullet or numbered) and paragraphs. Chunking
// and retrieval then work on structured text instead of one flat run of words,
// so a heading or a list item no longer ends up glued inside a paragraph.

const (
	// pdfHeadingRatio is the size ratio above which a line is a heading, and
	// pdfParagraphGap the number of body heights that separates two paragraphs.
	pdfHeadingRatio = 1.15
	pdfParagraphGap = 1.6
	// pdfMaxHeadingRunes keeps a bold body-size line from turning a whole
	// sentence into a heading.
	pdfMaxHeadingRunes = 80
)

var (
	pdfBulletMarker   = regexp.MustCompile(`^([-•*·▪◦‣–])\s+`)
	pdfNumberedMarker = regexp.MustCompile(`^(\d{1,3})[.)]\s+`)
	pdfSpaces         = regexp.MustCompile(`\s+`)
)

// pdfLine is one visual line of a page, with the layout data the block rules
// need.
type pdfLine struct {
	text string
	size float64
	x    float64
	top  float64
	bold bool
}

// pdfMarkdown renders every page as markdown blocks separated by blank lines.
func pdfMarkdown(doc *pdf.Reader) string {
	pages := make([][]pdfLine, 0, doc.NumPage())
	for i := 1; i <= doc.NumPage(); i++ {
		if page := doc.Page(i); !page.V.IsNull() {
			pages = append(pages, pageLines(page))
		}
	}
	body := pdfBodySize(pages)
	var blocks []string
	for _, lines := range pages {
		blocks = append(blocks, pageBlocks(lines, body)...)
	}
	return strings.Join(blocks, "\n\n")
}

// pageLines groups glyphs into lines: a baseline move starts a new line, and
// glyphs that do not abut are separated by a space so words are not glued.
func pageLines(page pdf.Page) []pdfLine {
	pieces := page.Content().Text
	lines := make([]pdfLine, 0, len(pieces)/8+1)
	var current pdfLine
	var text strings.Builder
	var lastX float64
	started := false
	for _, piece := range pieces {
		if started && newLine(piece.Y, current.top) {
			current.text = text.String()
			lines = append(lines, current)
			text.Reset()
			started = false
		}
		if !started {
			current = pdfLine{size: piece.FontSize, x: piece.X, top: piece.Y, bold: boldFont(piece.Font)}
			lastX = piece.X
			started = true
		} else if piece.X-lastX > piece.FontSize*0.2 {
			text.WriteByte(' ')
		}
		if piece.FontSize > current.size {
			current.size = piece.FontSize
		}
		text.WriteString(piece.S)
		lastX = piece.X + piece.W
	}
	if started {
		current.text = text.String()
		lines = append(lines, current)
	}
	return lines
}

// pdfBodySize returns the most frequent font size, weighted by text length: it
// is the size of the running text, against which headings are measured.
func pdfBodySize(pages [][]pdfLine) float64 {
	counts := map[float64]int{}
	for _, lines := range pages {
		for _, line := range lines {
			counts[line.size] += len([]rune(line.text))
		}
	}
	body, best := 0.0, -1
	for size, count := range counts {
		if count > best || (count == best && size < body) {
			body, best = size, count
		}
	}
	return body
}

// pageBlocks renders one page. A blank line always ends a block; paragraphs
// additionally end on a vertical gap large enough to be a paragraph break.
func pageBlocks(lines []pdfLine, body float64) []string {
	margin := pdfMargin(lines)
	var blocks []string
	var paragraph []string
	for i := range lines {
		line := lines[i]
		switch level := headingLevel(line, body); {
		case strings.TrimSpace(line.text) == "":
			blocks = flushParagraph(blocks, &paragraph)
		case level > 0:
			blocks = flushParagraph(blocks, &paragraph)
			blocks = append(blocks, strings.Repeat("#", level)+" "+collapse(line.text))
		case isListItem(line.text):
			blocks = flushParagraph(blocks, &paragraph)
			blocks = append(blocks, listItem(line, body, margin))
		default:
			paragraph = append(paragraph, line.text)
			if pdfGapAfter(lines, i) > body*pdfParagraphGap {
				blocks = flushParagraph(blocks, &paragraph)
			}
		}
	}
	return flushParagraph(blocks, &paragraph)
}

// headingLevel maps a line to an ATX level, or 0 when it is body text.
func headingLevel(line pdfLine, body float64) int {
	if body <= 0 || strings.TrimSpace(line.text) == "" {
		return 0
	}
	switch {
	case line.size >= body*1.8:
		return 1
	case line.size >= body*1.4:
		return 2
	case line.size >= body*pdfHeadingRatio:
		return 3
	case line.bold && len([]rune(line.text)) <= pdfMaxHeadingRunes && !strings.HasSuffix(line.text, "."):
		return 4
	default:
		return 0
	}
}

// listItem normalizes a bullet or a numbered item, keeping the nesting inferred
// from the horizontal offset relative to the page margin.
func listItem(line pdfLine, body, margin float64) string {
	text := collapse(line.text)
	depth := 0
	if body > 0 {
		depth = int((line.x - margin) / (body * 1.5))
	}
	if depth > 3 {
		depth = 3
	}
	if depth < 0 {
		depth = 0
	}
	prefix := strings.Repeat("  ", depth)
	if match := pdfNumberedMarker.FindStringSubmatch(text); match != nil {
		return prefix + match[1] + ". " + strings.TrimSpace(text[len(match[0]):])
	}
	return prefix + "- " + strings.TrimSpace(pdfBulletMarker.ReplaceAllString(text, ""))
}

// joinParagraph glues the lines of one paragraph. A line ending with a hyphen is
// a word cut by the layout, so the hyphen is dropped and the word reattached.
func joinParagraph(lines []string) string {
	words := make([]string, 0, len(lines))
	for _, line := range lines {
		text := strings.TrimSpace(line)
		if n := len(words); n > 0 && strings.HasSuffix(words[n-1], "-") {
			words[n-1] = strings.TrimSuffix(words[n-1], "-") + text
			continue
		}
		words = append(words, text)
	}
	return strings.Join(words, " ")
}

func flushParagraph(blocks []string, paragraph *[]string) []string {
	if len(*paragraph) == 0 {
		return blocks
	}
	blocks = append(blocks, joinParagraph(*paragraph))
	*paragraph = (*paragraph)[:0]
	return blocks
}

func isListItem(text string) bool {
	return pdfBulletMarker.MatchString(text) || pdfNumberedMarker.MatchString(text)
}

// pdfMargin is the left edge of the body text on a page.
func pdfMargin(lines []pdfLine) float64 {
	margin, found := 0.0, false
	for _, line := range lines {
		if strings.TrimSpace(line.text) == "" {
			continue
		}
		if !found || line.x < margin {
			margin, found = line.x, true
		}
	}
	return margin
}

// pdfGapAfter is the vertical distance to the next line, in points.
func pdfGapAfter(lines []pdfLine, i int) float64 {
	if i+1 >= len(lines) {
		return 0
	}
	if gap := lines[i].top - lines[i+1].top; gap > 0 {
		return gap
	}
	return 0
}

func boldFont(name string) bool {
	lower := strings.ToLower(name)
	return strings.Contains(lower, "bold") || strings.Contains(lower, "black") || strings.Contains(lower, "semibold")
}

func collapse(text string) string {
	return strings.TrimSpace(pdfSpaces.ReplaceAllString(text, " "))
}
