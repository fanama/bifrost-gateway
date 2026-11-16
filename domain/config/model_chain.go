package config

import "strings"

// ModelChoice designe un modele et, quand il differe de celui de la
// configuration, le provider qui doit le servir. Un provider vide signifie
// "le provider de la configuration".
type ModelChoice struct {
	Provider string `json:"provider,omitempty"`
	Model    string `json:"model"`
}

// Label rend l'entree lisible et stable pour les journaux et les cles de cache.
func (m ModelChoice) Label() string {
	provider := strings.TrimSpace(m.Provider)
	if provider == "" {
		return m.Model
	}
	return provider + "/" + m.Model
}

// Candidates rend la chaine ordonnee des modeles d'une configuration : le
// modele principal d'abord, puis les replis dans l'ordre declare. La premiere
// entree qui repond sert la requete, donc une configuration reste utilisable
// tant qu'un de ses modeles est disponible.
//
// Un repli sans provider herite de celui de la configuration ; les entrees
// vides sont ignorees et les doublons retires (provider + modele, sans casse).
func (c *ChatConfig) Candidates() []ModelChoice {
	if c == nil {
		return nil
	}
	return ModelCandidates(c.Provider, c.Model, c.FallbackModels)
}

// ModelCandidates normalise une chaine de modeles. Exportee pour que la saisie
// (formulaire) et les tests appliquent la meme regle que l'execution.
func ModelCandidates(provider, model string, fallbacks []ModelChoice) []ModelChoice {
	primary := strings.TrimSpace(provider)
	ordered := make([]ModelChoice, 0, len(fallbacks)+1)
	seen := make(map[string]struct{}, len(fallbacks)+1)

	all := make([]ModelChoice, 0, len(fallbacks)+1)
	all = append(all, ModelChoice{Model: model})
	all = append(all, fallbacks...)
	for _, raw := range all {
		choice := normalizeChoice(primary, raw)
		if choice.Model == "" {
			continue
		}
		key := strings.ToLower(choice.Provider + "/" + choice.Model)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		ordered = append(ordered, choice)
	}
	return ordered
}

func normalizeChoice(primary string, choice ModelChoice) ModelChoice {
	out := ModelChoice{
		Provider: strings.TrimSpace(choice.Provider),
		Model:    strings.TrimSpace(choice.Model),
	}
	if out.Provider == "" {
		out.Provider = primary
	}
	return out
}

// WithModel rend une copie de la configuration ciblant une entree de la
// chaine. Elle sert a construire chaque tentative sans muter la configuration
// stockee, qui reste l'unique representation en circulation.
//
// Les identifiants explicites de la configuration appartiennent a son
// provider : un repli heberge ailleurs repart sans cle ni URL, pour que
// l'adaptateur resolve celles du provider vise.
func (c *ChatConfig) WithModel(choice ModelChoice) *ChatConfig {
	out := *c
	out.Model = choice.Model
	if provider := strings.TrimSpace(choice.Provider); provider != "" {
		out.Provider = provider
	}
	if !strings.EqualFold(out.Provider, c.Provider) {
		out.APIKey = ""
		out.BaseURL = ""
	}
	return &out
}
