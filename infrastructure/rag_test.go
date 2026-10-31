package infrastructure

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	domainrag "bridge-gateway/domain/rag"
)

func TestRAGTextExtractorStripsHTML(t *testing.T) {
	text, err := NewRAGTextExtractor(0).Extract("page.html", []byte("<html><body><h1>Hello</h1><p>world</p></body></html>"))
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if !strings.Contains(text, "Hello") || !strings.Contains(text, "world") || strings.Contains(text, "<") {
		t.Fatalf("unexpected text: %q", text)
	}
}

func TestRAGTextExtractorRejectsBinary(t *testing.T) {
	_, err := NewRAGTextExtractor(0).Extract("scan.pdf", []byte("%PDF-1.7"))
	if err == nil {
		t.Fatal("expected unsupported file type error")
	}
}

func TestRAGStoresRoundTrip(t *testing.T) {
	ctx := context.Background()
	db, err := OpenDB(filepath.Join(t.TempDir(), "rag.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()
	if err := Migrate(ctx, db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	docs := NewRAGDocumentStore(db)
	chunks := NewRAGChunkStore(db)
	now := time.Now().UTC()
	doc := &domainrag.Document{ID: "d1", ProjectID: "p1", FileName: "a.txt", ChunkCount: 2, EmbeddingModel: "local-embedding", CreatedAt: now}
	if err := docs.Create(ctx, doc); err != nil {
		t.Fatalf("create doc: %v", err)
	}
	items := []domainrag.Chunk{
		{ID: "d1:0", DocumentID: "d1", ProjectID: "p1", FileName: "a.txt", Ordinal: 0, Text: "alpha", EmbeddingModel: "local-embedding", Vector: []float32{1, 0}, CreatedAt: now},
		{ID: "d1:1", DocumentID: "d1", ProjectID: "p1", FileName: "a.txt", Ordinal: 1, Text: "beta", EmbeddingModel: "local-embedding", Vector: []float32{0, 1}, CreatedAt: now},
	}
	if err := chunks.ReplaceForDocument(ctx, "p1", "d1", items); err != nil {
		t.Fatalf("replace chunks: %v", err)
	}

	gotDocs, err := docs.ListByProject(ctx, "p1")
	if err != nil || len(gotDocs) != 1 {
		t.Fatalf("list docs: %v %#v", err, gotDocs)
	}
	gotChunks, err := chunks.ListByProject(ctx, "p1")
	if err != nil || len(gotChunks) != 2 {
		t.Fatalf("list chunks: %v %#v", err, gotChunks)
	}

	if err := chunks.DeleteByDocument(ctx, "p1", "d1"); err != nil {
		t.Fatalf("delete chunks: %v", err)
	}
	if err := docs.Delete(ctx, "d1"); err != nil {
		t.Fatalf("delete doc: %v", err)
	}
	remaining, err := chunks.ListByProject(ctx, "p1")
	if err != nil || len(remaining) != 0 {
		t.Fatalf("chunks not deleted: %v %#v", err, remaining)
	}
}

func TestRAGSettingsStoreUpserts(t *testing.T) {
	ctx := context.Background()
	db, err := OpenDB(filepath.Join(t.TempDir(), "rag-settings.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()
	if err := Migrate(ctx, db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	store := NewRAGSettingsStore(db)
	if _, err := store.Get(ctx, "p1"); err != domainrag.ErrSettingsNotFound {
		t.Fatalf("missing settings error = %v", err)
	}
	settings := domainrag.DefaultSettings("p1")
	settings.TopK = 2
	if err := store.Save(ctx, &settings); err != nil {
		t.Fatalf("save: %v", err)
	}
	settings.TopK = 8
	if err := store.Save(ctx, &settings); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	got, err := store.Get(ctx, "p1")
	if err != nil || got.TopK != 8 {
		t.Fatalf("get: %v %#v", err, got)
	}
}
