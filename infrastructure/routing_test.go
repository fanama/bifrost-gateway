package infrastructure

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"bridge-gateway/domain"
)

// --- Tier sur ChatConfig ---

func TestTierRankOrdersTiers(t *testing.T) {
	cases := []struct {
		tier string
		want int
	}{
		{domain.TierFast, 0},
		{domain.TierBalanced, 1},
		{domain.TierFrontier, 2},
		{"FAST", 0},
		{"  frontier  ", 2},
		{"", -1},
		{"unknown", -1},
	}
	for _, c := range cases {
		if got := domain.TierRankOf(c.tier); got != c.want {
			t.Errorf("TierRankOf(%q) = %d, want %d", c.tier, got, c.want)
		}
	}
}

func TestValidateRejectsUnknownTier(t *testing.T) {
	cfg := &domain.ChatConfig{Name: "x", Provider: "ollama", Model: "m", Tier: "turbo"}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected a validation error for an unknown tier")
	}
	var verr *domain.ValidationError
	if !errors.As(err, &verr) {
		t.Fatalf("expected ValidationError, got %T", err)
	}
	if len(verr.Fields) != 1 || verr.Fields[0] != "tier" {
		t.Errorf("expected the tier field to be reported, got %v", verr.Fields)
	}
}

func TestValidateAcceptsEmptyTier(t *testing.T) {
	cfg := &domain.ChatConfig{Name: "x", Provider: "ollama", Model: "m"}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("an empty tier must stay valid so existing rows keep working: %v", err)
	}
}

// --- Classification des echecs ---

func TestFailoverReasonClassifiesStatusCodes(t *testing.T) {
	cases := []struct {
		name   string
		err    error
		reason FailoverReason
	}{
		{"rate limit", &ProviderError{StatusCode: 429, Message: "slow down"}, FailoverRateLimit},
		{"unauthorized", &ProviderError{StatusCode: 401, Message: "bad key"}, FailoverAuth},
		{"forbidden", &ProviderError{StatusCode: 403, Message: "nope"}, FailoverAuth},
		{"server error", &ProviderError{StatusCode: 503, Message: "unavailable"}, FailoverUnavailable},
		{"bad request", &ProviderError{StatusCode: 400, Message: "malformed"}, FailoverClient},
		{"nil", nil, FailoverNone},
	}
	for _, c := range cases {
		if got := failoverReason(c.err); got != c.reason {
			t.Errorf("%s: failoverReason = %q, want %q", c.name, got, c.reason)
		}
	}
}

func TestFailoverReasonDetectsContextLength(t *testing.T) {
	// Un 400 n'est bascule que s'il signale un depassement de contexte : sinon il
	// signale une requete invalide qu'aucun autre modele ne rendra valide.
	tooLong := &ProviderError{StatusCode: 400, Message: "This model's maximum context length is 8192 tokens"}
	if got := failoverReason(tooLong); got != FailoverContextLength {
		t.Errorf("expected context_length, got %q", got)
	}

	byCode := &ProviderError{StatusCode: 400, Code: "context_length_exceeded", Message: "trop long"}
	if got := failoverReason(byCode); got != FailoverContextLength {
		t.Errorf("expected context_length from code, got %q", got)
	}

	invalid := &ProviderError{StatusCode: 400, Message: "invalid json in messages[2]"}
	if got := failoverReason(invalid); got != FailoverClient {
		t.Errorf("expected client error, got %q", got)
	}
}

func TestFailoverReasonStopsOnClientCancellation(t *testing.T) {
	// Une requete annulee n'est pas une saturation provider : basculer
	// relancerait un travail que personne n'attend plus.
	cancelled := &ProviderError{StatusCode: 429, Message: "rate limited"}
	wrapped := errors.Join(context.Canceled, cancelled)
	if got := failoverReason(wrapped); got != FailoverClient {
		t.Errorf("expected no failover on cancellation, got %q", got)
	}
}

// --- Cooldown ---

func TestCooldownExpires(t *testing.T) {
	store := newCooldownStore(time.Minute)
	now := time.Now()
	store.now = func() time.Time { return now }

	store.Mark("ollama/fast")
	if !store.Active("ollama/fast") {
		t.Fatal("expected the key to be in cooldown right after Mark")
	}

	now = now.Add(2 * time.Minute)
	if store.Active("ollama/fast") {
		t.Fatal("expected the cooldown to have expired")
	}
	if store.size() != 0 {
		t.Errorf("expired keys must be dropped, got %d", store.size())
	}
}

