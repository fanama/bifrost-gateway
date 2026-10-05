package application

import (
	"context"
	"strings"

	"bridge-gateway/domain"
)

type ChatUseCase struct {
	configs domain.ChatConfigRepository
	enrich  *domain.EnrichmentService
	llm     domain.LLMProvider
}

func NewChatUseCase(
	configs domain.ChatConfigRepository,
	enrich *domain.EnrichmentService,
	llm domain.LLMProvider,
) *ChatUseCase {
	return &ChatUseCase{configs: configs, enrich: enrich, llm: llm}
}

const maxHistory = 40

// ChatResult porte l'historique complete de la conversation et le modele qui a
// reellement produit la reponse.
//
// Model est distinct du modele de la configuration demandee : le routeur peut
// avoir substitue un autre tier, et l'appelant doit pouvoir l'annoncer.
type ChatResult struct {
	Messages []domain.ChatMessage
	// Model est le modele qui a reellement repondu, RequestedModel celui de la
	// configuration demandee. Ils different des que le routeur substitue un tier.
	Model          string
	RequestedModel string
}

func (u *ChatUseCase) Send(
	ctx context.Context,
	cfgID string,
	history []domain.ChatMessage,
	message string,
	opts *domain.RequestOptions,
) (ChatResult, error) {
	cfg, messages, enriched, err := u.prepare(ctx, cfgID, history, message, opts)
	if err != nil {
		return ChatResult{}, err
	}

	reply, err := u.llm.Chat(ctx, cfg, enriched)
	if err != nil {
		return ChatResult{}, err
	}

	return ChatResult{
		Messages:       append(messages, domain.ChatMessage{Role: "assistant", Content: reply.Content}),
		Model:          reply.Model,
		RequestedModel: cfg.Model,
	}, nil
}

// Stream rend la meme reponse que Send mais fragment par fragment.
//
// Le modele reellement utilise est porte par les fragments eux-memes, car il
// n'est connu qu'une fois le premier recus : il peut differer de cfg.Model si le
// routeur a substitue un tier.
func (u *ChatUseCase) Stream(
	ctx context.Context,
	cfgID string,
	history []domain.ChatMessage,
	message string,
	opts *domain.RequestOptions,
) (<-chan domain.StreamEvent, error) {
	cfg, _, enriched, err := u.prepare(ctx, cfgID, history, message, opts)
	if err != nil {
		return nil, err
	}

	return domain.StreamFrom(ctx, u.llm, cfg, enriched)
}

// prepare factorise la partie commune a Send et Stream : resolution de la
// configuration, validation du message, fenetre d'historique et enrichissement.
// Les deux chemins doivent envoyer exactement la meme conversation au provider,
// sinon un meme prompt repondrait differemment selon le mode.
func (u *ChatUseCase) prepare(
	ctx context.Context,
	cfgID string,
	history []domain.ChatMessage,
	message string,
	opts *domain.RequestOptions,
) (*domain.ChatConfig, []domain.ChatMessage, []domain.ChatMessage, error) {
	stored, err := u.configs.Get(ctx, cfgID)
	if err != nil {
		return nil, nil, nil, err
	}

	// La surcharge est appliquee sur une copie : la configuration en base n'est
	// jamais modifiee par un appel, et le provider ne voit toujours qu'un
	// ChatConfig, surcharge comprise.
	cfg, err := opts.Apply(stored)
	if err != nil {
		return nil, nil, nil, err
	}

	message = strings.TrimSpace(message)
	if message == "" {
		return nil, nil, nil, &domain.ValidationError{Fields: []string{"message"}}
	}

	if len(history) > maxHistory {
		history = history[len(history)-maxHistory:]
	}
	messages := append(append([]domain.ChatMessage{}, history...), domain.ChatMessage{Role: "user", Content: message})

	req := &domain.ChatRequest{
		Model:    cfg.Model,
		Messages: messages,
		Metadata: &domain.RequestMetadata{
			TeamMetadata: &domain.TeamMetadata{
				SystemPrompt: cfg.SystemPrompt,
				DefaultModel: cfg.Model,
			},
		},
		Temperature: cfg.Temperature,
		MaxTokens:   cfg.MaxTokens,

		ResponseFormat: cfg.ResponseFormatMap(),
	}

	enriched, err := u.enrich.Enrich(req)
	if err != nil {
		return nil, nil, nil, err
	}

	return cfg, messages, enriched.Messages, nil
}
