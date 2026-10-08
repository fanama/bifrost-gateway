package domain

// ModelInfo est la vue agregee d'un modele connu (config.yaml, catalogue UI
// ou configuration enregistree). ID et Kind renseignent uniquement pour les
// modeles du catalogue gerable depuis l'UI ; Kind vaut ModelKindEmbedding pour
// ceux a proposer au testeur d'embeddings.
type ModelInfo struct {
	ID       string `json:"id,omitempty"`
	Name     string `json:"name"`
	Provider string `json:"provider"`
	Kind     string `json:"kind,omitempty"`
	Source   string `json:"source"`
}