func TestCooldownDisabledWhenTTLIsZero(t *testing.T) {
	store := newCooldownStore(0)
	store.Mark("ollama/fast")
	if store.Active("ollama/fast") {
		t.Fatal("a zero TTL must disable the cooldown entirely")
	}
}

// --- Classifieur ---

type stubClassifier struct {
	tier       string
	confidence float64
	err        error
	calls      int
}

func (s *stubClassifier) Classify(context.Context, []domain.ChatMessage) (string, float64, error) {
	s.calls++
	return s.tier, s.confidence, s.err
}

func TestParseVerdictAcceptsDecoratedJSON(t *testing.T) {
	// Les petits modeles locaux entourent frequemment le JSON d'un bloc de code
	// ou d'une phrase : la sortie ne doit pas etre rejetee pour autant.
	cases := []string{
		`{"tier":"fast","confidence":0.9}`,
		"```json\n{\"tier\":\"balanced\",\"confidence\":0.8}\n```",
		`Voici mon verdict : {"tier":"frontier","confidence":0.95} — j'ai analyse la demande.`,
		`  {"tier" : "fast" , "confidence" : 1.4}  `,
	}
	wantTier := []string{domain.TierFast, domain.TierBalanced, domain.TierFrontier, domain.TierFast}
	for i, raw := range cases {
		tier, confidence, err := parseVerdict(raw)
		if err != nil {
			t.Fatalf("case %d: %v", i, err)
		}
		if tier != wantTier[i] {
			t.Errorf("case %d: tier = %q, want %q", i, tier, wantTier[i])
		}
		if confidence < 0 || confidence > 1 {
			t.Errorf("case %d: confidence %f out of range", i, confidence)
		}
	}
}

func TestParseVerdictRejectsUnknownTier(t *testing.T) {
	// Un tier invente ne doit pas etre devine : le routeur garderait sa config.
	if _, _, err := parseVerdict(`{"tier":"turbo","confidence":0.9}`); err == nil {
		t.Fatal("expected an error for an unknown tier")
	}
}

func TestParseVerdictRejectsGarbage(t *testing.T) {
	for _, raw := range []string{"", "pas du json", "[]"} {
		if _, _, err := parseVerdict(raw); err == nil {
			t.Errorf("expected an error for %q", raw)
		}
	}
}

func TestLocalClassifierTruncatesHistoryFromTheEnd(t *testing.T) {
	// Une HugePiece de texte ferait exploser l'appel sans rien apprendre de plus :
	// le plafond s'applique en gardant la fin, qui porte la demande courante.
	c := &LocalClassifier{cfg: ClassifierSettings{MaxInputChars: 40, MaxTokens: 64}}
	msgs := []domain.ChatMessage{
		{Role: "user", Content: "Ancient irrelevant chatter"},
		{Role: "assistant", Content: "Ancient answer"},
		{Role: "user", Content: "question recente importante"},
	}

	prompt, err := c.buildPrompt(msgs)
	if err != nil {
		t.Fatalf("buildPrompt: %v", err)
	}
	if !strings.Contains(prompt, "question recente importante") {
		t.Error("the latest message must survive truncation")
	}
	if strings.Contains(prompt, "Ancient irrelevant chatter") {
		t.Error("the oldest message should have been dropped")
	}
	if !strings.Contains(prompt, "[user] question recente") {
		t.Error("the chronological order must be restored after scanning backwards")
	}
}

func TestLocalClassifierForcesJSONAndLowTemperature(t *testing.T) {
	// Le verdict doit etre du JSON strict et reproductible : deux requetes
	// identiques ne peuvent pas aboutir a deux routages differents.
	inner := &countingLLM{reply: `{"tier":"fast","confidence":0.9}`}
	c := NewLocalClassifier(inner, ClassifierSettings{Provider: "ollama", Model: "small", MaxTokens: 64})

	tier, confidence, err := c.Classify(context.Background(), []domain.ChatMessage{{Role: "user", Content: "bonjour"}})
	if err != nil {
		t.Fatalf("Classify: %v", err)
	}
	if tier != domain.TierFast || confidence < 0.9 {
		t.Errorf("unexpected verdict %s / %.2f", tier, confidence)
	}

	cfg := inner.lastCfg
	if cfg.ResponseFormat != "json_object" {
		t.Errorf("expected json_object, got %q", cfg.ResponseFormat)
	}
	if cfg.Temperature == nil || *cfg.Temperature != 0 {
		t.Error("expected a zero temperature for reproducibility")
	}
	if cfg.MaxTokens == nil || *cfg.MaxTokens != 64 {
		t.Error("expected MaxTokens to bound the verdict size")
	}
	if cfg.Tier != "" {
		t.Error("the classifier must not belong to a routing tier itself")
	}
}
