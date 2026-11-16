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
	msg, fromRaw, generic := bifrostMessage(bifrostErr)
	detail := bifrostDetail(bifrostErr, msg, fromRaw)

	out := &ProviderError{
		Message:    msg,
		Detail:     excerpt(detail, 200),
		Code:       bifrostCode(bifrostErr),
		StatusCode: bifrostStatus(bifrostErr),
	}
	if generic && out.Detail == "" {
		out.Hint = errorHint(out.StatusCode)
	}
	return out
}

func bifrostStatus(e *schemas.BifrostError) int {
	if e.StatusCode == nil {
		return 0
	}
	return *e.StatusCode
}

func bifrostCode(e *schemas.BifrostError) string {
	if e.Error == nil || e.Error.Code == nil {
		return ""
	}
	return strings.TrimSpace(*e.Error.Code)
}

// bifrostMessage returns the provider message when it is readable. Bifrost
// reports an empty or generic "unknown error" when it cannot parse the body,
// so the raw response is inspected before falling back to a status-based
// message. fromRaw reports that the message came from the body (so the body
// must not be repeated as detail), generic reports that no readable message
// was found and the text is a status-based fallback.
func bifrostMessage(e *schemas.BifrostError) (msg string, fromRaw, generic bool) {
	if e.Error != nil {
		msg = strings.TrimSpace(e.Error.Message)
	}
	if !genericProviderMessage(msg) {
		return msg, false, false
	}
	if extracted := rawResponseMessage(e.ExtraFields.RawResponse); extracted != "" {
		return extracted, true, false
	}
	return genericStatusMessage(bifrostStatus(e)), false, true
}

func bifrostDetail(e *schemas.BifrostError, msg string, fromRaw bool) string {
	if e.Error != nil && e.Error.Error != nil {
		if detail := strings.TrimSpace(e.Error.Error.Error()); detail != "" {
			return detail
		}
	}
	if fromRaw {
		return ""
	}
	detail := strings.TrimSpace(rawResponseExcerpt(e.ExtraFields.RawResponse))
	if strings.EqualFold(detail, msg) {
		return ""
	}
	return detail
}

func genericProviderMessage(msg string) bool {
	switch strings.ToLower(strings.TrimSpace(msg)) {
	case "", "error", "unknown", "unknown error":
		return true
	default:
		return false
	}
}

func genericStatusMessage(status int) string {
	switch {
	case status == 400:
		return "the provider rejected the request"
	case status == 401 || status == 403:
		return "the provider refused authentication"
	case status == 404:
		return "the provider could not find the model or endpoint"
	case status == 429:
		return "the provider rate limit was reached"
	case status >= 500:
		return "the provider is unavailable"
	default:
		return "the provider returned an error without details"
	}
}

func errorHint(status int) string {
	switch {
	case status == 400:
		return "check the model name and the parameters sent (response_format, max_tokens, reasoning_effort)"
	case status == 401 || status == 403:
		return "check the API key and its permissions"
	case status == 404:
		return "check the model name and the base URL"
	case status == 429:
		return "wait before retrying or raise the provider quota"
	case status >= 500:
		return "retry later or use another provider"
	default:
		return ""
	}
}

// rawResponseMessage extracts a human message from the usual provider error
// shapes: Mistral's flat {"message":...}, OpenAI's nested {"error":{...}},
// and plain strings.
func rawResponseMessage(raw any) string {
	body := rawResponseExcerpt(raw)
	if body == "" {
		return ""
	}
	var payload any
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		return ""
	}
	return messageFromPayload(payload)
}

func messageFromPayload(v any) string {
	switch typed := v.(type) {
	case string:
		return strings.TrimSpace(typed)
	case []any:
		for _, item := range typed {
			if msg := messageFromPayload(item); msg != "" {
				return msg
			}
		}
	case map[string]any:
		for _, key := range []string{"message", "detail", "error_description", "reason"} {
			if msg, ok := typed[key].(string); ok && strings.TrimSpace(msg) != "" {
				return strings.TrimSpace(msg)
			}
		}
		if nested, ok := typed["error"]; ok {
			if msg := messageFromPayload(nested); msg != "" {
				return msg
			}
		}
	}
	return ""
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
