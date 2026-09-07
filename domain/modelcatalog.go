package domain

import (
	"strings"
	"time"
)

// CatalogModel est un modele du catalogue gerable depuis l'UI (nom + provider).
// Il alimente le datalist du formulaire de configuration.
type CatalogModel struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Provider  string    `json:"provider"`
	CreatedAt time.Time `json:"created_at"`
}

func (m *CatalogModel) Validate() error {
	var missing []string
	if strings.TrimSpace(m.Name) == "" {
		missing = append(missing, "name")
	}
	if strings.TrimSpace(m.Provider) == "" {
		missing = append(missing, "provider")
	}
	if len(missing) > 0 {
		return &ValidationError{Fields: missing}
	}
	return nil
}

// ModelCatalogRepository est le port de persistance du catalogue de modeles.
type ModelCatalogRepository = CrudRepository[CatalogModel]
