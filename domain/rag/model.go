// Package rag is the retrieval-augmented generation bounded context.
//
// It owns document/chunk entities, retrieval rules and prompt construction.
// It imports nothing from delivery or persistence: adapters implement the
// ports declared here, and the application layer orchestrates them.
package rag

import (
	"context"
	"strings"
	"time"

	"bridge-gateway/domain/errs"
	"bridge-gateway/domain/shared"
)

// Document is one indexed file for a project.
type Document struct {
	ID             string    `json:"id"`
	ProjectID      string    `json:"project_id"`
	FileName       string    `json:"file_name"`
	ContentType    string    `json:"content_type,omitempty"`
	SizeBytes      int       `json:"size_bytes"`
	ChunkCount     int       `json:"chunk_count"`
	EmbeddingModel string    `json:"embedding_model"`
	CreatedAt      time.Time `json:"created_at"`
}

// Validate enforces the identity rules shared by every adapter.
func (d *Document) Validate() error {
	var missing []string
	if strings.TrimSpace(d.ProjectID) == "" {
		missing = append(missing, "project_id")
	}
	if strings.TrimSpace(d.FileName) == "" {
		missing = append(missing, "file_name")
	}
	if strings.TrimSpace(d.EmbeddingModel) == "" {
		missing = append(missing, "embedding_model")
	}
	if len(missing) > 0 {
		return &errs.ValidationError{Fields: missing}
	}
	return nil
}

// Chunk is one embedded passage of a document. Vector stays out of the HTTP
// representation: it is persistence data, not a client concern.
type Chunk struct {
	ID             string    `json:"id"`
	DocumentID     string    `json:"document_id"`
	ProjectID      string    `json:"project_id"`
	FileName       string    `json:"file_name"`
	Ordinal        int       `json:"ordinal"`
	Text           string    `json:"text"`
	EmbeddingModel string    `json:"embedding_model"`
	Vector         []float32 `json:"vector"`
	CreatedAt      time.Time `json:"created_at"`
}

// RetrievedChunk pairs a chunk with its similarity to the query.
type RetrievedChunk struct {
	Chunk Chunk   `json:"chunk"`
	Score float64 `json:"score"`
}

// Source is the citation returned to the caller for a generated answer.
type Source struct {
	DocumentID string  `json:"document_id"`
	FileName   string  `json:"file_name"`
	ChunkIndex int     `json:"chunk_index"`
	Score      float64 `json:"score"`
}

// Answer is an LLM reply grounded in the retrieved passages.
type Answer struct {
	Answer     string   `json:"answer"`
	Sources    []Source `json:"sources"`
	HasContext bool     `json:"has_context"`
	Model      string   `json:"model,omitempty"`
}

// DocumentRepository is the persistence port for indexed documents.
type DocumentRepository interface {
	shared.UpdatableRepository[Document]
	ListByProject(ctx context.Context, projectID string) ([]Document, error)
}

// ChunkRepository is the persistence port for embedded passages.
type ChunkRepository interface {
	ReplaceForDocument(ctx context.Context, projectID, documentID string, chunks []Chunk) error
	ListByProject(ctx context.Context, projectID string) ([]Chunk, error)
	DeleteByDocument(ctx context.Context, projectID, documentID string) error
}

// TextExtractor turns raw upload bytes into plain text.
type TextExtractor interface {
	Extract(fileName string, content []byte) (string, error)
}

// ErrDocumentNotFound is returned when a document does not belong to the
// caller's project or no longer exists.
var ErrDocumentNotFound = errs.ErrDocumentNotFound
