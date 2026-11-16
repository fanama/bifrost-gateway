package ragindex

import (
	"context"
	"fmt"
	"math/rand"
	"strconv"
	"sync"
	"testing"
	"time"

	domainrag "bridge-gateway/domain/rag"
)

type fakeRepo struct {
	mu     sync.Mutex
	chunks []domainrag.Chunk
}

func (f *fakeRepo) set(chunks []domainrag.Chunk) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.chunks = chunks
}

func (f *fakeRepo) ListByProject(context.Context, string) ([]domainrag.Chunk, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]domainrag.Chunk, len(f.chunks))
	copy(out, f.chunks)
	return out, nil
}

func (f *fakeRepo) ReplaceForDocument(context.Context, string, string, []domainrag.Chunk) error {
	return nil
}

func (f *fakeRepo) DeleteByDocument(context.Context, string, string) error { return nil }

func randomVector(rng *rand.Rand, dim int) []float32 {
	out := make([]float32, dim)
	for i := range out {
		out[i] = float32(rng.NormFloat64())
	}
	return out
}

func testChunks(rng *rand.Rand, count int) []domainrag.Chunk {
	chunks := make([]domainrag.Chunk, count)
	for i := range chunks {
		chunks[i] = domainrag.Chunk{
			ID:             "d" + strconv.Itoa(i) + ":0",
			DocumentID:     "d" + strconv.Itoa(i),
			FileName:       "doc.txt",
			EmbeddingModel: "model",
			Text:           "passage " + strconv.Itoa(i),
			Vector:         randomVector(rng, 16),
		}
	}
	return chunks
}

func testOptions() domainrag.IndexOptions {
	return domainrag.DefaultSettings("p").IndexOptions()
}

// The graph must preserve the exact top-k for the target corpus size. This is
// the manual 99% recall target turned into a deterministic check.
func TestGraphRecallMatchesExactSearch(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	chunks := testChunks(rng, 400)
	graph := newGraph("model", 16, 200, 1)
	graph.addChunks(chunks, "model")

	found, total := 0, 0
	for i := 0; i < 25; i++ {
		query := randomVector(rng, 16)
		exact := domainrag.RankChunks(query, chunks, 5)
		ids := map[string]bool{}
		for _, id := range graph.search(query, 5, 128) {
			ids[graph.nodes[id].chunk.ID] = true
		}
		for _, hit := range exact {
			if ids[hit.Chunk.ID] {
				found++
			}
			total++
		}
	}
	recall := float64(found) / float64(total)
	if recall < 0.99 {
		t.Fatalf("recall %.3f below the 0.99 target", recall)
	}
}

func TestGraphIgnoresOtherModelsAndZeroVectors(t *testing.T) {
	graph := newGraph("model", 16, 200, 1)
	graph.addChunks([]domainrag.Chunk{
		{ID: "a", EmbeddingModel: "model", Vector: []float32{1, 0}},
		{ID: "b", EmbeddingModel: "other", Vector: []float32{0, 1}},
		{ID: "c", EmbeddingModel: "model", Vector: []float32{0, 0}},
	}, "model")
	if len(graph.nodes) != 1 || graph.nodes[0].chunk.ID != "a" {
		t.Fatalf("unexpected nodes: %#v", graph.nodes)
	}
}

func TestManagerColdSearchBuildsInBackground(t *testing.T) {
	repo := &fakeRepo{}
	repo.set(testChunks(rand.New(rand.NewSource(1)), 20))
	manager := NewManager(repo)

	query := randomVector(rand.New(rand.NewSource(9)), 16)
	_, status, ok := manager.Search(context.Background(), "p1", "model", query, 3, testOptions())
	if ok {
		t.Fatal("expected a cold index to defer to the exact scan")
	}
	if status.State != domainrag.IndexCold && status.State != domainrag.IndexBuilding {
		t.Fatalf("unexpected cold state: %#v", status)
	}
	waitReady(t, manager, "p1", "model", query)
}

func TestManagerUpsertAndDeleteAreVisible(t *testing.T) {
	ctx := context.Background()
	repo := &fakeRepo{}
	manager := NewManager(repo)
	chunks := testChunks(rand.New(rand.NewSource(2)), 3)
	repo.set(chunks)

	if err := manager.Upsert(ctx, "p1", "model", chunks, testOptions()); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	hits, status, ok := manager.Search(ctx, "p1", "model", chunks[0].Vector, 2, testOptions())
	if !ok || len(hits) == 0 || status.State != domainrag.IndexReady {
		t.Fatalf("index not ready after upsert: ok=%v status=%#v hits=%d", ok, status, len(hits))
	}

	repo.set(chunks[1:])
	if err := manager.DeleteDocument(ctx, "p1", chunks[0].DocumentID, testOptions()); err != nil {
		t.Fatalf("delete: %v", err)
	}
	hits, _, _ = manager.Search(ctx, "p1", "model", chunks[0].Vector, 3, testOptions())
	for _, hit := range hits {
		if hit.Chunk.DocumentID == chunks[0].DocumentID {
			t.Fatalf("deleted document still returned: %#v", hit)
		}
	}
}

func TestManagerModelMismatchFallsBack(t *testing.T) {
	ctx := context.Background()
	repo := &fakeRepo{}
	manager := NewManager(repo)
	chunks := testChunks(rand.New(rand.NewSource(3)), 5)
	repo.set(chunks)
	if err := manager.Upsert(ctx, "p1", "model", chunks, testOptions()); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	if _, _, ok := manager.Search(ctx, "p1", "other-model", chunks[0].Vector, 2, testOptions()); ok {
		t.Fatal("a model mismatch must defer to the exact scan")
	}
}

func TestManagerOverCapacityUsesExactScan(t *testing.T) {
	ctx := context.Background()
	repo := &fakeRepo{}
	repo.set(testChunks(rand.New(rand.NewSource(4)), 10))
	manager := NewManager(repo)
	opts := testOptions()
	opts.MemoryLimitBytes = 1
	if err := manager.Rebuild(ctx, "p1", "model", opts); err != nil {
		t.Fatalf("rebuild: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		status := manager.Status(ctx, "p1")
		if status.State == domainrag.IndexOverCapacity {
			_, _, ok := manager.Search(ctx, "p1", "model", []float32{1, 0}, 2, opts)
			if ok {
				t.Fatal("over-capacity index must not answer searches")
			}
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("index never reported over_capacity")
}

func waitReady(t *testing.T, manager *Manager, projectID, model string, query []float32) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		_, status, ok := manager.Search(context.Background(), projectID, model, query, 2, testOptions())
		if ok {
			return
		}
		if status.State == domainrag.IndexDegraded || status.State == domainrag.IndexOverCapacity {
			t.Fatalf("index failed to build: %#v", status)
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal(fmt.Sprintf("index for %s never became ready", projectID))
}
