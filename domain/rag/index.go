package rag

import (
	"context"
	"time"
)

// IndexState describes how a project's vector index is doing. It is exposed in
// API responses so callers know whether retrieval used the graph, the exact
// fallback, or no index at all.
type IndexState string

const (
	IndexDisabled     IndexState = "disabled"
	IndexCold         IndexState = "cold"
	IndexBuilding     IndexState = "building"
	IndexReady        IndexState = "ready"
	IndexDegraded     IndexState = "degraded"
	IndexOverCapacity IndexState = "over_capacity"
)

// IndexParams are the graph-shape parameters used to build an index.
type IndexParams struct {
	M              int `json:"m"`
	EfConstruction int `json:"ef_construction"`
	EfSearch       int `json:"ef_search"`
}

// IndexStatus is the observable state of one project's index.
type IndexStatus struct {
	ProjectID        string      `json:"project_id"`
	State            IndexState  `json:"state"`
	Degraded         bool        `json:"degraded"`
	EmbeddingModel   string      `json:"embedding_model,omitempty"`
	Dimensions       int         `json:"dimensions,omitempty"`
	Nodes            int         `json:"nodes"`
	Tombstones       int         `json:"tombstones"`
	TombstoneRatio   float64     `json:"tombstone_ratio"`
	MemoryBytes      int64       `json:"memory_bytes"`
	MemoryLimitBytes int64       `json:"memory_limit_bytes"`
	BuildDurationMS  int64       `json:"build_duration_ms"`
	LastBuiltAt      time.Time   `json:"last_built_at,omitempty"`
	LastError        string      `json:"last_error,omitempty"`
	Params           IndexParams `json:"params"`
}

// IndexOptions carries the per-project settings the index needs. They are
// derived from Settings so the adapter never depends on the settings model.
type IndexOptions struct {
	M                     int
	EfConstruction        int
	EfSearch              int
	TombstoneRebuildRatio float64
	MemoryLimitBytes      int64
}

// IndexOptions mirrors the persisted HNSW settings into adapter options.
func (s Settings) IndexOptions() IndexOptions {
	return IndexOptions{
		M:                     s.HNSWM,
		EfConstruction:        s.HNSWEfConstruction,
		EfSearch:              s.HNSWEfSearch,
		TombstoneRebuildRatio: s.HNSWTombstoneRebuildRatio,
		MemoryLimitBytes:      int64(s.HNSWMemoryLimitMB) << 20,
	}
}

// ChunkIndex is the retrieval port implemented by the HNSW adapter. ok is false
// whenever the caller must use the exact scan: cold index, background build,
// model/width mismatch, over capacity or an index error. The adapter never
// blocks a search on a build.
type ChunkIndex interface {
	Search(ctx context.Context, projectID, model string, query []float32, k int, opts IndexOptions) ([]RetrievedChunk, IndexStatus, bool)
	Upsert(ctx context.Context, projectID, model string, chunks []Chunk, opts IndexOptions) error
	DeleteDocument(ctx context.Context, projectID, documentID string, opts IndexOptions) error
	Rebuild(ctx context.Context, projectID, model string, opts IndexOptions) error
	Status(ctx context.Context, projectID string) IndexStatus
}

// SearchResult carries retrieved passages plus whether the exact fallback was
// used and which index state produced the answer.
type SearchResult struct {
	Chunks     []RetrievedChunk
	Degraded   bool
	IndexState IndexState
}
