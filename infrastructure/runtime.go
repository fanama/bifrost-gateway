package infrastructure

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"bridge-gateway/domain"
	"github.com/maximhq/bifrost/core/schemas"
)

type BifrostLLMProvider struct {
	mu      sync.Mutex
	clients map[string]*BifrostClient
}

func NewBifrostLLMProvider() *BifrostLLMProvider {
	return &BifrostLLMProvider{clients: map[string]*BifrostClient{}}
}

func (p *BifrostLLMProvider) clientKey(cfg *domain.ChatConfig) string {
	return fmt.Sprintf("%s:%d", cfg.ID, cfg.UpdatedAt.Unix())
}

func (p *BifrostLLMProvider) clientFor(cfg *domain.ChatConfig) (*BifrostClient, error) {
	key := p.clientKey(cfg)
	p.mu.Lock()
	defer p.mu.Unlock()
	if c, ok := p.clients[key]; ok {
		return c, nil
	}
	client, err := NewBifrostClient(NewDynamicAccount(cfg))
	if err != nil {
		return nil, fmt.Errorf("init bifrost client: %w", err)
	}
	p.clients[key] = client
	return client, nil
}

func (p *BifrostLLMProvider) Chat(ctx context.Context, cfg *domain.ChatConfig, messages []domain.ChatMessage) (string, error) {
	provider, err := ToModelProvider(cfg.Provider)
	if err != nil {
		return "", err
	}

	client, err := p.clientFor(cfg)
	if err != nil {
		return "", err
	}

	bifrostMessages := make([]schemas.ChatMessage, 0, len(messages))
	for _, m := range messages {
		content := m.Content
		bifrostMessages = append(bifrostMessages, schemas.ChatMessage{
			Role:    schemas.ChatMessageRole(m.Role),
			Content: &schemas.ChatMessageContent{ContentStr: &content},
		})
	}

	resp, err := client.ChatCompletion(ctx, provider, cfg.Model, bifrostMessages, chatParams(cfg))
	if err != nil {
		return "", err
	}
	return ExtractAssistantContent(resp), nil
}

func chatParams(cfg *domain.ChatConfig) *schemas.ChatParameters {
	params := &schemas.ChatParameters{
		Temperature:         cfg.Temperature,
		TopP:                cfg.TopP,
		MaxCompletionTokens: cfg.MaxTokens,
		FrequencyPenalty:    cfg.FrequencyPenalty,
		PresencePenalty:     cfg.PresencePenalty,
	}
	if m := cfg.ResponseFormatMap(); m != nil {
		v := any(m)
		params.ResponseFormat = &v
	}
	if params.Temperature == nil &&
		params.TopP == nil &&
		params.MaxCompletionTokens == nil &&
		params.FrequencyPenalty == nil &&
		params.PresencePenalty == nil &&
		params.ResponseFormat == nil {
		return nil
	}
	return params
}

func ExtractAssistantContent(resp *schemas.BifrostChatResponse) string {
	if resp == nil || len(resp.Choices) == 0 {
		return ""
	}
	choice := resp.Choices[0]
	if choice.ChatNonStreamResponseChoice == nil || choice.ChatNonStreamResponseChoice.Message == nil {
		return ""
	}
	msg := choice.ChatNonStreamResponseChoice.Message
	if msg.Content != nil && msg.Content.ContentStr != nil {
		return *msg.Content.ContentStr
	}
	return ""
}

func ToModelProvider(s string) (schemas.ModelProvider, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "ollama":
		return schemas.Ollama, nil
	case "openai":
		return schemas.OpenAI, nil
	case "azure":
		return schemas.Azure, nil
	case "vertex", "vertexai", "google":
		return schemas.Vertex, nil
	}
	return "", fmt.Errorf("provider not supported: %s", s)
}

func defaultBaseURLFor(p schemas.ModelProvider) string {
	if p == schemas.Ollama {
		return "http://localhost:11434"
	}
	return ""
}

type DynamicAccount struct {
	cfg *domain.ChatConfig
}

func NewDynamicAccount(cfg *domain.ChatConfig) *DynamicAccount {
	return &DynamicAccount{cfg: cfg}
}

func (a *DynamicAccount) provider() (schemas.ModelProvider, error) {
	return ToModelProvider(a.cfg.Provider)
}

func (a *DynamicAccount) GetConfiguredProviders() ([]schemas.ModelProvider, error) {
	p, err := a.provider()
	if err != nil {
		return nil, err
	}
	return []schemas.ModelProvider{p}, nil
}

func (a *DynamicAccount) GetKeysForProvider(_ context.Context, _ schemas.ModelProvider) ([]schemas.Key, error) {
	if _, err := a.provider(); err != nil {
		return nil, err
	}
	return []schemas.Key{{
		Name:   a.cfg.Name,
		Value:  schemas.EnvVar{Val: a.cfg.APIKey},
		Models: []string{},
		Weight: 1.0,
	}}, nil
}

func (a *DynamicAccount) GetConfigForProvider(provider schemas.ModelProvider) (*schemas.ProviderConfig, error) {
	baseURL := strings.TrimSpace(a.cfg.BaseURL)
	if baseURL == "" {
		baseURL = defaultBaseURLFor(provider)
	}
	return &schemas.ProviderConfig{
		NetworkConfig: schemas.NetworkConfig{
			BaseURL:                        baseURL,
			DefaultRequestTimeoutInSeconds: 60,
			MaxRetries:                     2,
			RetryBackoffInitial:            500 * time.Millisecond,
			RetryBackoffMax:                5 * time.Second,
		},
		ConcurrencyAndBufferSize: schemas.DefaultConcurrencyAndBufferSize,
	}, nil
}
