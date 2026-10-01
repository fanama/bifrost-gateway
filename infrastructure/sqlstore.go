package infrastructure

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	_ "modernc.org/sqlite" // driver SQLite pur Go (compatible CGO_ENABLED=0 / image scratch)
)

// SQLStore est la realisation SQLite du port generique de persistance. Il
// remplace le JSONStore : chaque collection occupe une table a deux colonnes
// (id + payload JSON), ce qui conserve les tags json des entites du domaine
// comme format de serialisation et evite d'ecrire un mapping SQL par entite.
//
// Les operations de bas niveau gardent les memes noms et les memes signatures
// que le JSONStore pour que l'Adapter generique reste inchange : seule la source
// de verite change. Les predicats Go (Where, FindWhere) sont appliques en memoire
// apres chargement ; les requetes reellement filtrees et indexees utilisent
// WhereSQL ou FindBySQL.
type SQLStore[T any] struct {
	db    *sql.DB
	table string
	idOf  func(*T) string
}

func NewSQLStore[T any](db *sql.DB, table string, idOf func(*T) string) *SQLStore[T] {
	return &SQLStore[T]{db: db, table: table, idOf: idOf}
}

// DB expose le pool de connexions pour les seeds, les index et l'import JSON.
func (s *SQLStore[T]) DB() *sql.DB { return s.db }

// Table expose le nom de la table, utilise par les stores d'entite pour
// declarer leurs propres index.
func (s *SQLStore[T]) Table() string { return s.table }

// Seed insere fill une seule fois, lorsque la table est encore vide.
func (s *SQLStore[T]) Seed(ctx context.Context, fill func() []T) error {
	var count int
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+s.table).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return nil
	}

	items := fill()
	if items == nil {
		items = []T{}
	}
	return s.tx(ctx, func(tx *sql.Tx) error {
		for i := range items {
			if err := s.insertTx(tx, &items[i]); err != nil {
				return err
			}
		}
		return nil
	})
}

// All retourne la collection complete.
func (s *SQLStore[T]) All(ctx context.Context) ([]T, error) {
	return s.WhereSQL(ctx, "")
}

// FindWhere retourne le premier element validant pred.
func (s *SQLStore[T]) FindWhere(ctx context.Context, pred func(T) bool) (*T, bool) {
	items, err := s.Where(ctx, pred)
	if err != nil || len(items) == 0 {
		return nil, false
	}
	item := items[0]
	return &item, true
}

// Where filtre l'integralite de la collection en memoire.
func (s *SQLStore[T]) Where(ctx context.Context, pred func(T) bool) ([]T, error) {
	all, err := s.All(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]T, 0, len(all))
	for _, item := range all {
		if pred(item) {
			out = append(out, item)
		}
	}
	return out, nil
}

// WhereSQLfiltre via une clause SQL explicite, pour pousser les predicats
// indexables (project_id, digest...) jusqu'a SQLite au lieu de charger toute
// la table. where est concatene a la requete : n'accepter que des litteraux
// compiles dans le code, jamais de saisie utilisateur.
func (s *SQLStore[T]) WhereSQL(ctx context.Context, where string, args ...any) ([]T, error) {
	query := "SELECT payload FROM " + s.table
	if where != "" {
		query += " WHERE " + where
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	return scanAll[T](rows)
}

// FindBySQL retourne le premier resultat d'une clause SQL, ou found=false.
func (s *SQLStore[T]) FindBySQL(ctx context.Context, where string, args ...any) (*T, bool, error) {
	items, err := s.WhereSQL(ctx, where, args...)
	if err != nil || len(items) == 0 {
		return nil, false, err
	}
	return &items[0], true, nil
}

func (s *SQLStore[T]) Insert(ctx context.Context, item *T, reject func([]T) error) error {
	if reject != nil {
		existing, err := s.All(ctx)
		if err != nil {
			return err
		}
		if err := reject(existing); err != nil {
			return err
		}
	}
	return s.tx(ctx, func(tx *sql.Tx) error { return s.insertTx(tx, item) })
}

func (s *SQLStore[T]) insertTx(tx *sql.Tx, item *T) error {
	payload, err := json.Marshal(item)
	if err != nil {
		return err
	}
	_, err = tx.Exec("INSERT INTO "+s.table+"(id, payload) VALUES(?, ?)", s.idOf(item), payload)
	return err
}

// Replace remplace integralement l'entite dont l'identifiant vaut id. Retourne
// false si aucune ligne ne correspond, ce que l'Adapter traduit en notFound.
func (s *SQLStore[T]) Replace(ctx context.Context, id string, item *T) (bool, error) {
	payload, err := json.Marshal(item)
	if err != nil {
		return false, err
	}
	res, err := s.db.ExecContext(ctx, "UPDATE "+s.table+" SET payload = ? WHERE id = ?", payload, id)
	if err != nil {
		return false, err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return affected > 0, nil
}

func (s *SQLStore[T]) Remove(ctx context.Context, id string) (bool, error) {
	res, err := s.db.ExecContext(ctx, "DELETE FROM "+s.table+" WHERE id = ?", id)
	if err != nil {
		return false, err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return affected > 0, nil
}

// ReplaceAll reecrit la collection dans une transaction : soit la
// transformation est persistee, soit rien ne l'est.
func (s *SQLStore[T]) ReplaceAll(ctx context.Context, apply func(*[]T) error) error {
	items, err := s.All(ctx)
	if err != nil {
		return err
	}
	if err := apply(&items); err != nil {
		return err
	}

	return s.tx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.Exec("DELETE FROM " + s.table); err != nil {
			return err
		}
		for i := range items {
			if err := s.insertTx(tx, &items[i]); err != nil {
				return err
			}
		}
		return nil
	})
}

// UpdateColumns applique des affectations SQL sans charger la collection.
func (s *SQLStore[T]) UpdateColumns(ctx context.Context, where string, sets map[string]any, args ...any) (int64, error) {
	if len(sets) == 0 {
		return 0, nil
	}
	assignments := make([]string, 0, len(sets))
	values := make([]any, 0, len(sets)+len(args))
	for column, value := range sets {
		assignments = append(assignments, quoteIdent(column)+" = ?")
		values = append(values, value)
	}
	values = append(values, args...)

	query := "UPDATE " + s.table + " SET " + strings.Join(assignments, ", ")
	if where != "" {
		query += " WHERE " + where
	}
	res, err := s.db.ExecContext(ctx, query, values...)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func (s *SQLStore[T]) tx(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

func scanAll[T any](rows *sql.Rows) ([]T, error) {
	defer rows.Close()
	out := []T{}
	for rows.Next() {
		var payload []byte
		if err := rows.Scan(&payload); err != nil {
			return nil, err
		}
		var item T
		if err := json.Unmarshal(payload, &item); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
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
