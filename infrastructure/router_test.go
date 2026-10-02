package infrastructure

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"bridge-gateway/domain"
)

// routingFixture monte un projet avec une configuration par tier et fournit un
// provider qui repond ou echoue selon le modele demande.
type routingFixture struct {
	router *RoutingLLMProvider
	inner  *modelAwareLLM
	store  *ChatConfigStore
}

func newRoutingFixture(t *testing.T, tiers map[string]string, opts fixtureOpts) *routingFixture {
	t.Helper()

	db, err := OpenDB(filepath.Join(t.TempDir(), "routing.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := Migrate(context.Background(), db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	store := NewChatConfigStore(db)
	ctx := context.Background()
	now := time.Now().UTC()

	for tier, model := range tiers {
		cfg := &domain.ChatConfig{
			ID:        "cfg-" + tier,
			ProjectID: "p1",
			Name:      tier,
			Provider:  "ollama",
			Model:     model,
			Tier:      tier,
			CreatedAt: now,
			UpdatedAt: now,
		}
		if err := store.Create(ctx, cfg); err != nil {
			t.Fatalf("create %s: %v", tier, err)
		}
	}

	inner := &modelAwareLLM{}
	classifier := &stubClassifier{tier: opts.tier(), confidence: opts.confidence()}
	router := NewRoutingLLMProvider(inner, classifier, store, nil, opts.routing())

	return &routingFixture{router: router, inner: inner, store: store}
}

// modelAwareLLM repond avec le nom du modele qui a servi, ce qui permet
// d'affirmer quelle cible a ete choisie sans inspecter les logs.
type modelAwareLLM struct {
	served  []string
	failing map[string]error
	calls   int
}

func (m *modelAwareLLM) Chat(_ context.Context, cfg *domain.ChatConfig, _ []domain.ChatMessage) (domain.LLMResult, error) {
	m.calls++
	m.served = append(m.served, cfg.Model)
	if err, ok := m.failing[cfg.Model]; ok {
		return domain.LLMResult{}, err
	}
	return domain.LLMResult{Content: "reponse de " + cfg.Model, Model: cfg.Model}, nil
}

func (m *modelAwareLLM) tried(model string) bool {
	for _, s := range m.served {
		if s == model {
			return true
		}
	}
	return false
}

type fixtureOpts struct {
	opts     RoutingOptions
	fromTier string
	conf     float64
}

func (o fixtureOpts) routing() RoutingOptions {
	if o.opts.Cooldown == 0 {
		o.opts.Cooldown = time.Minute
	}
	if o.opts.MinConfidence == 0 {
		o.opts.MinConfidence = 0.5
	}
	return o.opts
}

func (o fixtureOpts) tier() string {
	if o.fromTier == "" {
		return domain.TierFast
	}
	return o.fromTier
}

func (o fixtureOpts) confidence() float64 {
	if o.conf == 0 {
		return 0.9
	}
	return o.conf
}

func rateLimited(model string) error {
	return &ProviderError{StatusCode: 429, Message: "quota exhausted for " + model}
}

func activeConfig(tier, model string) *domain.ChatConfig {
	return &domain.ChatConfig{ID: "cfg-" + tier, ProjectID: "p1", Provider: "ollama", Model: model, Tier: tier}
}

// Sans tier dans le projet, le routeur ne doit rien changer : c'est la
// non-regression la plus importante, car le routage est desactive par defaut.
func TestRoutingWithoutTiersFallsBackToActiveConfig(t *testing.T) {
	inner := &modelAwareLLM{}
	db, err := OpenDB(filepath.Join(t.TempDir(), "routing.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	if err := Migrate(context.Background(), db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	store := NewChatConfigStore(db)
	active := &domain.ChatConfig{ID: "c1", ProjectID: "p1", Provider: "ollama", Model: "seul-modele"}
	if err := store.Create(context.Background(), active); err != nil {
		t.Fatalf("create: %v", err)
	}

	router := NewRoutingLLMProvider(inner, &stubClassifier{tier: domain.TierFrontier, confidence: 1},
		store, nil, RoutingOptions{MinConfidence: 0.5, Cooldown: time.Minute})

	result, err := router.Chat(context.Background(), active, []domain.ChatMessage{{Role: "user", Content: "salut"}})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if result.Model != "seul-modele" {
		t.Errorf("expected the active config to be used, got %q", result.Model)
	}
	if inner.calls != 1 {
		t.Errorf("expected exactly one call, got %d", inner.calls)
	}
}

// Le classifieur fixe le point de depart : une demande complexe ne doit pas
// partir du modele le plus econome.
func TestRoutingStartsAtClassifierTier(t *testing.T) {
	f := newRoutingFixture(t, map[string]string{
		domain.TierFast:     "m-fast",
		domain.TierBalanced: "m-balanced",
		domain.TierFrontier: "m-frontier",
	}, fixtureOpts{fromTier: domain.TierBalanced})

	result, err := f.router.Chat(context.Background(), activeConfig(domain.TierBalanced, "m-balanced"),
		[]domain.ChatMessage{{Role: "user", Content: "conception d'une architecture"}})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if result.Model != "m-balanced" {
		t.Errorf("expected to start at balanced, got %q", result.Model)
	}
	if f.inner.tried("m-fast") {
		t.Error("a cheaper tier must not be tried when the classifier asked for balanced")
	}
}

// Le system prompt et les reglages de sampling restent ceux de la configuration
// active : changer de modele ne doit pas changer le comportement de la
// conversation.
func TestRoutingKeepsActiveSettings(t *testing.T) {
	f := newRoutingFixture(t, map[string]string{
		domain.TierBalanced: "m-balanced",
		domain.TierFrontier: "m-frontier",
	}, fixtureOpts{fromTier: domain.TierBalanced})

	temp := 0.3
	maxTokens := 256
	active := activeConfig(domain.TierBalanced, "m-balanced")
	active.SystemPrompt = "tu es un assistant strict"
	active.Temperature = &temp
	active.MaxTokens = &maxTokens
	active.ResponseFormat = "json_object"

	// La config du tier cible porte des reglages differents, qui ne doivent pas
	// etre adoptes.
	target := &domain.ChatConfig{Provider: "ollama", Model: "m-frontier", Tier: domain.TierFrontier, SystemPrompt: "autre prompt"}
	merged := f.router.applyTier(context.Background(), active, target)

	if merged.SystemPrompt != active.SystemPrompt {
		t.Errorf("system prompt changed: %q", merged.SystemPrompt)
	}
	if merged.Temperature == nil || *merged.Temperature != 0.3 {
		t.Error("temperature must come from the active config")
	}
	if merged.MaxTokens == nil || *merged.MaxTokens != 256 {
		t.Error("max_tokens must come from the active config")
	}
	if merged.ResponseFormat != "json_object" {
		t.Errorf("response_format must come from the active config, got %q", merged.ResponseFormat)
	}
	if merged.Model != "m-frontier" {
		t.Errorf("expected the target model, got %q", merged.Model)
	}
}

// Le mecanisme central : sur saturation, on monte d'un cran et la reponse finit
// par etre servie par un modele plus puissant.
func TestRoutingEscalatesOnSaturation(t *testing.T) {
	f := newRoutingFixture(t, map[string]string{
		domain.TierFast:     "m-fast",
		domain.TierBalanced: "m-balanced",
		domain.TierFrontier: "m-frontier",
	}, fixtureOpts{fromTier: domain.TierFast})
	f.inner.failing = map[string]error{"m-fast": rateLimited("m-fast")}

	result, err := f.router.Chat(context.Background(), activeConfig(domain.TierFast, "m-fast"),
		[]domain.ChatMessage{{Role: "user", Content: "question simple"}})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if result.Model != "m-balanced" {
		t.Errorf("expected escalation to balanced, got %q", result.Model)
	}
	if !f.inner.tried("m-fast") || !f.inner.tried("m-balanced") {
		t.Errorf("expected fast then balanced to be tried, got %v", f.inner.served)
	}
	if f.inner.tried("m-frontier") {
		t.Error("escalation must stop at the first tier that answers")
	}
}

// Chaque declencheur de saturation doit entrainer une bascule.
func TestRoutingEscalatesOnEverySaturationReason(t *testing.T) {
	reasons := []error{
		&ProviderError{StatusCode: 429, Message: "rate limited"},
		&ProviderError{StatusCode: 503, Message: "upstream down"},
		&ProviderError{StatusCode: 401, Message: "invalid key"},
		&ProviderError{StatusCode: 403, Message: "forbidden"},
		&ProviderError{StatusCode: 400, Message: "maximum context length is 8192 tokens"},
	}
	for _, reason := range reasons {
		t.Run(string(failoverReason(reason)), func(t *testing.T) {
			f := newRoutingFixture(t, map[string]string{
				domain.TierFast:     "m-fast",
				domain.TierFrontier: "m-frontier",
			}, fixtureOpts{fromTier: domain.TierFast})
			f.inner.failing = map[string]error{"m-fast": reason}

			result, err := f.router.Chat(context.Background(), activeConfig(domain.TierFast, "m-fast"),
				[]domain.ChatMessage{{Role: "user", Content: "salut"}})
			if err != nil {
				t.Fatalf("Chat: %v", err)
			}
			if result.Model != "m-frontier" {
				t.Errorf("expected escalation, got %q", result.Model)
			}
		})
	}
}

// Une requete invalide ne sera pas rendue valide par un autre modele : inutile
// de consommer les trois tiers.
func TestRoutingDoesNotEscalateOnClientError(t *testing.T) {
	f := newRoutingFixture(t, map[string]string{
		domain.TierFast:     "m-fast",
		domain.TierBalanced: "m-balanced",
		domain.TierFrontier: "m-frontier",
	}, fixtureOpts{fromTier: domain.TierFast})
	f.inner.failing = map[string]error{"m-fast": &ProviderError{StatusCode: 400, Message: "invalid json in messages[2]"}}

	_, err := f.router.Chat(context.Background(), activeConfig(domain.TierFast, "m-fast"),
		[]domain.ChatMessage{{Role: "user", Content: "salut"}})
	if err == nil {
		t.Fatal("expected an error")
	}
	if f.inner.calls != 1 {
		t.Errorf("a client error must not be retried on other tiers, got %d calls", f.inner.calls)
	}
}

func TestRoutingAggregatesFailureWhenEveryTierFails(t *testing.T) {
	f := newRoutingFixture(t, map[string]string{
		domain.TierFast:     "m-fast",
		domain.TierBalanced: "m-balanced",
	}, fixtureOpts{fromTier: domain.TierFast})
	f.inner.failing = map[string]error{
		"m-fast":     rateLimited("m-fast"),
		"m-balanced": &ProviderError{StatusCode: 503, Message: "down"},
	}

	_, err := f.router.Chat(context.Background(), activeConfig(domain.TierFast, "m-fast"),
		[]domain.ChatMessage{{Role: "user", Content: "salut"}})
	if err == nil {
		t.Fatal("expected an error when every tier fails")
	}
	msg := err.Error()
	for _, want := range []string{"m-fast", "m-balanced", "rate_limit", "unavailable"} {
		if !strings.Contains(msg, want) {
			t.Errorf("the aggregated error should mention %q, got: %s", want, msg)
		}
	}
}

// Le cooldown evite de rebruiter un modele mort sur chaque requete.
func TestCooldownSkipsSaturatedTierOnNextRequest(t *testing.T) {
	f := newRoutingFixture(t, map[string]string{
		domain.TierFast:     "m-fast",
		domain.TierBalanced: "m-balanced",
	}, fixtureOpts{fromTier: domain.TierFast, opts: RoutingOptions{Cooldown: time.Minute}})
	f.inner.failing = map[string]error{"m-fast": rateLimited("m-fast")}

	active := activeConfig(domain.TierBalanced, "m-balanced")
	for i := 0; i < 2; i++ {
		result, err := f.router.Chat(context.Background(), active, []domain.ChatMessage{{Role: "user", Content: fmt.Sprintf("q%d", i)}})
		if err != nil {
			t.Fatalf("request %d: %v", i, err)
		}
		if result.Model != "m-balanced" {
			t.Errorf("request %d: expected balanced, got %q", i, result.Model)
		}
	}

	if f.inner.tried("m-fast") {
		t.Errorf("the saturated tier should have been skipped on the second request, calls: %v", f.inner.served)
	}
}

// Un cooldown qui vide toute l'echelle ne doit pas empecher de repondre.
func TestCooldownNeverEmptiesTheLadder(t *testing.T) {
	f := newRoutingFixture(t, map[string]string{domain.TierFast: "m-fast"}, fixtureOpts{fromTier: domain.TierFast})

	// Les deux cibles sont en cooldown.
	f.router.cooldowns.Mark(cooldownKey(&domain.ChatConfig{Provider: "ollama", Model: "m-fast"}))

	result, err := f.router.Chat(context.Background(), activeConfig(domain.TierFast, "m-fast"),
		[]domain.ChatMessage{{Role: "user", Content: "salut"}})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if result.Model != "m-fast" {
		t.Errorf("expected the only tier to be retried, got %q", result.Model)
	}
}

// Un classifieur en panne ne doit jamais empecher une reponse : le routeur
// conserve alors le rang de la configuration active.
func TestRoutingSurvivesClassifierFailure(t *testing.T) {
	for _, tc := range []struct {
		name  string
		build func() domain.Classifier
	}{
		{"erreur", func() domain.Classifier { return &stubClassifier{err: fmt.Errorf("ollama injoignable")} }},
		{"tier inconnu", func() domain.Classifier { return &stubClassifier{tier: "turbo", confidence: 1} }},
		{"confiance faible", func() domain.Classifier { return &stubClassifier{tier: domain.TierFrontier, confidence: 0.1} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newRoutingFixture(t, map[string]string{
				domain.TierFast:     "m-fast",
				domain.TierFrontier: "m-frontier",
			}, fixtureOpts{fromTier: domain.TierFast})
			f.router.classifier = tc.build()

			active := activeConfig(domain.TierFrontier, "m-frontier")
			result, err := f.router.Chat(context.Background(), active, []domain.ChatMessage{{Role: "user", Content: "salut"}})
			if err != nil {
				t.Fatalf("Chat must still succeed: %v", err)
			}
			if result.Model != "m-frontier" {
				t.Errorf("expected the active config to be kept, got %q", result.Model)
			}
		})
	}
}

// Le classifieur ne peut pas envoyer une requete sous le tier que l'utilisateur a
// choisi explicitement.
func TestClassifierCannotDowngradeBelowActiveTier(t *testing.T) {
	f := newRoutingFixture(t, map[string]string{
		domain.TierFast:     "m-fast",
		domain.TierFrontier: "m-frontier",
	}, fixtureOpts{fromTier: domain.TierFast})

	result, err := f.router.Chat(context.Background(), activeConfig(domain.TierFrontier, "m-frontier"),
		[]domain.ChatMessage{{Role: "user", Content: "bonjour"}})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if result.Model != "m-frontier" {
		t.Errorf("expected frontier, got %q", result.Model)
	}
}

// Un timeout provider doit entrainer une bascule : le delai concerne ce modele,
// pas la requete, et un autre modele peut repondre dans la meme fenetre.
// Regression : context.DeadlineExceeded etait pris pour une annulation de
// l'appelant, ce qui empechait toute escalade sur saturation.
func TestRoutingEscalatesOnProviderTimeout(t *testing.T) {
	f := newRoutingFixture(t, map[string]string{
		domain.TierFast:     "m-fast",
		domain.TierBalanced: "m-balanced",
	}, fixtureOpts{fromTier: domain.TierFast})
	f.inner.failing = map[string]error{
		"m-fast": fmt.Errorf("bifrost: %w", context.DeadlineExceeded),
	}

	result, err := f.router.Chat(context.Background(), activeConfig(domain.TierFast, "m-fast"),
		[]domain.ChatMessage{{Role: "user", Content: "salut"}})
	if err != nil {
		t.Fatalf("a provider timeout must not be terminal: %v", err)
	}
	if result.Model != "m-balanced" {
		t.Errorf("expected escalation to balanced, got %q", result.Model)
	}
}

func TestFailoverReasonTreatsCancellationAsTerminal(t *testing.T) {
	if got := failoverReason(fmt.Errorf("bifrost: %w", context.Canceled)); got != FailoverClient {
		t.Errorf("a cancelled call must not escalate, got %q", got)
	}
}

// Un classifieur lent doit couter la precision du routage, pas la requete.
func TestClassifierTimeoutDegradesInsteadOfFailing(t *testing.T) {
	slow := &slowLLM{delay: 200 * time.Millisecond}
	c := NewLocalClassifier(slow, ClassifierSettings{
		Provider: "ollama", Model: "small", MaxTokens: 64, Timeout: Duration(20 * time.Millisecond),
	})

	if _, _, err := c.Classify(context.Background(), []domain.ChatMessage{{Role: "user", Content: "salut"}}); err == nil {
		t.Fatal("expected a timeout error so the router falls back to the active config")
	}
}

type slowLLM struct{ delay time.Duration }

func (s *slowLLM) Chat(ctx context.Context, _ *domain.ChatConfig, _ []domain.ChatMessage) (domain.LLMResult, error) {
	select {
	case <-time.After(s.delay):
		return domain.LLMResult{Content: `{"tier":"fast","confidence":0.9}`, Model: "small"}, nil
	case <-ctx.Done():
		return domain.LLMResult{}, ctx.Err()
	}
}

func TestClassifierTimeoutDefaultsToNoBound(t *testing.T) {
	// Sans timeout declare, le classifieur n'impose pas de limite : le provider
	// reste seul juge du delai.
	slow := &slowLLM{delay: 20 * time.Millisecond}
	c := NewLocalClassifier(slow, ClassifierSettings{Provider: "ollama", Model: "small", MaxTokens: 64})

	tier, _, err := c.Classify(context.Background(), []domain.ChatMessage{{Role: "user", Content: "salut"}})
	if err != nil {
		t.Fatalf("expected the call to complete: %v", err)
	}
	if tier != domain.TierFast {
		t.Errorf("unexpected tier %q", tier)
	}
}

// Un tier qui pointe sur un modele inexistant est un defaut de la cible : aucun
// retry sur ce modele ne peut aboutir, mais le tier suivant le peut.
func TestRoutingEscalatesOnUnknownModel(t *testing.T) {
	f := newRoutingFixture(t, map[string]string{
		domain.TierFast:     "m-fast",
		domain.TierBalanced: "m-balanced",
	}, fixtureOpts{fromTier: domain.TierFast})
	f.inner.failing = map[string]error{
		"m-fast": &ProviderError{StatusCode: 404, Message: "model 'm-fast' not found"},
	}

	result, err := f.router.Chat(context.Background(), activeConfig(domain.TierFast, "m-fast"),
		[]domain.ChatMessage{{Role: "user", Content: "salut"}})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if result.Model != "m-balanced" {
		t.Errorf("expected escalation to balanced, got %q", result.Model)
	}
}

// En revanche un 404 sur le chemin de l'API ne concerne pas le modele : le tier
// suivant n'y changerait rien.
func TestRoutingDoesNotEscalateOnWrongEndpoint(t *testing.T) {
	f := newRoutingFixture(t, map[string]string{
		domain.TierFast:     "m-fast",
		domain.TierBalanced: "m-balanced",
	}, fixtureOpts{fromTier: domain.TierFast})
	f.inner.failing = map[string]error{
		"m-fast": &ProviderError{StatusCode: 404, Message: "404 page not found"},
	}

	if _, err := f.router.Chat(context.Background(), activeConfig(domain.TierFast, "m-fast"),
		[]domain.ChatMessage{{Role: "user", Content: "salut"}}); err == nil {
		t.Fatal("expected an error")
	}
	if f.inner.calls != 1 {
		t.Errorf("a wrong endpoint must not be retried on other tiers, got %d calls", f.inner.calls)
	}
}

// cancellingLLM annule le contexte au premier appel, comme le ferait un client
// qui ferme la connexion, puis echoue.
type cancellingLLM struct {
	cancel context.CancelFunc
	calls  int
}

func (c *cancellingLLM) Chat(_ context.Context, _ *domain.ChatConfig, _ []domain.ChatMessage) (domain.LLMResult, error) {
	c.calls++
	c.cancel()
	return domain.LLMResult{}, rateLimited("m-fast")
}

func TestRoutingStopsWhenClientCancels(t *testing.T) {
	db, err := OpenDB(filepath.Join(t.TempDir(), "routing.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	if err := Migrate(context.Background(), db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	store := NewChatConfigStore(db)
	now := time.Now().UTC()
	for _, tier := range []string{domain.TierFast, domain.TierBalanced, domain.TierFrontier} {
		if err := store.Create(context.Background(), &domain.ChatConfig{
			ID: "cfg-" + tier, ProjectID: "p1", Name: tier,
			Provider: "ollama", Model: "m-" + tier, Tier: tier,
			CreatedAt: now, UpdatedAt: now,
		}); err != nil {
			t.Fatalf("create %s: %v", tier, err)
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	inner := &cancellingLLM{cancel: cancel}

	router := NewRoutingLLMProvider(inner, &stubClassifier{tier: domain.TierFast, confidence: 0.9},
		store, nil, RoutingOptions{MinConfidence: 0.5, Cooldown: time.Minute})

	if _, err := router.Chat(ctx, activeConfig(domain.TierFast, "m-fast"),
		[]domain.ChatMessage{{Role: "user", Content: "salut"}}); err == nil {
		t.Fatal("expected an error once the client has gone away")
	}
	if inner.calls != 1 {
		t.Errorf("the escalation must stop on cancellation, got %d calls", inner.calls)
	}
}

func TestMaxAttemptsBoundsTheLadder(t *testing.T) {
	f := newRoutingFixture(t, map[string]string{
		domain.TierFast:     "m-fast",
		domain.TierBalanced: "m-balanced",
		domain.TierFrontier: "m-frontier",
	}, fixtureOpts{fromTier: domain.TierFast, opts: RoutingOptions{MaxAttempts: 2, Cooldown: time.Minute}})
	f.inner.failing = map[string]error{"m-fast": rateLimited("m-fast"), "m-balanced": rateLimited("m-balanced")}

	if _, err := f.router.Chat(context.Background(), activeConfig(domain.TierFast, "m-fast"),
		[]domain.ChatMessage{{Role: "user", Content: "salut"}}); err == nil {
		t.Fatal("expected an error once the budget is exhausted")
	}
	if f.inner.calls != 2 {
		t.Errorf("expected exactly 2 attempts, got %d", f.inner.calls)
	}
}

// Le cache doit entourer le routeur : sa cle est alors calculee sur la
// configuration active, et un hit evite la classification.
func TestCacheWrappingRouterAvoidsClassification(t *testing.T) {
	f := newRoutingFixture(t, map[string]string{
		domain.TierBalanced: "m-balanced",
	}, fixtureOpts{fromTier: domain.TierBalanced})

	cache := NewMemoryCache(MemoryCacheOptions{})
	defer cache.Stop()
	outer := NewCachedLLMProvider(f.router, cache, 5*time.Minute)

	active := activeConfig(domain.TierBalanced, "m-balanced")
	msgs := []domain.ChatMessage{{Role: "user", Content: "identique"}}

	first, err := outer.Chat(context.Background(), active, msgs)
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	second, err := outer.Chat(context.Background(), active, msgs)
	if err != nil {
		t.Fatalf("second: %v", err)
	}

	if f.router.classifier.(*stubClassifier).calls != 1 {
		t.Errorf("a cache hit must not re-classify, classifier called %d times",
			f.router.classifier.(*stubClassifier).calls)
	}
	if second.Model != first.Model || second.Content != first.Content {
		t.Error("the cached entry must preserve both the content and the served model")
	}
}
