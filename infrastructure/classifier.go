package infrastructure

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"bridge-gateway/domain"
)

// LocalClassifier evalue la complexite d'une conversation en interrogeant un
// petit modele local via Bifrost.
//
// Aucun appel reseau sortant n'est requis : le modele tourne generalement sur la
// meme machine (Ollama), donc le routage ne coute rien et reste disponible hors
// ligne. Le prix est une latence additionnelle de l'ordre de la seconde par
// appel non mis en cache.
//
// Le classifieur n'est jamais bloquant : toute erreur, sortie illisible ou
// confiance insuffisante conduit l'appelant a conserver sa configuration.
type LocalClassifier struct {
	// inner est le provider Bifrost brut. Il ne doit jamais etre le provider
	// decoré du routeur, sinon le classifieur se rappellerait lui-meme.
	inner domain.LLMProvider
	cfg   ClassifierSettings
}

func NewLocalClassifier(inner domain.LLMProvider, cfg ClassifierSettings) *LocalClassifier {
	return &LocalClassifier{inner: inner, cfg: cfg}
}

const classifierSystemPrompt = `Tu routes des requetes vers le modele le plus economique qui suffise.

Reponds UNIQUEMENT par un objet JSON, sans texte autour, avec exactement ces deux cles :
{"tier": "<valeur>", "confidence": <nombre entre 0 et 1>}

Valeurs possibles pour "tier" :
- "fast"    : question courte, factuelle, redaction simple, traduction, resume, formatage.
- "balanced": raisonnement multi-etapes, code, maths, analyse de plusieurs documents.
- "frontier": conception d'architecture, recherche ou conception ambigue, maths avancées, long contexte, multiples contraintes a respecter.

"confidence" est ta certitude sur ce choix, entre 0 et 1. Sois honnete : si le cas est limite entre deux valeurs, baisse la confiance.`

// Classify demande au modele local un tier et un niveau de confiance.
//
// L'historique complet est evalue plutot que le dernier message : plus une
// conversation est longue et riche, plus elle exige de puissance. Cela rend
// l'escalade naturelle au fil d'une session.
//
// L'appel est borne par un timeout. Un classifieur lent sur une machine chargee
// doit coûter la precision du routage, pas la reponse entiere : au-dela du
// delai, l'erreur remonte et le routeur conserve la configuration active.
func (c *LocalClassifier) Classify(ctx context.Context, messages []domain.ChatMessage) (string, float64, error) {
	if c.inner == nil {
		return "", 0, fmt.Errorf("classifier: no provider")
	}
	if len(messages) == 0 {
		return "", 0, fmt.Errorf("classifier: no messages")
	}

	prompt, err := c.buildPrompt(messages)
	if err != nil {
		return "", 0, err
	}

	cfg := c.config()

	maxTokens := c.cfg.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 64
	}

	// Le classifieur doit produire du JSON strict, et sa temperature est nulle
	// pour que deux requetes identiques donnent le meme verdict.
	cfg.ResponseFormat = "json_object"
	cfg.MaxTokens = &maxTokens
	temp := 0.0
	cfg.Temperature = &temp

	if timeout := c.cfg.Timeout; timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(timeout))
		defer cancel()
	}

	result, err := c.inner.Chat(ctx, cfg, []domain.ChatMessage{{Role: "system", Content: classifierSystemPrompt}, {Role: "user", Content: prompt}})
	if err != nil {
		return "", 0, fmt.Errorf("classifier call: %w", err)
	}

	tier, confidence, err := parseVerdict(result.Content)
	if err != nil {
		return "", 0, err
	}
	return tier, confidence, nil
}

// buildPrompt resume la conversation. L'historique est tronque par nombre de
// caracteres plutot que par nombre de messages : une seule HugePiece de texte
// ferait exploser l'appel sans rien apprendre de plus sur la complexite.
func (c *LocalClassifier) buildPrompt(messages []domain.ChatMessage) (string, error) {
	limit := c.cfg.MaxInputChars
	if limit <= 0 {
		limit = 6000
	}

	var b strings.Builder
	b.WriteString("Conversation a classer :\n\n")

	used := 0
	// On parcourt a l'envers pour Privilegier la fin de la conversation, qui
	// porte la demande courante, puis on restaure l'ordre chronologique.
	selected := make([]domain.ChatMessage, 0, len(messages))
	for i := len(messages) - 1; i >= 0; i-- {
		content := strings.TrimSpace(messages[i].Content)
		if content == "" {
			continue
		}
		if used+len(content) > limit {
			break
		}
		used += len(content)
		selected = append(selected, domain.ChatMessage{Role: messages[i].Role, Content: content})
	}
	for i, j := 0, len(selected)-1; i < j; i, j = i+1, j-1 {
		selected[i], selected[j] = selected[j], selected[i]
	}
	if len(selected) == 0 {
		return "", fmt.Errorf("classifier: no usable message content")
	}

	for _, m := range selected {
		label := m.Role
		if label == "" {
			label = "user"
		}
		b.WriteString("[" + label + "] " + m.Content + "\n\n")
	}
	b.WriteString("Quel tier pour cette conversation ?")
	return b.String(), nil
}

// config fabrique une ChatConfig dediee au classifieur. Elle ne porte pas de
// tier : c'est lui qui attribue les tiers, il ne doit pas en etre un.
func (c *LocalClassifier) config() *domain.ChatConfig {
	return &domain.ChatConfig{
		Name:     "classifier",
		Provider: c.cfg.Provider,
		Model:    c.cfg.Model,
		BaseURL:  c.cfg.BaseURL,
		APIKey:   c.cfg.APIKey,
	}
}

// verdict est la reponse attendue du modele de classification.
type verdict struct {
	Tier       string  `json:"tier"`
	Confidence float64 `json:"confidence"`
}

func parseVerdict(content string) (string, float64, error) {
	raw := strings.TrimSpace(content)
	if raw == "" {
		return "", 0, fmt.Errorf("classifier: empty verdict")
	}

	// Le modele peut entourer le JSON d'un ```json ... ``` ou d'une phrase :
	// on isole le premier objet equilibre avant de deserialiser.
	if start := strings.Index(raw, "{"); start >= 0 {
		if end := strings.LastIndex(raw, "}"); end > start {
			raw = raw[start : end+1]
		}
	}

	var v verdict
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		return "", 0, fmt.Errorf("classifier: unparsable verdict: %w", err)
	}

	tier := domain.TierRankOf(v.Tier)
	if tier < 0 {
		// Le modele a invente un tier inconnu : on ne devine pas, l'appelant
		// gardera sa configuration.
		return "", 0, fmt.Errorf("classifier: unknown tier %q", v.Tier)
	}

	confidence := v.Confidence
	if confidence < 0 {
		confidence = 0
	}
	if confidence > 1 {
		confidence = 1
	}
	return strings.ToLower(strings.TrimSpace(v.Tier)), confidence, nil
}

var _ domain.Classifier = (*LocalClassifier)(nil)
