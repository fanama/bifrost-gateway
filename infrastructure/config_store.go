package infrastructure

import (
	"context"
	"database/sql"
	"errors"

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

func (s *ChatConfigStore) ListByProject(ctx context.Context, projectID string) ([]domain.ChatConfig, error) {
	// Pousse le filtre project_id jusqu'a l'index JSON au lieu de scanner la table.
	return s.Store().WhereSQL(ctx, `json_extract(payload, '$.project_id') = ?`, projectID)
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

// SetActive bascule la config id comme seule active du projet.
//
// L'echec doit etre total : si id n'appartient pas au projet, aucune ecriture
// n'est effectuee. Pour cela la verification "trouve" est faite dans la
// fonction de mutation, qui fait echouer ReplaceAll avant la transaction. Sans
// cela, toutes les configs du projet seraient desactivees puis reappliquees,
// laissant le projet sans aucune config active.
func (s *ChatConfigStore) SetActive(ctx context.Context, projectID string, id string) error {
	return s.Store().ReplaceAll(ctx, func(items *[]domain.ChatConfig) error {
		found := false
		for i := range *items {
			if (*items)[i].ProjectID != projectID {
				continue
			}
			if (*items)[i].ID == id {
				(*items)[i].Active = true
				found = true
			} else {
				(*items)[i].Active = false
			}
		}
		if !found {
			return domain.ErrConfigNotFound
		}
		return nil
	})
}

var _ domain.ChatConfigRepository = (*ChatConfigStore)(nil)

func IsNotFound(err error) bool {
	return errors.Is(err, domain.ErrConfigNotFound)
}
