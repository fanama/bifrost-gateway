package routing

import (
	"context"
	"errors"
	"testing"

	"bridge-gateway/domain/llm"
)

type stubCandidate struct {
	name string
	rank int
}

func (s stubCandidate) TierRank() int { return s.rank }

func TestClassifyFailureTable(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want FailoverReason
	}{
		{"nil", nil, FailoverNone},
		{"canceled", errors.Join(context.Canceled, &llm.ProviderError{StatusCode: 429}), FailoverClient},
		{"rate limit", &llm.ProviderError{StatusCode: 429, Message: "slow down"}, FailoverRateLimit},
		{"unauthorized", &llm.ProviderError{StatusCode: 401, Message: "bad key"}, FailoverAuth},
		{"forbidden", &llm.ProviderError{StatusCode: 403, Message: "denied"}, FailoverAuth},
		{"server error", &llm.ProviderError{StatusCode: 503, Message: "down"}, FailoverUnavailable},
		{"model 404", &llm.ProviderError{StatusCode: 404, Code: "model_not_found", Message: "missing"}, FailoverModel},
		{"route 404", &llm.ProviderError{StatusCode: 404, Message: "not found"}, FailoverClient},
		{"context 400", &llm.ProviderError{StatusCode: 400, Message: "maximum context length is 8192"}, FailoverContextLength},
		{"client 400", &llm.ProviderError{StatusCode: 400, Message: "invalid json"}, FailoverClient},
		{"non-provider error", errors.New("dial tcp timeout"), FailoverUnavailable},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ClassifyFailure(tc.err); got != tc.want {
				t.Fatalf("ClassifyFailure() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestFilterAndOrderLadderRespectsStartRankAndCooldownFallback(t *testing.T) {
	items := []stubCandidate{
		{name: "frontier", rank: 2},
		{name: "unranked", rank: -1},
		{name: "fast", rank: 0},
		{name: "balanced", rank: 1},
	}

	start := SelectStartRank(0, 1, true)
	if start != 1 {
		t.Fatalf("SelectStartRank = %d, want 1", start)
	}

	ladder := FilterAndOrderLadder(items, start, func(c stubCandidate) bool {
		return c.name == "balanced"
	})
	if len(ladder) != 1 || ladder[0].name != "frontier" {
		t.Fatalf("ladder after cooldown skip = %+v, want [frontier]", ladder)
	}

	// If all eligible tiers are in cooldown, fallback keeps the full ladder.
	allCooled := FilterAndOrderLadder(items, start, func(stubCandidate) bool { return true })
	if len(allCooled) != 2 || allCooled[0].name != "balanced" || allCooled[1].name != "frontier" {
		t.Fatalf("ladder when all cooled = %+v, want [balanced frontier]", allCooled)
	}
}
