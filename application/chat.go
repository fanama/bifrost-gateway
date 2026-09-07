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

func (u *ChatUseCase) Send(
	ctx context.Context,
	cfgID string,
	history []domain.ChatMessage,
	message string,
) ([]domain.ChatMessage, error) {
	cfg, err := u.configs.Get(ctx, cfgID)
	if err != nil {
		return nil, err
	}

	message = strings.TrimSpace(message)
	if message == "" {
		return nil, &domain.ValidationError{Fields: []string{"message"}}
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
				CostCenter:   cfg.CostCenter,
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
		return nil, err
	}

	reply, err := u.llm.Chat(ctx, cfg, enriched.Messages)
	if err != nil {
		return nil, err
	}

	return append(messages, domain.ChatMessage{Role: "assistant", Content: reply}), nil
}
