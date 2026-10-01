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

	imported := 0
	for i := range items {
		item := items[i]
		id := source.idOf(&item)
		if id == "" {
			continue // entite sans identifiant : rien a inserer
		}
		encoded, err := json.Marshal(&item)
		if err != nil {
			return imported, fmt.Errorf("encode item %d from %s: %w", i, source.path, err)
		}
		res, err := db.ExecContext(ctx,
			"INSERT OR IGNORE INTO "+quoteIdent(source.table)+"(id, payload) VALUES(?, ?)",
			id, encoded)
		if err != nil {
			return imported, fmt.Errorf("insert into %s: %w", source.table, err)
		}
		affected, err := res.RowsAffected()
		if err != nil {
			return imported, err
		}
		imported += int(affected)
	}

	if err := renameAside(source.path); err != nil {
		return imported, err
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
	sources := []struct {
		name  string
		table string
		file  string
		idOf  func(any) string
		run   func() (int, error)
	}{
		{
			name: "configs", table: TableConfigs, file: "configs.json",
			run: func() (int, error) {
				return ImportJSON(ctx, db, jsonSource[domain.ChatConfig]{
					table: TableConfigs, path: filepath.Join(dir, "configs.json"),
					idOf: func(c *domain.ChatConfig) string { return c.ID },
				})
			},
		},
		{
			name: "projects", table: TableProjects, file: "projects.json",
			run: func() (int, error) {
				return ImportJSON(ctx, db, jsonSource[domain.Project]{
					table: TableProjects, path: filepath.Join(dir, "projects.json"),
					idOf: func(p *domain.Project) string { return p.ID },
				})
			},
		},
		{
			name: "apikeys", table: TableAPIKeys, file: "apikeys.json",
			run: func() (int, error) {
				return ImportJSON(ctx, db, jsonSource[domain.APIKey]{
					table: TableAPIKeys, path: filepath.Join(dir, "apikeys.json"),
					idOf: func(k *domain.APIKey) string { return k.ID },
				})
			},
		},
		{
			name: "providers", table: TableProviders, file: "providers.json",
			run: func() (int, error) {
				return ImportJSON(ctx, db, jsonSource[domain.Provider]{
					table: TableProviders, path: filepath.Join(dir, "providers.json"),
					idOf: func(p *domain.Provider) string { return p.ID },
				})
			},
		},
		{
			name: "models", table: TableCatalogModel, file: "models.json",
			run: func() (int, error) {
				return ImportJSON(ctx, db, jsonSource[domain.CatalogModel]{
					table: TableCatalogModel, path: filepath.Join(dir, "models.json"),
					idOf: func(m *domain.CatalogModel) string { return m.ID },
				})
			},
		},
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
