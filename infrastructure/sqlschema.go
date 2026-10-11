package infrastructure

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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

// ensureTable cree la table d'une collection si elle n'existe pas encore. Le
// payload est stocke en JSON : le schema reste stable quelle que soit l'evolution
// des entites du domaine, et les colonnes extraites servent aux index.
func ensureTable(db *sql.DB, table string) error {
	ddl := fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s (id TEXT PRIMARY KEY, payload TEXT NOT NULL)", quoteIdent(table))
	if _, err := db.Exec(ddl); err != nil {
		return fmt.Errorf("create table %s: %w", table, err)
	}
	return nil
}

// createJSONIndex declare un index sur une ou plusieurs colonnes extraites du
// payload JSON, ce qui evite de dupliquer chaque colonne metier dans le schema.
func createJSONIndex(db *sql.DB, name, table string, jsonPaths ...string) error {
	targets := make([]string, len(jsonPaths))
	for i, path := range jsonPaths {
		targets[i] = fmt.Sprintf("json_extract(payload, '%s')", path)
	}
	ddl := fmt.Sprintf("CREATE INDEX IF NOT EXISTS %s ON %s (%s)",
		quoteIdent(name), quoteIdent(table), strings.Join(targets, ", "))
	if _, err := db.Exec(ddl); err != nil {
		return fmt.Errorf("create index %s: %w", name, err)
	}
	return nil
}

func quoteIdent(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}
