package chat

import (
	"context"
	"errors"
	"testing"

	"bridge-gateway/domain"
)

type stubConfigRepo struct {
	domain.ChatConfigRepository
	cfg *domain.ChatConfig
}

func (s *stubConfigRepo) Get(context.Context, string) (*domain.ChatConfig, error) {
	return s.cfg, nil
}

type stubLLM struct {
	result  domain.LLMResult
	err     error
	calls   int
	cfg     *domain.ChatConfig
	history []domain.ChatMessage
}

func (s *stubLLM) Chat(_ context.Context, cfg *domain.ChatConfig, messages []domain.ChatMessage) (domain.LLMResult, error) {
	s.calls++
	s.cfg = cfg
	s.history = messages
	return s.result, s.err
}

func TestSendPrependsSystemPromptAndAppendsReply(t *testing.T) {
	cfg := &domain.ChatConfig{
		ID:           "cfg-1",
		Provider:     "ollama",
		Model:        "test-model",
		SystemPrompt: "be brief",
	}
	llm := &stubLLM{result: domain.LLMResult{Content: "reply", Model: "served-model"}}
	uc := NewChatUseCase(&stubConfigRepo{cfg: cfg}, domain.NewEnrichmentService(), llm)

	result, err := uc.Send(context.Background(), "cfg-1",
		[]domain.ChatMessage{{Role: "user", Content: "previous"}},
		"hello", nil)
	if err != nil {
		t.Fatalf("Send: %v", err)
	}

	if llm.calls != 1 {
		t.Fatalf("provider calls = %d, want 1", llm.calls)
	}
	if len(llm.history) != 3 {
		t.Fatalf("provider history length = %d, want 3", len(llm.history))
	}
	if llm.history[0].Role != "system" || llm.history[0].Content != "be brief" {
		t.Fatalf("system prompt not injected: %+v", llm.history[0])
	}
	if llm.history[2].Content != "hello" {
		t.Fatalf("current message not last: %+v", llm.history)
	}

	if len(result.Messages) != 3 {
		t.Fatalf("result history length = %d, want 3", len(result.Messages))
	}
	if last := result.Messages[2]; last.Role != "assistant" || last.Content != "reply" {
		t.Fatalf("assistant reply not appended: %+v", last)
	}
	if result.Model != "served-model" || result.RequestedModel != "test-model" {
		t.Fatalf("models = %q/%q", result.Model, result.RequestedModel)
	}
}

func TestSendRejectsBlankMessageWithoutCallingProvider(t *testing.T) {
	cfg := &domain.ChatConfig{ID: "cfg-1", Provider: "ollama", Model: "m"}
	llm := &stubLLM{}
	uc := NewChatUseCase(&stubConfigRepo{cfg: cfg}, domain.NewEnrichmentService(), llm)

	_, err := uc.Send(context.Background(), "cfg-1", nil, "   ", nil)
	var validation *domain.ValidationError
	if !errors.As(err, &validation) {
		t.Fatalf("error = %v, want ValidationError", err)
	}
	if llm.calls != 0 {
		t.Fatalf("provider was called %d times for a blank message", llm.calls)
	}
}

func TestConversationCapsHistory(t *testing.T) {
	history := make([]domain.ChatMessage, maxHistory+5)
	for i := range history {
		history[i] = domain.ChatMessage{Role: "user", Content: "old"}
	}
	messages := conversation(history, "new")
	if len(messages) != maxHistory+1 {
		t.Fatalf("messages = %d, want %d", len(messages), maxHistory+1)
	}
	if last := messages[len(messages)-1]; last.Content != "new" {
		t.Fatalf("last message = %+v", last)
	}
}
