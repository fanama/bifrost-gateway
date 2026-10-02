package infrastructure

import (
	"context"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	"bridge-gateway/domain"
)

// RoutingLLMProvider dirige une requete vers le modele le plus economique qui
// suffise, puis escalade vers les tiers superieurs si le provider sature.
//
// Deux mecanismes cohabitent et poussent dans le meme sens :
//
//   - le classifieur choisit le tier de depart d'apres la complexite de la
//     conversation ;
//   - la saturation (rate limit, quota, contexte trop long, panne, cle
//     refusee) fait monter d'un cran.
//
// Comme ils vont tous deux vers le haut, ils se composent sans conflit : un tier
// choisi pour sa complexite peut encore etre ecarte pour indisponibilite.
//
// Le decorateur ne touche que provider, modele, identifiants et BaseURL. Le
// system prompt et les reglages de sampling restent ceux de la configuration
// active : changer de modele ne doit pas changer silencieusement le comportement
// de la conversation en cours.
type RoutingLLMProvider struct {
	inner      domain.LLMProvider
	classifier domain.Classifier
	configs    domain.ChatConfigRepository
	providers  domain.ProviderRepository

	minConfidence float64
	cooldowns     *cooldownStore
	maxAttempts   int
}

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

// RoutingOptions regroupe les reglages du routeur, derives de config.yaml.
type RoutingOptions struct {
	MinConfidence float64
	Cooldown      time.Duration
	MaxAttempts   int
}

// Chat selectionne une cible puis tente les tiers en ascending jusqu'a reussir.
//
// La configuration active est toujours le premier essai : si le routage est
// desactive, si le classifieur echoue ou si aucune config n'a de tier, le
// comportement est exactement celui d'avant.
func (p *RoutingLLMProvider) Chat(ctx context.Context, cfg *domain.ChatConfig, messages []domain.ChatMessage) (domain.LLMResult, error) {
	ladder := p.ladder(ctx, cfg, messages)
	if len(ladder) == 0 {
		// Aucun tier en echelle : on ne touche a rien.
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

		merged := p.applyTier(ctx, cfg, target)
		if merged == nil {
			// Cible sans identifiants exploitables : inutile de tenter l'appel.
			attempts = append(attempts, fmt.Sprintf("%s/%s (identifiants manquants)", target.Provider, target.Model))
			continue
		}

		result, err := p.inner.Chat(ctx, merged, messages)
		if err == nil {
			if len(attempts) > 0 {
				logRoutingFailover(cfg.Model, result.Model, attempts)
			}
			return result, nil
		}

		reason := failoverReason(err)
		attempts = append(attempts, fmt.Sprintf("%s/%s (%s)", target.Provider, target.Model, reason))
		lastErr = err

		// L'appelant est parti : plus personne n'attend la reponse, et relancer
		// sur un autre modele consommerait des credits pour rien. C'est le
		// contexte de la requete qui tranche, car un timeout provider se presente
		// lui aussi comme context.DeadlineExceeded.
		if ctx.Err() != nil {
			break
		}

		if reason == FailoverClient {
			// Erreur imputable a la requete elle-meme : aucun autre modele ne la
			// rendra valide, on s'arrete pour ne pas multiplier les appels.
			return domain.LLMResult{}, p.aggregateError(cfg, attempts, err)
		}

		// Une cle refusee signale une configuration erronee plus qu'une
		// saturation : elle reste visible dans les logs.
		if reason == FailoverAuth {
			log.Printf("routing: auth failure on %s/%s, escalating (check the API key for this configuration)", target.Provider, target.Model)
		}
		log.Printf("routing: %s/%s unavailable (%s), escalating", target.Provider, target.Model, reason)
		p.cooldownTier(target)
	}

	if lastErr == nil {
		lastErr = fmt.Errorf("no routing candidate was callable")
	}
	return domain.LLMResult{}, p.aggregateError(cfg, attempts, lastErr)
}

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
	}
	return &out
}

// cooldownKey identifie une cible de routage. Le tier n'en fait pas partie :
// deux configurations d'un meme provider et modele sont la meme porteuse de
// capacite.
func cooldownKey(target *domain.ChatConfig) string {
	return strings.ToLower(target.Provider) + "/" + strings.ToLower(target.Model)
}

func (p *RoutingLLMProvider) cooldownTier(target *domain.ChatConfig) {
	p.cooldowns.Mark(cooldownKey(target))
}

func (p *RoutingLLMProvider) inCooldown(target *domain.ChatConfig) bool {
	return p.cooldowns.Active(cooldownKey(target))
}

// aggregateError rend compte de toutes les tentatives plutot que de la seule
// derniere : c'est la seule information utile pour comprendre un echec de
// bout en bout.
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
