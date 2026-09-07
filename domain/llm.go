package domain

import "context"

type LLMProvider interface {
	Chat(ctx context.Context, cfg *ChatConfig, messages []ChatMessage) (string, error)
}
