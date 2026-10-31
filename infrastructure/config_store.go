package infrastructure

import (
	"context"
	"database/sql"
	"errors"
	"sort"

	"bridge-gateway/domain"
)

// ChatConfigStore implemente domain.ChatConfigRepository sur SQLite.
type ChatConfigStore struct {
	*Adapter[domain.ChatConfig]
}

func NewChatConfigStore(db *sql.DB) *ChatConfigStore {
	inner := NewSQLStore[domain.ChatConfig](db, TableConfigs, func(c *domain.ChatConfig) string { return c.ID })
	return &ChatConfigStore{Adapter: NewAdapter(
		inner,
		func(c *domain.ChatConfig) string { return c.ID },
		domain.ErrConfigNotFound,
		nil,
	)}
}

// Seed insere les configurations par defaut si la table est vide.
func (s *ChatConfigStore) Seed(ctx context.Context, seeds []domain.ChatConfig) error {
	if seeds == nil {
		seeds = []domain.ChatConfig{}
	}
	return s.Store().Seed(ctx, func() []domain.ChatConfig { return seeds })
}

// ListByProject rend les configurations d'un projet dans un ordre stable.
//
// L'ordre SQL ne peut pas servir : SetActive reecrit toute la table (DELETE puis
// INSERT), ce qui regenere les rowid et fait sauter les lignes d'une action a
// l'autre. Une liste qui se reordonne apres chaque clic est indesorientante --
// on ne retrouve plus la ligne que l'on vient d'activer. Le tri est donc applique
// ici, sur un critere qui survit a la reecriture : la date de creation, puis
// l'identifiant comme departage.
func (s *ChatConfigStore) ListByProject(ctx context.Context, projectID string) ([]domain.ChatConfig, error) {
	// Pousse le filtre project_id jusqu'a l'index JSON au lieu de scanner la table.
	items, err := s.Store().WhereSQL(ctx, `json_extract(payload, '$.project_id') = ?`, projectID)
	if err != nil {
		return nil, err
	}
	sort.SliceStable(items, func(i, j int) bool {
		if !items[i].CreatedAt.Equal(items[j].CreatedAt) {
			return items[i].CreatedAt.Before(items[j].CreatedAt)
		}
		return items[i].ID < items[j].ID
	})
	return items, nil
}

func (s *ChatConfigStore) GetActiveByProject(ctx context.Context, projectID string) (*domain.ChatConfig, error) {
	item, ok, err := s.Store().FindBySQL(ctx,
		`json_extract(payload, '$.project_id') = ? AND json_extract(payload, '$.active') = ?`,
		projectID, 1)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, domain.ErrNoActiveConfig
	}
	return item, nil
}

// SetActive bascule la config id comme seule active du projet. L'invariant
// appartient au domaine (domain.MarkActiveConfig) ; l'adaptateur ne fait que
// le persister dans une transaction. Si id n'appartient pas au projet, la
// mutation echoue et ReplaceAll n'ecrit rien.
func (s *ChatConfigStore) SetActive(ctx context.Context, projectID string, id string) error {
	return s.Store().ReplaceAll(ctx, func(items *[]domain.ChatConfig) error {
		return domain.MarkActiveConfig(*items, projectID, id)
	})
}

var _ domain.ChatConfigRepository = (*ChatConfigStore)(nil)

func IsNotFound(err error) bool {
	return errors.Is(err, domain.ErrConfigNotFound)
}
