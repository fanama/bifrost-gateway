package routing

import (
	"context"
	"errors"
	"sort"
	"strings"

	"bridge-gateway/domain/llm"
)

// FailoverReason classifies why a provider attempt failed so routing decisions
// are testable and logged with stable labels.
type FailoverReason string

const (
	FailoverNone          FailoverReason = ""
	FailoverRateLimit     FailoverReason = "rate_limit"
	FailoverContextLength FailoverReason = "context_length"
	FailoverUnavailable   FailoverReason = "unavailable"
	FailoverAuth          FailoverReason = "auth"
	FailoverModel         FailoverReason = "model_unavailable"
	FailoverClient        FailoverReason = "client"
)

// ClassifyFailure decides whether a provider error warrants escalating to the
// next tier in the routing ladder.
func ClassifyFailure(err error) FailoverReason {
	if err == nil {
		return FailoverNone
	}
	if errors.Is(err, context.Canceled) {
		return FailoverClient
	}

	var provErr *llm.ProviderError
	if !errors.As(err, &provErr) {
		return FailoverUnavailable
	}

	switch {
	case provErr.StatusCode == 429:
		return FailoverRateLimit
	case provErr.StatusCode == 401 || provErr.StatusCode == 403:
		return FailoverAuth
	case provErr.StatusCode >= 500:
		return FailoverUnavailable
	case provErr.StatusCode == 404 && IsModelUnavailableError(provErr):
		return FailoverModel
	case provErr.StatusCode == 400 && IsContextLengthError(provErr):
		return FailoverContextLength
	case provErr.StatusCode >= 400 && provErr.StatusCode < 500:
		return FailoverClient
	default:
		return FailoverUnavailable
	}
}

// IsModelUnavailableError distinguishes a 404 caused by an unknown/unavailable
// model from a 404 caused by an invalid API path.
func IsModelUnavailableError(err *llm.ProviderError) bool {
	if err == nil {
		return false
	}
	switch err.Code {
	case "model_not_found", "unknown_model", "model_not_available":
		return true
	}
	haystack := strings.ToLower(err.Message + " " + err.Detail)
	if !strings.Contains(haystack, "model") {
		return false
	}
	for _, marker := range []string{
		"not found",
		"does not exist",
		"unknown model",
		"no such model",
		"not available",
		"is not supported",
		"unsupported model",
		"not supported",
	} {
		if strings.Contains(haystack, marker) {
			return true
		}
	}
	return false
}

// IsContextLengthError detects provider errors caused by prompt/context length
// exceeding the target model's window.
func IsContextLengthError(err *llm.ProviderError) bool {
	if err == nil {
		return false
	}
	switch err.Code {
	case "context_length_exceeded", "model_context_length_exceeded", "string_above_max_length":
		return true
	}
	haystack := strings.ToLower(err.Message + " " + err.Detail)
	for _, marker := range []string{
		"context length",
		"context_length",
		"context window",
		"maximum context",
		"too many tokens",
		"reduce the length",
		"prompt is too long",
		"input length",
	} {
		if strings.Contains(haystack, marker) {
			return true
		}
	}
	return false
}

// CooldownKey returns the canonical provider/model key used to track tier
// saturation independent of configuration IDs.
func CooldownKey(provider, model string) string {
	return strings.ToLower(provider) + "/" + strings.ToLower(model)
}

// RankedCandidate is any routing candidate with a tier rank.
type RankedCandidate interface {
	TierRank() int
}

// SelectStartRank chooses the starting tier rank from the active rank and an
// optional classifier prediction. The classifier may escalate above the active
// rank but never downgrade below it.
func SelectStartRank(activeRank, predictedRank int, hasPrediction bool) int {
	start := 0
	if activeRank >= 0 {
		start = activeRank
	}
	if hasPrediction && predictedRank >= start {
		start = predictedRank
	}
	return start
}

// FilterAndOrderLadder filters candidates with valid tiers >= startRank, sorts
// them in ascending tier order, and removes candidates in cooldown unless doing
// so would empty the ladder (never prefer silence over retrying a cooled tier).
func FilterAndOrderLadder[T RankedCandidate](candidates []T, startRank int, inCooldown func(T) bool) []T {
	usable := make([]T, 0, len(candidates))
	for _, c := range candidates {
		if c.TierRank() >= 0 {
			usable = append(usable, c)
		}
	}
	if len(usable) == 0 {
		return nil
	}
	sort.Slice(usable, func(i, j int) bool {
		return usable[i].TierRank() < usable[j].TierRank()
	})

	ladder := make([]T, 0, len(usable))
	for _, c := range usable {
		if c.TierRank() >= startRank {
			ladder = append(ladder, c)
		}
	}
	if len(ladder) == 0 {
		return nil
	}
	if inCooldown == nil {
		return ladder
	}

	fresh := make([]T, 0, len(ladder))
	for _, c := range ladder {
		if !inCooldown(c) {
			fresh = append(fresh, c)
		}
	}
	if len(fresh) == 0 {
		return ladder
	}
	return fresh
}
