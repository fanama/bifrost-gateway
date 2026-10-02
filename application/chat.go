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
) (ChatResult, error) {
	cfg, err := u.configs.Get(ctx, cfgID)
	if err != nil {
		return ChatResult{}, err
	}

	message = strings.TrimSpace(message)
	if message == "" {
		return ChatResult{}, &domain.ValidationError{Fields: []string{"message"}}
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
		return ChatResult{}, err
	}

	reply, err := u.llm.Chat(ctx, cfg, enriched.Messages)
	if err != nil {
		return ChatResult{}, err
	}

	return ChatResult{
		Messages:       append(messages, domain.ChatMessage{Role: "assistant", Content: reply.Content}),
		Model:          reply.Model,
		RequestedModel: cfg.Model,
	}, nil
}
