package domain

import (
	"strings"
	"time"
)

// Kind de modele dans le catalogue. Un modele d'embedding est propose dans le
// select du testeur d'embeddings et resoluble par le routeur ; un modele de
// chat alimente uniquement le datalist des configurations.
const (
	ModelKindChat      = "chat"
	ModelKindEmbedding = "embedding"
)

// NormalizeModelKind ramene un kind saisi vers sa forme canonique. Une valeur
// vide vaut "chat" : les entrees enregistrees avant l'apparition du champ
// restent des modeles de chat, et un appelant qui ne sait pas peut taire le
// parametre sans produire un kind inconnu.
func NormalizeModelKind(kind string) string {
	k := strings.ToLower(strings.TrimSpace(kind))
	if k == "" {
		return ModelKindChat
	}
	return k
}

// CatalogModel est un modele du catalogue gerable depuis l'UI (nom + provider
// + kind). Il alimente le datalist du formulaire de configuration et, pour un
// modele d'embedding, le select du testeur d'embeddings.
type CatalogModel struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Provider  string    `json:"provider"`
	Kind      string    `json:"kind,omitempty"`
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
	switch NormalizeModelKind(m.Kind) {
	case ModelKindChat, ModelKindEmbedding:
	default:
		return &ValidationError{Fields: []string{"kind"}, Reason: "doit valoir chat ou embedding"}
	}
	return nil
}

// ModelCatalogRepository est le port de persistance du catalogue de modeles.
// La mise a jour est requise pour corriger le kind d'un modele deja present
// sans passer par une suppression (reajout du meme modele sous un autre type).
type ModelCatalogRepository = UpdatableRepository[CatalogModel]
