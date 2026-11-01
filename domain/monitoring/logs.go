package monitoring

import "time"

// CallLog est une entree du journal des appels LLM sortants : la requete
// envoyee et la reponse produite, pour le debogage depuis le tableau de bord.
//
// Le domaine ne fixe que la forme de l'entree. Le collecteur la remplit en
// memoire (journal circulaire borne), l'UI la rend dans une modale et
// l'export JSON/CSV la serialise telle quelle.
type CallLog struct {
	Time     time.Time     `json:"time"`
	Provider string        `json:"provider"`
	Model    string        `json:"model"`
	Stream   bool          `json:"stream"`
	Duration time.Duration `json:"duration_ns"`
	Input    string        `json:"input"`
	Output   string        `json:"output"`
	Error    string        `json:"error,omitempty"`
}
