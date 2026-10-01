package infrastructure

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
)

// TableNames centralise le schema physique. Le domaine ignore completement ces
// noms : ils appartiennent a l'adaptateur SQLite.
const (
	TableProjects     = "projects"
	TableConfigs      = "chat_configs"
	TableAPIKeys      = "api_keys"
	TableProviders    = "providers"
	TableCatalogModel = "catalog_models"
)

// OpenDB ouvre le fichier SQLite avec le pragmas adaptes au contexte : le
// driver modernc.org/sqlite est pur Go, donc aucun CGO n'est requis (build
// linux/amd64 et image scratch continus de fonctionner).
//
// busy_timeout + WAL evitent les erreurs "database is locked" lors des
// lectures concurrentes du serveur HTTP, et foreign_keys active les contraintes
// declarees dans le schema.
func OpenDB(path string) (*sql.DB, error) {
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("create database directory: %w", err)
		}
	}

	dsn := path + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite %s: %w", path, err)
	}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping sqlite %s: %w", path, err)
	}
	return db, nil
}

// Migrate cree les tables et les index du schema. L'operation est idempotente
// (IF NOT EXISTS), donc appelee a chaque demarrage.
func Migrate(ctx context.Context, db *sql.DB) error {
	for _, table := range []string{
		TableProjects, TableConfigs, TableAPIKeys, TableProviders, TableCatalogModel,
	} {
		if err := ensureTable(db, table); err != nil {
			return err
		}
	}

	// Index sur les champs reellement filtres : resolutions de cle par digest,
	// listages par projet, config active d'un projet, catalogue par provider.
	indexes := []struct {
		name  string
		table string
		paths []string
	}{
		{"idx_api_keys_project", TableAPIKeys, []string{"$.project_id"}},
		{"idx_api_keys_digest", TableAPIKeys, []string{"$.digest"}},
		{"idx_chat_configs_project", TableConfigs, []string{"$.project_id"}},
		{"idx_chat_configs_project_active", TableConfigs, []string{"$.project_id", "$.active"}},
		{"idx_catalog_models_provider", TableCatalogModel, []string{"$.provider"}},
		{"idx_providers_name", TableProviders, []string{"$.name"}},
	}
	for _, idx := range indexes {
		if err := createJSONIndex(db, idx.name, idx.table, idx.paths...); err != nil {
			return err
		}
	}
	return nil
}
