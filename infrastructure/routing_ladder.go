package infrastructure

import (
	"context"
	"fmt"
	"log"

	"bridge-gateway/domain"
	"bridge-gateway/domain/routing"
)

// ladder builds the ordered list of candidate configurations to attempt.
func (p *RoutingLLMProvider) ladder(ctx context.Context, cfg *domain.ChatConfig, messages []domain.ChatMessage) []*domain.ChatConfig {
	if p.classifier == nil || p.configs == nil {
		return nil
	}

	tiers, err := p.configs.ListByProject(ctx, cfg.ProjectID)
	if err != nil {
		log.Printf("routing: unable to list configurations: %v", err)
		return nil
	}

	ptrs := make([]*domain.ChatConfig, 0, len(tiers))
	for i := range tiers {
		copied := tiers[i]
		ptrs = append(ptrs, &copied)
	}

	predicted, predErr := p.predict(ctx, messages)
	start := routing.SelectStartRank(cfg.TierRank(), predicted, predErr == nil)

	return routing.FilterAndOrderLadder(ptrs, start, func(c *domain.ChatConfig) bool {
		if p.inCooldown(c) {
			log.Printf("routing: %s/%s in cooldown, skipped", c.Provider, c.Model)
			return true
		}
		return false
	})
}

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

// applyTier resout les identifiants de la cible puis delegue au domaine la
// fusion des reglages de conversation. L'adaptateur ne garde que la resolution
// des credentials, qui depend du repository de providers.
func (p *RoutingLLMProvider) applyTier(ctx context.Context, active *domain.ChatConfig, target *domain.ChatConfig) *domain.ChatConfig {
	if target == nil {
		return nil
	}

	out := *target
	if out.APIKey == "" && out.BaseURL == "" && p.providers != nil {
		if prov := resolveProvider(ctx, p.providers, target.Provider); prov != nil {
			out.APIKey = prov.APIKey
			out.BaseURL = prov.BaseURL
		}
	}
	return routing.MergeTierConfig(active, &out)
}
