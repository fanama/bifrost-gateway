package domain

// ModelInfo est la vue agregee d'un modele connu (config.yaml, catalogue UI
// ou configuration enregistree). ID renseigne uniquement pour les modeles du
// catalogue gerable depuis l'UI.
type ModelInfo struct {
	ID       string `json:"id,omitempty"`
	Name     string `json:"name"`
	Provider string `json:"provider"`
	Source   string `json:"source"`
}
