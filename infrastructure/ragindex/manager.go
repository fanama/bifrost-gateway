package ragindex

import (
	"context"
	"sync"
	"time"

	domainrag "bridge-gateway/domain/rag"
)

// Manager owns one lazily built HNSW graph per project. Builds are serialized
// per project, searches never block on a build, and every failure leaves the
// exact scan in charge.
type Manager struct {
	chunks   domainrag.ChunkRepository
	now      func() time.Time
	mu       sync.Mutex
	projects map[string]*project
	wg       sync.WaitGroup
}

type project struct {
	// buildMu serializes rebuilds with ingestion so the upload's chunks are
	// visible before the write returns.
	buildMu sync.Mutex

	mu         sync.RWMutex
	building   bool // guarded by Manager.mu
	graph      *graph
	model      string
	dim        int
	status     domainrag.IndexStatus
	tombstones map[string]struct{}
}

// NewManager wires the index to the chunk repository it rebuilds from.
func NewManager(chunks domainrag.ChunkRepository) *Manager {
	return &Manager{
		chunks:   chunks,
		now:      time.Now,
		projects: map[string]*project{},
	}
}

func (m *Manager) project(projectID string) *project {
	m.mu.Lock()
	defer m.mu.Unlock()
	p := m.projects[projectID]
	if p == nil {
		p = &project{
			tombstones: map[string]struct{}{},
			status: domainrag.IndexStatus{
				ProjectID: projectID,
				State:     domainrag.IndexCold,
			},
		}
		m.projects[projectID] = p
	}
	return p
}

// Search answers from the graph when it is ready and matches the query space.
// Otherwise it starts a background rebuild and lets the caller use exact search.
func (m *Manager) Search(
	ctx context.Context,
	projectID, model string,
	query []float32,
	k int,
	opts domainrag.IndexOptions,
) ([]domainrag.RetrievedChunk, domainrag.IndexStatus, bool) {
	p := m.project(projectID)

	p.mu.RLock()
	ready := p.graph != nil && p.model == model && p.dim == len(query) &&
		paramsEqual(p.status.Params, opts)
	if !ready {
		p.mu.RUnlock()
		m.startRebuild(projectID, model, opts)
		return nil, m.Status(ctx, projectID), false
	}
	hits := p.hitsLocked(p.graph.search(query, k, opts.EfSearch), query)
	status := p.status
	status.State = domainrag.IndexReady
	status.Nodes = len(p.graph.nodes)
	status.Tombstones = len(p.tombstones)
	status.TombstoneRatio = tombstoneRatio(status.Tombstones, status.Nodes)
	p.mu.RUnlock()
	return hits, status, true
}

// Upsert applies newly persisted chunks synchronously. If no compatible graph
// exists, it rebuilds from storage first, so the next query sees the document.
func (m *Manager) Upsert(
	ctx context.Context,
	projectID, model string,
	chunks []domainrag.Chunk,
	opts domainrag.IndexOptions,
) error {
	if len(chunks) == 0 {
		return nil
	}
	p := m.project(projectID)
	p.buildMu.Lock()
	defer p.buildMu.Unlock()

	p.mu.RLock()
	ready := p.graph != nil && p.model == model && p.dim == len(chunks[0].Vector) &&
		paramsEqual(p.status.Params, opts)
	p.mu.RUnlock()
	if !ready {
		return m.rebuildLocked(ctx, p, projectID, model, opts)
	}

	p.mu.Lock()
	for i := range chunks {
		if len(chunks[i].Vector) != p.dim {
			continue
		}
		p.graph.insert(chunks[i])
		delete(p.tombstones, chunks[i].ID)
	}
	memory := estimateMemory(p.graph)
	if opts.MemoryLimitBytes > 0 && memory > opts.MemoryLimitBytes {
		p.dropLocked(memory)
		p.mu.Unlock()
		return nil
	}
	p.markReadyLocked(memory, opts)
	p.mu.Unlock()
	return nil
}

// DeleteDocument tombstones a document's nodes immediately and schedules a
// rebuild once tombstones cross the configured ratio.
func (m *Manager) DeleteDocument(
	_ context.Context,
	projectID, documentID string,
	opts domainrag.IndexOptions,
) error {
	p := m.project(projectID)
	p.buildMu.Lock()
	defer p.buildMu.Unlock()
	p.mu.Lock()
	if p.graph == nil {
		p.mu.Unlock()
		return nil
	}
	model := p.model
	for i := range p.graph.nodes {
		if p.graph.nodes[i].chunk.DocumentID == documentID {
			p.tombstones[p.graph.nodes[i].chunk.ID] = struct{}{}
		}
	}
	p.status.Nodes = len(p.graph.nodes)
	p.status.Tombstones = len(p.tombstones)
	p.status.TombstoneRatio = tombstoneRatio(len(p.tombstones), len(p.graph.nodes))
	rebuild := p.status.TombstoneRatio >= opts.TombstoneRebuildRatio
	p.mu.Unlock()

	if rebuild {
		m.startRebuild(projectID, model, opts)
	}
	return nil
}

// Rebuild starts a background rebuild. Callers poll Status for progress.
func (m *Manager) Rebuild(_ context.Context, projectID, model string, opts domainrag.IndexOptions) error {
	m.startRebuild(projectID, model, opts)
	return nil
}

// Wait blocks until every background rebuild has finished. Call it before
// closing the database so a rebuild cannot outlive storage.
func (m *Manager) Wait() {
	m.wg.Wait()
}

// Status returns a consistent snapshot of a project's index.
func (m *Manager) Status(_ context.Context, projectID string) domainrag.IndexStatus {
	p := m.project(projectID)
	p.mu.RLock()
	defer p.mu.RUnlock()

	status := p.status
	status.ProjectID = projectID
	if p.graph != nil {
		status.Nodes = len(p.graph.nodes)
	}
	status.Tombstones = len(p.tombstones)
	status.TombstoneRatio = tombstoneRatio(status.Tombstones, status.Nodes)
	status.Degraded = status.State == domainrag.IndexDegraded || status.State == domainrag.IndexOverCapacity
	return status
}
