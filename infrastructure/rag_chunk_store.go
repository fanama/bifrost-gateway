package infrastructure

import (
	"context"
	"database/sql"
	"encoding/json"
	"sort"

	domainrag "bridge-gateway/domain/rag"
)

// RAGChunkStore implements domainrag.ChunkRepository on SQLite. Chunks share
// the generic two-column layout, but replacement is transaction-scoped so a
// document is never observed with a partial chunk set.
type RAGChunkStore struct {
	db *sql.DB
}

// NewRAGChunkStore opens the chunk table.
func NewRAGChunkStore(db *sql.DB) *RAGChunkStore {
	return &RAGChunkStore{db: db}
}

// ReplaceForDocument atomically deletes the document's previous chunks and
// inserts the new ones.
func (s *RAGChunkStore) ReplaceForDocument(ctx context.Context, projectID, documentID string, chunks []domainrag.Chunk) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := deleteChunksTx(ctx, tx, projectID, documentID); err != nil {
		tx.Rollback()
		return err
	}
	for i := range chunks {
		payload, err := json.Marshal(&chunks[i])
		if err != nil {
			tx.Rollback()
			return err
		}
		_, err = tx.ExecContext(ctx,
			"INSERT INTO "+TableRAGChunks+"(id, payload) VALUES(?, ?)", chunks[i].ID, payload)
		if err != nil {
			tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

func deleteChunksTx(ctx context.Context, tx *sql.Tx, projectID, documentID string) error {
	_, err := tx.ExecContext(ctx,
		"DELETE FROM "+TableRAGChunks+
			" WHERE json_extract(payload, '$.project_id') = ? AND json_extract(payload, '$.document_id') = ?",
		projectID, documentID)
	return err
}

// ListByProject returns every passage of a project in document order.
func (s *RAGChunkStore) ListByProject(ctx context.Context, projectID string) ([]domainrag.Chunk, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT payload FROM "+TableRAGChunks+" WHERE json_extract(payload, '$.project_id') = ?", projectID)
	if err != nil {
		return nil, err
	}
	chunks, err := scanAll[domainrag.Chunk](rows)
	if err != nil {
		return nil, err
	}
	sort.SliceStable(chunks, func(i, j int) bool {
		if chunks[i].DocumentID != chunks[j].DocumentID {
			return chunks[i].DocumentID < chunks[j].DocumentID
		}
		return chunks[i].Ordinal < chunks[j].Ordinal
	})
	return chunks, nil
}

// DeleteByDocument removes every passage of a document.
func (s *RAGChunkStore) DeleteByDocument(ctx context.Context, projectID, documentID string) error {
	_, err := s.db.ExecContext(ctx,
		"DELETE FROM "+TableRAGChunks+
			" WHERE json_extract(payload, '$.project_id') = ? AND json_extract(payload, '$.document_id') = ?",
		projectID, documentID)
	return err
}

var _ domainrag.ChunkRepository = (*RAGChunkStore)(nil)
