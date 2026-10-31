package infrastructure

import (
	"context"
	"database/sql"
	"sort"

	domainrag "bridge-gateway/domain/rag"
)

// RAGDocumentStore implements domainrag.DocumentRepository on SQLite.
type RAGDocumentStore struct {
	*Adapter[domainrag.Document]
}

// NewRAGDocumentStore opens the document table.
func NewRAGDocumentStore(db *sql.DB) *RAGDocumentStore {
	inner := NewSQLStore[domainrag.Document](db, TableRAGDocuments, func(d *domainrag.Document) string { return d.ID })
	return &RAGDocumentStore{Adapter: NewAdapter(
		inner,
		func(d *domainrag.Document) string { return d.ID },
		domainrag.ErrDocumentNotFound,
		nil,
	)}
}

// ListByProject returns a project's documents, newest first.
func (s *RAGDocumentStore) ListByProject(ctx context.Context, projectID string) ([]domainrag.Document, error) {
	items, err := s.Store().WhereSQL(ctx, `json_extract(payload, '$.project_id') = ?`, projectID)
	if err != nil {
		return nil, err
	}
	sort.SliceStable(items, func(i, j int) bool {
		if !items[i].CreatedAt.Equal(items[j].CreatedAt) {
			return items[i].CreatedAt.After(items[j].CreatedAt)
		}
		return items[i].ID < items[j].ID
	})
	return items, nil
}

var _ domainrag.DocumentRepository = (*RAGDocumentStore)(nil)
