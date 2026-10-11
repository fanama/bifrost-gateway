package infrastructure

import (
	"context"
	"fmt"
	"log"
	"sort"

	"bridge-gateway/domain"
)

// ladder construit la liste ordonnee des cibles a tenter.
//
// Le point de depart est le tier le plus faible entre le verdict du classifieur
// et la configuration active : le classifieur ne peut ni descendre sous le
// modele que l'utilisateur a choisi, ni envoyer une requete complexe vers un
// modele incapable de la traiter. Ensuite l'echelle monte jusqu'au plus
// puissant disponible.
func (p *RoutingLLMProvider) ladder(ctx context.Context, cfg *domain.ChatConfig, messages []domain.ChatMessage) []*domain.ChatConfig {
	if p.classifier == nil || p.configs == nil {
		return nil
	}

	tiers, err := p.configs.ListByProject(ctx, cfg.ProjectID)
	if err != nil {
		log.Printf("routing: unable to list configurations: %v", err)
		return nil
	}

	usable := make([]*domain.ChatConfig, 0, len(tiers))
	for i := range tiers {
		if tiers[i].TierRank() < 0 {
			continue
		}
		copied := tiers[i]
		usable = append(usable, &copied)
	}
	if len(usable) == 0 {
		return nil
	}
	sort.Slice(usable, func(i, j int) bool {
		return usable[i].TierRank() < usable[j].TierRank()
	})

	start := 0
	activeRank := cfg.TierRank()
	if activeRank >= 0 {
		start = activeRank
	}

	if predicted, err := p.predict(ctx, messages); err == nil && predicted >= start {
		start = predicted
	}

	ladder := make([]*domain.ChatConfig, 0, len(usable))
	for _, c := range usable {
		if c.TierRank() >= start {
			ladder = append(ladder, c)
		}
	}
	if len(ladder) == 0 {
		// Aucun tier ne convient : la configuration active reste le seul essai
		// possible.
		return nil
	}

	// On retire les cibles en cooldown, sauf si cela vide l'echelle : mieux vaut
	// retenter un modele douteux que de ne pas repondre du tout.
	fresh := ladder[:0:0]
	for _, c := range ladder {
		if p.inCooldown(c) {
			log.Printf("routing: %s/%s in cooldown, skipped", c.Provider, c.Model)
			continue
		}
		fresh = append(fresh, c)
	}
	if len(fresh) == 0 {
		return ladder
	}
	return fresh
}

// predict retourne le rang propose par le classifieur, ou une erreur.
//
// Une confiance sous le seuil est traitee comme une absence de verdict : le
// routeur conserve alors le point de depart calcule par le rang de la
// configuration active.
func (p *RoutingLLMProvider) predict(ctx context.Context, messages []domain.ChatMessage) (int, error) {
	tier, confidence, err := p.classifier.Classify(ctx, messages)
	if err != nil {
		return 0, err
	}
	if confidence < p.minConfidence {
		return 0, fmt.Errorf("classifier confidence %.2f below threshold %.2f", confidence, p.minConfidence)
	}
	rank := domain.TierRankOf(tier)
	if rank < 0 {
		return 0, fmt.Errorf("classifier returned unknown tier %q", tier)
	}
	return rank, nil
}

// applyTier produit la configuration effective pour une cible.
//
// Seuls les champs qui identifient le fournisseur et le modele sont repris. Le
// system prompt, la temperature, le top_p, les penalties et le format de
// reponse viennent de la configuration active, pour que la bascule ne modifie
// pas le comportement attendu par l'appelant.
func (p *RoutingLLMProvider) applyTier(ctx context.Context, active *domain.ChatConfig, target *domain.ChatConfig) *domain.ChatConfig {
	if target == nil {
		return nil
	}
	if active != nil && active.Provider == target.Provider && active.Model == target.Model {
		// Meme cible : on rend la configuration active, et non une copie.
		return active
	}

	out := *target
	if out.APIKey == "" && out.BaseURL == "" && p.providers != nil {
		// Une cible qui n'a pas ses propres identifiants les emprunte au
		// provider enregistre, comme le fait deja clientFor en aval.
		if prov := resolveProvider(ctx, p.providers, target.Provider); prov != nil {
			out.APIKey = prov.APIKey
			out.BaseURL = prov.BaseURL
		}
	}

	// Les reglages de comportement restent ceux de la configuration active.
	if active != nil {
		out.SystemPrompt = active.SystemPrompt
		out.Temperature = active.Temperature
		out.TopP = active.TopP
		out.MaxTokens = active.MaxTokens
		out.FrequencyPenalty = active.FrequencyPenalty
		out.PresencePenalty = active.PresencePenalty
		out.ResponseFormat = active.ResponseFormat
		// Le schema accompagne le format : sans lui, une escalade vers un tier
		// superieur continuerait de demander du JSON structuree mais perdrait la
		// forme exacte attendue.
		out.ResponseSchema = active.ResponseSchema
	}
	return &out
}
