package infrastructure

import (
	"context"
	"encoding/json"
	"strings"

	"bridge-gateway/domain/llm"
	bifrost "github.com/maximhq/bifrost/core"
	"github.com/maximhq/bifrost/core/schemas"
)

// ProviderError is an alias to domain/llm.ProviderError so existing callers
// and tests keep working while the error type lives in the domain layer.
type ProviderError = llm.ProviderError

// BifrostClient wraps the Bifrost SDK client for chat, streaming, and embeddings.
type BifrostClient struct {
	client *bifrost.Bifrost
}

// NewBifrostClient initializes a Bifrost SDK client for the given account.
func NewBifrostClient(account schemas.Account) (*BifrostClient, error) {
	client, err := bifrost.Init(context.Background(), schemas.BifrostConfig{
		Account: account,
	})
	if err != nil {
		return nil, err
	}
	return &BifrostClient{client: client}, nil
}

// ChatCompletion executes a synchronous chat completion call via Bifrost.
func (c *BifrostClient) ChatCompletion(
	ctx context.Context,
	provider schemas.ModelProvider,
	model string,
	messages []schemas.ChatMessage,
	params *schemas.ChatParameters,
) (*schemas.BifrostChatResponse, error) {
	req := &schemas.BifrostChatRequest{
		Provider: provider,
		Model:    model,
		Input:    messages,
		Params:   params,
	}

	resp, bifrostErr := c.client.ChatCompletionRequest(
		schemas.NewBifrostContext(ctx, schemas.NoDeadline),
		req,
	)
	if bifrostErr != nil {
		return nil, formatBifrostError(bifrostErr)
	}
	return resp, nil
}

// Embedding executes an embedding call via Bifrost.
func (c *BifrostClient) Embedding(
	ctx context.Context,
	provider schemas.ModelProvider,
	model string,
	input *schemas.EmbeddingInput,
	params *schemas.EmbeddingParameters,
) (*schemas.BifrostEmbeddingResponse, error) {
	req := &schemas.BifrostEmbeddingRequest{
		Provider: provider,
		Model:    model,
		Input:    input,
		Params:   params,
	}

	resp, bifrostErr := c.client.EmbeddingRequest(
		schemas.NewBifrostContext(ctx, schemas.NoDeadline),
		req,
	)
	if bifrostErr != nil {
		return nil, formatBifrostError(bifrostErr)
	}
	return resp, nil
}

func formatBifrostError(bifrostErr *schemas.BifrostError) error {
	msg := "unknown error"
	if bifrostErr.Error != nil && bifrostErr.Error.Message != "" {
		msg = bifrostErr.Error.Message
	}

	var code string
	if bifrostErr.Error != nil && bifrostErr.Error.Code != nil {
		code = strings.TrimSpace(*bifrostErr.Error.Code)
	}

	var detail string
	if bifrostErr.Error != nil && bifrostErr.Error.Error != nil {
		detail = strings.TrimSpace(bifrostErr.Error.Error.Error())
	}
	if detail == "" {
		detail = rawResponseExcerpt(bifrostErr.ExtraFields.RawResponse)
	}

	out := &ProviderError{Message: msg, Detail: excerpt(detail, 200), Code: code}
	if bifrostErr.StatusCode != nil {
		out.StatusCode = *bifrostErr.StatusCode
	}
	return out
}

func rawResponseExcerpt(raw any) string {
	switch v := raw.(type) {
	case nil:
		return ""
	case string:
		return v
	case []byte:
		return string(v)
	default:
		encoded, err := json.Marshal(v)
		if err != nil {
			return ""
		}
		return string(encoded)
	}
}

func excerpt(s string, max int) string {
	collapsed := strings.Join(strings.Fields(s), " ")
	if max <= 0 || len(collapsed) <= max {
		return collapsed
	}
	return collapsed[:max] + "..."
}

// ChatCompletionStream opens a streaming chat completion channel via Bifrost.
func (c *BifrostClient) ChatCompletionStream(
	ctx context.Context,
	provider schemas.ModelProvider,
	model string,
	messages []schemas.ChatMessage,
	params *schemas.ChatParameters,
) (chan *schemas.BifrostStreamChunk, error) {
	req := &schemas.BifrostChatRequest{
		Provider: provider,
		Model:    model,
		Input:    messages,
		Params:   params,
	}

	chunks, bifrostErr := c.client.ChatCompletionStreamRequest(
		schemas.NewBifrostContext(ctx, schemas.NoDeadline),
		req,
	)
	if bifrostErr != nil {
		return nil, formatBifrostError(bifrostErr)
	}
	return chunks, nil
}
