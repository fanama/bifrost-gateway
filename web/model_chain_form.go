package web

import (
	"net/http"
	"strings"

	"bridge-gateway/domain"
)

// parseFallbackChoices lit les listes paralleles du formulaire de repli :
// chaque ligne envoie son provider et son modele, et l'ordre DOM des lignes est
// l'ordre d'essai. Une ligne sans modele est ignoree ; un provider inconnu
// laisse le domaine retomber sur celui de la configuration.
func parseFallbackChoices(r *http.Request, providers []string) []domain.ModelChoice {
	providerValues := r.Form["fallback_provider[]"]
	modelValues := r.Form["fallback_model[]"]
	choices := make([]domain.ModelChoice, 0, len(modelValues))
	for i, model := range modelValues {
		model = strings.TrimSpace(model)
		if model == "" {
			continue
		}
		provider := ""
		if i < len(providerValues) {
			provider = strings.TrimSpace(providerValues[i])
		}
		if !knownProvider(provider, providers) {
			provider = ""
		}
		choices = append(choices, domain.ModelChoice{Provider: provider, Model: model})
	}
	if len(choices) == 0 {
		return nil
	}
	return choices
}

func knownProvider(name string, providers []string) bool {
	name = strings.TrimSpace(name)
	for _, provider := range providers {
		if strings.EqualFold(provider, name) {
			return true
		}
	}
	return false
}
