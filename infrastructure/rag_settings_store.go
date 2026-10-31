package infrastructure

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	domainrag "bridge-gateway/domain/rag"
)

// RAGSettingsStore persists one Settings document per project.
type RAGSettingsStore struct {
	db *sql.DB
}

// NewRAGSettingsStore opens the RAG settings table.
func NewRAGSettingsStore(db *sql.DB) *RAGSettingsStore {
	return &RAGSettingsStore{db: db}
}

// Get returns the project's settings or domainrag.ErrSettingsNotFound.
func (s *RAGSettingsStore) Get(ctx context.Context, projectID string) (*domainrag.Settings, error) {
	var payload []byte
	err := s.db.QueryRowContext(ctx,
		"SELECT payload FROM "+TableRAGSettings+" WHERE id = ?", projectID).Scan(&payload)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domainrag.ErrSettingsNotFound
	}
	if err != nil {
		return nil, err
	}
	var settings domainrag.Settings
	if err := json.Unmarshal(payload, &settings); err != nil {
		return nil, err
	}
	return &settings, nil
}

// Save upserts the project's settings in a single statement.
func (s *RAGSettingsStore) Save(ctx context.Context, settings *domainrag.Settings) error {
	payload, err := json.Marshal(settings)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx,
		"INSERT INTO "+TableRAGSettings+"(id, payload) VALUES(?, ?) "+
			"ON CONFLICT(id) DO UPDATE SET payload = excluded.payload",
		settings.ProjectID, payload)
	return err
}

var _ domainrag.SettingsRepository = (*RAGSettingsStore)(nil)
