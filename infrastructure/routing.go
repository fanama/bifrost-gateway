package infrastructure

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"bridge-gateway/domain"
	"bridge-gateway/domain/routing"
)

// RoutingLLMProvider routes chat requests across complexity tiers and escalates
// up the ladder when a target tier is saturated or unavailable.
type RoutingLLMProvider struct {
	inner      domain.LLMProvider
	classifier domain.Classifier
	configs    domain.ChatConfigRepository
	providers  domain.ProviderRepository

	minConfidence float64
	cooldowns     *cooldownStore
	maxAttempts   int
}

// NewRoutingLLMProvider constructs a tier-routing decorator around inner.
func NewRoutingLLMProvider(
	inner domain.LLMProvider,
	classifier domain.Classifier,
	configs domain.ChatConfigRepository,
	providers domain.ProviderRepository,
	opts RoutingOptions,
) *RoutingLLMProvider {
	return &RoutingLLMProvider{
		inner:         inner,
		classifier:    classifier,
		configs:       configs,
		providers:     providers,
		minConfidence: opts.MinConfidence,
		cooldowns:     newCooldownStore(opts.Cooldown),
		maxAttempts:   opts.MaxAttempts,
	}
}

// RoutingOptions holds tier-routing parameters derived from config.yaml.
type RoutingOptions struct {
	MinConfidence float64
	Cooldown      time.Duration
	MaxAttempts   int
}

// Chat selects a starting tier and escalates up the ladder on recoverable provider failures.
func (p *RoutingLLMProvider) Chat(ctx context.Context, cfg *domain.ChatConfig, messages []domain.ChatMessage) (domain.LLMResult, error) {
	ladder := p.ladder(ctx, cfg, messages)
	if len(ladder) == 0 {
		return p.inner.Chat(ctx, cfg, messages)
	}

	var (
		attempts []string
		lastErr  error
	)
	for i, target := range ladder {
		if p.maxAttempts > 0 && i >= p.maxAttempts {
			break
		}
		result, done, err := p.tryChatTarget(ctx, cfg, target, messages, &attempts, &lastErr)
		if done {
			return result, err
		}
	}

	if lastErr == nil {
		lastErr = fmt.Errorf("no routing candidate was callable")
	}
	return domain.LLMResult{}, p.aggregateError(cfg, attempts, lastErr)
}

func (p *RoutingLLMProvider) tryChatTarget(
	ctx context.Context,
	cfg *domain.ChatConfig,
	target *domain.ChatConfig,
	messages []domain.ChatMessage,
	attempts *[]string,
	lastErr *error,
) (domain.LLMResult, bool, error) {
	merged := p.applyTier(ctx, cfg, target)
	if merged == nil {
		*attempts = append(*attempts, fmt.Sprintf("%s/%s (identifiants manquants)", target.Provider, target.Model))
		return domain.LLMResult{}, false, nil
	}

	result, err := p.inner.Chat(ctx, merged, messages)
	if err == nil {
		if len(*attempts) > 0 {
			logRoutingFailover(cfg.Model, result.Model, *attempts)
		}
		return result, true, nil
	}

	if p.recordAttemptFailure(ctx, target, err, attempts, lastErr) {
		return domain.LLMResult{}, true, p.aggregateError(cfg, *attempts, err)
	}
	return domain.LLMResult{}, false, nil
}

func (p *RoutingLLMProvider) recordAttemptFailure(
	ctx context.Context,
	target *domain.ChatConfig,
	err error,
	attempts *[]string,
	lastErr *error,
) bool {
	reason := failoverReason(err)
	*attempts = append(*attempts, fmt.Sprintf("%s/%s (%s)", target.Provider, target.Model, reason))
	*lastErr = err

	if ctx.Err() != nil {
		return true
	}
	if reason == FailoverClient {
		return true
	}
	if reason == FailoverAuth {
		log.Printf("routing: auth failure on %s/%s, escalating (check the API key for this configuration)", target.Provider, target.Model)
	}
	log.Printf("routing: %s/%s unavailable (%s), escalating", target.Provider, target.Model, reason)
	p.cooldownTier(target)
	return false
}

func cooldownKey(target *domain.ChatConfig) string {
	return routing.CooldownKey(target.Provider, target.Model)
}

func (p *RoutingLLMProvider) cooldownTier(target *domain.ChatConfig) {
	p.cooldowns.Mark(cooldownKey(target))
}

func (p *RoutingLLMProvider) inCooldown(target *domain.ChatConfig) bool {
	return p.cooldowns.Active(cooldownKey(target))
}

func (p *RoutingLLMProvider) aggregateError(cfg *domain.ChatConfig, attempts []string, lastErr error) error {
	if len(attempts) <= 1 {
		return lastErr
	}
	return fmt.Errorf("all routing candidates failed for %s: %s: %w", cfg.Model, strings.Join(attempts, ", "), lastErr)
}

func logRoutingFailover(requested, served string, attempts []string) {
	log.Printf("routing: %s saturated after %s, served by %s", requested, strings.Join(attempts, " -> "), served)
}

var _ domain.LLMProvider = (*RoutingLLMProvider)(nil)
