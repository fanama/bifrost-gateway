package ragindex

import (
	"context"
	"log"
	"time"

	domainrag "bridge-gateway/domain/rag"
)

// startRebuild launches at most one background build per project.
func (m *Manager) startRebuild(projectID, model string, opts domainrag.IndexOptions) {
	p := m.project(projectID)
	m.mu.Lock()
	if p.building {
		m.mu.Unlock()
		return
	}
	p.building = true
	m.mu.Unlock()

	p.mu.Lock()
	p.status.State = domainrag.IndexBuilding
	p.status.LastError = ""
	p.mu.Unlock()

	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		defer func() {
			m.mu.Lock()
			p.building = false
			m.mu.Unlock()
		}()
		_ = m.rebuildNow(p, projectID, model, opts)
	}()
}

func (m *Manager) rebuildNow(p *project, projectID, model string, opts domainrag.IndexOptions) error {
	p.buildMu.Lock()
	defer p.buildMu.Unlock()
	return m.rebuildLocked(context.Background(), p, projectID, model, opts)
}

// rebuildLocked loads persisted chunks, builds a fresh graph off to the side
// and swaps it in. The caller must hold p.buildMu.
func (m *Manager) rebuildLocked(
	ctx context.Context,
	p *project,
	projectID, model string,
	opts domainrag.IndexOptions,
) error {
	started := m.now()
	chunks, err := m.chunks.ListByProject(ctx, projectID)
	if err != nil {
		p.fail(err)
		return err
	}
	g := newGraph(model, opts.M, opts.EfConstruction, 1)
	g.addChunks(chunks, model)
	memory := estimateMemory(g)
	if opts.MemoryLimitBytes > 0 && memory > opts.MemoryLimitBytes {
		p.overCapacity(memory, opts)
		return nil
	}
	p.swap(g, model, memory, opts, m.now().Sub(started))
	return nil
}

func (p *project) swap(g *graph, model string, memory int64, opts domainrag.IndexOptions, duration time.Duration) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.graph = g
	p.model = model
	p.dim = g.width()
	p.tombstones = map[string]struct{}{}
	p.status = domainrag.IndexStatus{
		ProjectID:        p.status.ProjectID,
		State:            domainrag.IndexReady,
		EmbeddingModel:   model,
		Dimensions:       g.width(),
		Nodes:            len(g.nodes),
		MemoryBytes:      memory,
		MemoryLimitBytes: opts.MemoryLimitBytes,
		BuildDurationMS:  duration.Milliseconds(),
		LastBuiltAt:      time.Now().UTC(),
		Params:           paramsFromOptions(opts),
	}
}

func (p *project) markReadyLocked(memory int64, opts domainrag.IndexOptions) {
	p.status.State = domainrag.IndexReady
	p.status.Degraded = false
	p.status.EmbeddingModel = p.model
	p.status.Dimensions = p.dim
	p.status.Nodes = len(p.graph.nodes)
	p.status.Tombstones = len(p.tombstones)
	p.status.TombstoneRatio = tombstoneRatio(len(p.tombstones), len(p.graph.nodes))
	p.status.MemoryBytes = memory
	p.status.MemoryLimitBytes = opts.MemoryLimitBytes
	p.status.Params = paramsFromOptions(opts)
	p.status.LastError = ""
}

func (p *project) dropLocked(memory int64) {
	log.Printf("RAG index over capacity: project=%s memory=%d limit=%d", p.status.ProjectID, memory, p.status.MemoryLimitBytes)
	p.graph = nil
	p.model = ""
	p.dim = 0
	p.tombstones = map[string]struct{}{}
	p.status.State = domainrag.IndexOverCapacity
	p.status.Nodes = 0
	p.status.Tombstones = 0
	p.status.TombstoneRatio = 0
	p.status.MemoryBytes = memory
	p.status.LastError = "index memory limit exceeded"
}

func (p *project) overCapacity(memory int64, opts domainrag.IndexOptions) {
	p.mu.Lock()
	defer p.mu.Unlock()
	log.Printf("RAG index over capacity: project=%s memory=%d limit=%d", p.status.ProjectID, memory, opts.MemoryLimitBytes)
	// The old graph may not contain the chunks that pushed the estimate over
	// the cap, so drop it and let the exact scan answer until a rebuild fits.
	p.graph = nil
	p.model = ""
	p.dim = 0
	p.tombstones = map[string]struct{}{}
	p.status.State = domainrag.IndexOverCapacity
	p.status.MemoryBytes = memory
	p.status.MemoryLimitBytes = opts.MemoryLimitBytes
	p.status.Params = paramsFromOptions(opts)
	p.status.LastError = "index memory limit exceeded"
}

func (p *project) fail(err error) {
	log.Printf("RAG index build failed: %v", err)
	p.mu.Lock()
	defer p.mu.Unlock()
	p.status.State = domainrag.IndexDegraded
	p.status.LastError = err.Error()
}

// hitsLocked maps graph node indexes to retrieved chunks, dropping tombstones.
// The caller must hold at least a read lock.
func (p *project) hitsLocked(ids []int32, query []float32) []domainrag.RetrievedChunk {
	hits := make([]domainrag.RetrievedChunk, 0, len(ids))
	for _, id := range ids {
		node := &p.graph.nodes[id]
		if _, deleted := p.tombstones[node.chunk.ID]; deleted {
			continue
		}
		hits = append(hits, domainrag.RetrievedChunk{
			Chunk: node.chunk,
			Score: domainrag.CosineSimilarity(query, node.chunk.Vector),
		})
	}
	return hits
}

func estimateMemory(g *graph) int64 {
	var total int64
	for i := range g.nodes {
		node := &g.nodes[i]
		total += int64(len(node.vector))*4 + int64(len(node.chunk.Text)) + 128
		for _, links := range node.links {
			total += int64(len(links)) * 4
		}
	}
	return total
}

func paramsFromOptions(opts domainrag.IndexOptions) domainrag.IndexParams {
	return domainrag.IndexParams{
		M:              opts.M,
		EfConstruction: opts.EfConstruction,
		EfSearch:       opts.EfSearch,
	}
}

func paramsEqual(params domainrag.IndexParams, opts domainrag.IndexOptions) bool {
	return params.M == opts.M && params.EfConstruction == opts.EfConstruction
}

func tombstoneRatio(tombstones, nodes int) float64 {
	if nodes == 0 {
		return 0
	}
	return float64(tombstones) / float64(nodes)
}
