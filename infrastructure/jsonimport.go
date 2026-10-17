package infrastructure

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"bridge-gateway/domain"
)

// jsonSource est la provenance d'une entite lue depuis les anciens fichiers
// JSON de data/.
type jsonSource[T any] struct {
	table string
	path  string
	idOf  func(*T) string
}

// ImportJSON charge les anciens fichiers JSON (data/*.json) dans les tables
// SQLite et supprime le fichier source une fois l'import termine.
//
// L'import est idempotent et non destructif cote SQLite : une entite dont l'ID
// existe deja dans la table est ignoree (INSERT OR IGNORE), donc relancer
// l'import ne duplique rien et n'ecrase aucune modification faite via l'UI.
// Chaque fichier est renomme en .imported une fois migre, ce qui rend l'operation
// visible et permet a un humain de verifier le contenu avant de le supprimer.
func ImportJSON[T any](ctx context.Context, db *sql.DB, source jsonSource[T]) (int, error) {
	payload, err := os.ReadFile(source.path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("read %s: %w", source.path, err)
	}

	var items []T
	if err := json.Unmarshal(payload, &items); err != nil {
		return 0, fmt.Errorf("decode %s: %w", source.path, err)
	}
	if len(items) == 0 {
		return 0, renameAside(source.path)
	}

	imported, err := insertItems(ctx, db, source.table, items, source.idOf)
	if err != nil {
		return imported, fmt.Errorf("%s: %w", source.path, err)
	}
	if err := renameAside(source.path); err != nil {
		return imported, err
	}
	return imported, nil
}

// insertItems persists the decoded entities with INSERT OR IGNORE, so an
// existing ID is left untouched and the import stays idempotent.
func insertItems[T any](ctx context.Context, db *sql.DB, table string, items []T, idOf func(*T) string) (int, error) {
	imported := 0
	for i := range items {
		item := items[i]
		id := idOf(&item)
		if id == "" {
			continue
		}
		encoded, err := json.Marshal(&item)
		if err != nil {
			return imported, fmt.Errorf("encode item %d: %w", i, err)
		}
		res, err := db.ExecContext(ctx,
			"INSERT OR IGNORE INTO "+quoteIdent(table)+"(id, payload) VALUES(?, ?)", id, encoded)
		if err != nil {
			return imported, fmt.Errorf("insert into %s: %w", table, err)
		}
		affected, err := res.RowsAffected()
		if err != nil {
			return imported, err
		}
		imported += int(affected)
	}
	return imported, nil
}

// renameAside marque le fichier JSON comme migre sans le detruire, pour qu'une
// restauration reste possible.
func renameAside(path string) error {
	target := path + ".imported"
	if _, err := os.Stat(target); err == nil {
		// Deja marque : on retire le fichier importe pour ne pas accumuler.
		return os.Remove(path)
	}
	if err := os.Rename(path, target); err != nil {
		// L'import est deja en base : l'echec de renommage ne doit pas
		// bloquer le demarrage, mais il est signale.
		fmt.Fprintf(os.Stderr, "warning: could not mark %s as imported: %v\n", path, err)
	}
	return nil
}

// ImportLegacyStoreData migre l'ensemble des collections JSON historiques vers
// SQLite. dir est le dossier data/ (celui que derivait --store desormais).
func ImportLegacyStoreData(ctx context.Context, db *sql.DB, dir string) (map[string]int, error) {
	counts := map[string]int{}
	sources := []legacyImporter{
		legacyImporterFor[domain.ChatConfig](ctx, db, dir, "configs", TableConfigs, "configs.json", func(c *domain.ChatConfig) string { return c.ID }),
		legacyImporterFor[domain.Project](ctx, db, dir, "projects", TableProjects, "projects.json", func(p *domain.Project) string { return p.ID }),
		legacyImporterFor[domain.APIKey](ctx, db, dir, "apikeys", TableAPIKeys, "apikeys.json", func(k *domain.APIKey) string { return k.ID }),
		legacyImporterFor[domain.Provider](ctx, db, dir, "providers", TableProviders, "providers.json", func(p *domain.Provider) string { return p.ID }),
		legacyImporterFor[domain.CatalogModel](ctx, db, dir, "models", TableCatalogModel, "models.json", func(m *domain.CatalogModel) string { return m.ID }),
	}

	for _, src := range sources {
		n, err := src.run()
		if err != nil {
			return counts, fmt.Errorf("import %s: %w", src.name, err)
		}
		if n > 0 {
			counts[src.name] = n
		}
	}
	return counts, nil
}

// legacyImporter binds one JSON collection to its SQLite table.
type legacyImporter struct {
	name string
	run  func() (int, error)
}

func legacyImporterFor[T any](ctx context.Context, db *sql.DB, dir, name, table, file string, idOf func(*T) string) legacyImporter {
	return legacyImporter{name: name, run: func() (int, error) {
		return ImportJSON(ctx, db, jsonSource[T]{
			table: table, path: filepath.Join(dir, file), idOf: idOf,
		})
	}}
}
