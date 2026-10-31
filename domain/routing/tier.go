package routing

import "bridge-gateway/domain/config"

// MergeTierConfig construit la configuration effective d'une cible de
// l'echelle de tiers. La cible fournit provider, modele et identifiants ; les
// reglages de generation et le prompt systeme restent ceux de la configuration
// active, pour qu'une escalation ne change pas le comportement de la
// conversation.
//
// Meme provider et meme modele : la configuration active est retournee telle
// quelle. Cible nulle : nil.
func MergeTierConfig(active, target *config.ChatConfig) *config.ChatConfig {
	if target == nil {
		return nil
	}
	if active == nil {
		out := *target
		return &out
	}
	if active.Provider == target.Provider && active.Model == target.Model {
		return active
	}

	out := *target
	out.SystemPrompt = active.SystemPrompt
	out.Temperature = active.Temperature
	out.TopP = active.TopP
	out.MaxTokens = active.MaxTokens
	out.FrequencyPenalty = active.FrequencyPenalty
	out.PresencePenalty = active.PresencePenalty
	out.ResponseFormat = active.ResponseFormat
	out.ResponseSchema = active.ResponseSchema
	return &out
}
