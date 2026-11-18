package rag

import (
	"strings"
	"testing"
)

func TestChunkSentencesKeepsSentencesWhole(t *testing.T) {
	chunks := ChunkSentences("Première phrase. Deuxième phrase ! Troisième phrase ?", 40, 0)
	if len(chunks) != 2 {
		t.Fatalf("chunks = %#v", chunks)
	}
	if !strings.HasSuffix(chunks[0], "!") || !strings.HasSuffix(chunks[1], "?") {
		t.Fatalf("a sentence was cut: %#v", chunks)
	}
	if !strings.Contains(chunks[0], "Première phrase.") {
		t.Fatalf("first sentence lost: %#v", chunks)
	}
}

func TestChunkSentencesKeepsListMarkers(t *testing.T) {
	chunks := ChunkSentences("1. premier point\n2. second point", 200, 0)
	if len(chunks) != 1 || !strings.Contains(chunks[0], "2. second point") {
		t.Fatalf("numbered list split at its markers: %#v", chunks)
	}
}

func TestChunkSentencesEmptyInput(t *testing.T) {
	if chunks := ChunkSentences("   \n\n ", 100, 10); chunks != nil {
		t.Fatalf("expected nil, got %#v", chunks)
	}
}

func TestChunkMarkdownSplitsOnHeadings(t *testing.T) {
	text := "# Intro\n\nalpha beta.\n\n## Détails\n\ngamma delta."
	chunks := ChunkMarkdown(text, 30, 0)
	if len(chunks) != 2 {
		t.Fatalf("chunks = %#v", chunks)
	}
	if !strings.HasPrefix(chunks[0], "# Intro") || !strings.Contains(chunks[0], "alpha beta.") {
		t.Fatalf("first section wrong: %#v", chunks)
	}
	if !strings.HasPrefix(chunks[1], "## Détails") || !strings.Contains(chunks[1], "gamma delta.") {
		t.Fatalf("second section wrong: %#v", chunks)
	}
}

func TestChunkMarkdownIgnoresHeadingsInFences(t *testing.T) {
	text := "# Titre\n\n```\n# pas un titre\n```\n\nfin."
	chunks := ChunkMarkdown(text, 100, 0)
	if len(chunks) != 1 || !strings.Contains(chunks[0], "# pas un titre") {
		t.Fatalf("fenced heading split the section: %#v", chunks)
	}
}

func TestGroupSemanticSplitsOnTopicChange(t *testing.T) {
	units := []string{"alpha one", "alpha two", "beta one"}
	vectors := [][]float32{{1, 0}, {1, 0}, {0, 1}}
	chunks := GroupSemantic(units, vectors, 100)
	if len(chunks) != 2 || chunks[0] != "alpha one alpha two" || chunks[1] != "beta one" {
		t.Fatalf("unexpected chunks: %#v", chunks)
	}
}

func TestGroupSemanticRejectsMisalignedInput(t *testing.T) {
	if chunks := GroupSemantic([]string{"a", "b"}, [][]float32{{1, 0}}, 100); chunks != nil {
		t.Fatalf("expected nil, got %#v", chunks)
	}
}

func TestSettingsValidateChunkingMethod(t *testing.T) {
	settings := DefaultSettings("p1")
	if err := settings.Validate(); err != nil {
		t.Fatalf("default settings invalid: %v", err)
	}
	settings.ChunkingMethod = ChunkingSemantic
	if err := settings.Validate(); err != nil {
		t.Fatalf("semantic should be valid: %v", err)
	}
	settings.ChunkingMethod = "magic"
	if err := settings.Validate(); err == nil {
		t.Fatal("unknown method must be rejected")
	}
}

func TestNormalizeFillsDefaultChunkingMethod(t *testing.T) {
	settings := Settings{ProjectID: "p1"}
	settings.Normalize()
	if settings.ChunkingMethod != ChunkingFixed {
		t.Fatalf("method = %q, want %q", settings.ChunkingMethod, ChunkingFixed)
	}
}
