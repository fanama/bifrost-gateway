package rag

import (
	"context"
	"strings"
	"time"

	"bridge-gateway/domain/embedding"
	"bridge-gateway/domain/errs"
)

// MaxSystemPromptChars bounds the extra RAG instruction stored per project.
const MaxSystemPromptChars = 8000

// HNSW defaults and bounds. The defaults are provisional until the manual
// recall benchmark in hnsw-spec.md is run.
const (
	DefaultHNSWM                     = 16
	DefaultHNSWEfConstruction        = 200
	DefaultHNSWEfSearch              = 64
	DefaultHNSWTombstoneRebuildRatio = 0.20
	DefaultHNSWMemoryLimitMB         = 64

	MinHNSWM                     = 16
	MaxHNSWM                     = 64
	MinHNSWEfConstruction        = 40
	MaxHNSWEfConstruction        = 2000
	MinHNSWEfSearch              = 16
	MaxHNSWEfSearch              = 500
	MinHNSWTombstoneRebuildRatio = 0.05
	MaxHNSWTombstoneRebuildRatio = 0.50
	MinHNSWMemoryLimitMB         = 8
	MaxHNSWMemoryLimitMB         = 1024
)

// Settings are the per-project RAG parameters used as defaults by ingestion
// and query. A missing row means DefaultSettings.
type Settings struct {
	ProjectID      string `json:"project_id"`
	EmbeddingModel string `json:"embedding_model"`
	TopK           int    `json:"top_k"`
	ChunkSize      int    `json:"chunk_size"`
	ChunkOverlap   int    `json:"chunk_overlap"`
	SystemPrompt   string `json:"system_prompt,omitempty"`
	// HNSW parameters. Zero values on a stored record predate the index and
	// are filled by Normalize, so no migration is required.
	HNSWM                     int       `json:"hnsw_m"`
	HNSWEfConstruction        int       `json:"hnsw_ef_construction"`
	HNSWEfSearch              int       `json:"hnsw_ef_search"`
	HNSWTombstoneRebuildRatio float64   `json:"hnsw_tombstone_rebuild_ratio"`
	HNSWMemoryLimitMB         int       `json:"hnsw_memory_limit_mb"`
	HNSWRecallSampleRate      float64   `json:"hnsw_recall_sample_rate"`
	UpdatedAt                 time.Time `json:"updated_at,omitempty"`
}

// DefaultSettings returns the offline-friendly defaults for a project.
func DefaultSettings(projectID string) Settings {
	return Settings{
		ProjectID:                 strings.TrimSpace(projectID),
		EmbeddingModel:            embedding.ModelLocalEmbedding,
		TopK:                      DefaultTopK,
		ChunkSize:                 DefaultChunkSize,
		ChunkOverlap:              DefaultChunkOverlap,
		HNSWM:                     DefaultHNSWM,
		HNSWEfConstruction:        DefaultHNSWEfConstruction,
		HNSWEfSearch:              DefaultHNSWEfSearch,
		HNSWTombstoneRebuildRatio: DefaultHNSWTombstoneRebuildRatio,
		HNSWMemoryLimitMB:         DefaultHNSWMemoryLimitMB,
	}
}

// Normalize trims text fields and fills an empty embedding model.
func (s *Settings) Normalize() {
	s.ProjectID = strings.TrimSpace(s.ProjectID)
	s.EmbeddingModel = strings.TrimSpace(s.EmbeddingModel)
	s.SystemPrompt = strings.TrimSpace(s.SystemPrompt)
	if s.EmbeddingModel == "" {
		s.EmbeddingModel = embedding.ModelLocalEmbedding
	}
	if s.HNSWM == 0 {
		s.HNSWM = DefaultHNSWM
	}
	if s.HNSWEfConstruction == 0 {
		s.HNSWEfConstruction = DefaultHNSWEfConstruction
	}
	if s.HNSWEfSearch == 0 {
		s.HNSWEfSearch = DefaultHNSWEfSearch
	}
	if s.HNSWTombstoneRebuildRatio == 0 {
		s.HNSWTombstoneRebuildRatio = DefaultHNSWTombstoneRebuildRatio
	}
	if s.HNSWMemoryLimitMB == 0 {
		s.HNSWMemoryLimitMB = DefaultHNSWMemoryLimitMB
	}
}

// Validate enforces the bounds shared by the UI and the API.
func (s Settings) Validate() error {
	if s.ProjectID == "" {
		return &errs.ValidationError{Fields: []string{"project_id"}}
	}
	if s.TopK < 1 || s.TopK > MaxTopK {
		return &errs.ValidationError{Fields: []string{"top_k"}, Reason: "doit etre compris entre 1 et 20"}
	}
	if s.ChunkSize < 1 {
		return &errs.ValidationError{Fields: []string{"chunk_size"}, Reason: "doit etre superieur a 0"}
	}
	if s.ChunkOverlap < 0 || s.ChunkOverlap >= s.ChunkSize {
		return &errs.ValidationError{Fields: []string{"chunk_overlap"}, Reason: "doit etre compris entre 0 et chunk_size - 1"}
	}
	if len([]rune(s.SystemPrompt)) > MaxSystemPromptChars {
		return &errs.ValidationError{Fields: []string{"system_prompt"}, Reason: "depasse la longueur maximale"}
	}
	if s.HNSWM < MinHNSWM || s.HNSWM > MaxHNSWM {
		return &errs.ValidationError{Fields: []string{"hnsw_m"}, Reason: "doit etre compris entre 16 et 64"}
	}
	if s.HNSWEfConstruction < MinHNSWEfConstruction || s.HNSWEfConstruction > MaxHNSWEfConstruction || s.HNSWEfConstruction < s.HNSWM {
		return &errs.ValidationError{Fields: []string{"hnsw_ef_construction"}, Reason: "doit etre compris entre 40 et 2000 et superieur ou egal a hnsw_m"}
	}
	if s.HNSWEfSearch < MinHNSWEfSearch || s.HNSWEfSearch > MaxHNSWEfSearch {
		return &errs.ValidationError{Fields: []string{"hnsw_ef_search"}, Reason: "doit etre compris entre 16 et 500"}
	}
	if s.HNSWTombstoneRebuildRatio < MinHNSWTombstoneRebuildRatio || s.HNSWTombstoneRebuildRatio > MaxHNSWTombstoneRebuildRatio {
		return &errs.ValidationError{Fields: []string{"hnsw_tombstone_rebuild_ratio"}, Reason: "doit etre compris entre 0.05 et 0.50"}
	}
	if s.HNSWMemoryLimitMB < MinHNSWMemoryLimitMB || s.HNSWMemoryLimitMB > MaxHNSWMemoryLimitMB {
		return &errs.ValidationError{Fields: []string{"hnsw_memory_limit_mb"}, Reason: "doit etre compris entre 8 et 1024"}
	}
	if s.HNSWRecallSampleRate < 0 || s.HNSWRecallSampleRate > 1 {
		return &errs.ValidationError{Fields: []string{"hnsw_recall_sample_rate"}, Reason: "doit etre compris entre 0 et 1"}
	}
	return nil
}

// SettingsRepository persists the RAG parameters of a project.
type SettingsRepository interface {
	Get(ctx context.Context, projectID string) (*Settings, error)
	Save(ctx context.Context, settings *Settings) error
}

// ErrSettingsNotFound means the project never saved RAG parameters.
var ErrSettingsNotFound = errs.ErrRAGSettingsNotFound
