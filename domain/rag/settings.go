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

// Settings are the per-project RAG parameters used as defaults by ingestion
// and query. A missing row means DefaultSettings.
type Settings struct {
	ProjectID      string    `json:"project_id"`
	EmbeddingModel string    `json:"embedding_model"`
	TopK           int       `json:"top_k"`
	ChunkSize      int       `json:"chunk_size"`
	ChunkOverlap   int       `json:"chunk_overlap"`
	SystemPrompt   string    `json:"system_prompt,omitempty"`
	UpdatedAt      time.Time `json:"updated_at,omitempty"`
}

// DefaultSettings returns the offline-friendly defaults for a project.
func DefaultSettings(projectID string) Settings {
	return Settings{
		ProjectID:      strings.TrimSpace(projectID),
		EmbeddingModel: embedding.ModelLocalEmbedding,
		TopK:           DefaultTopK,
		ChunkSize:      DefaultChunkSize,
		ChunkOverlap:   DefaultChunkOverlap,
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
	return nil
}

// SettingsRepository persists the RAG parameters of a project.
type SettingsRepository interface {
	Get(ctx context.Context, projectID string) (*Settings, error)
	Save(ctx context.Context, settings *Settings) error
}

// ErrSettingsNotFound means the project never saved RAG parameters.
var ErrSettingsNotFound = errs.ErrRAGSettingsNotFound
