package domain

type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ChatRequest struct {
	Model           string            `json:"model"`
	Messages        []ChatMessage     `json:"messages"`
	Metadata        *RequestMetadata  `json:"metadata,omitempty"`
	BifrostMetadata *RequestMetadata  `json:"bifrost_metadata,omitempty"`
	ResponseFormat  map[string]any    `json:"response_format,omitempty"`
	Labels          map[string]string `json:"labels,omitempty"`
	Temperature     *float64          `json:"temperature,omitempty"`
	MaxTokens       *int              `json:"max_tokens,omitempty"`
	Stream          bool              `json:"stream,omitempty"`
}

type EnrichedRequest struct {
	Model          string
	Messages       []ChatMessage
	ResponseFormat map[string]any
	Labels         map[string]string
	Temperature    *float64
	MaxTokens      *int
	Stream         bool
}

func ExtractMetadata(req *ChatRequest) *RequestMetadata {
	if req.Metadata != nil {
		return req.Metadata
	}
	if req.BifrostMetadata != nil {
		return req.BifrostMetadata
	}
	return nil
}
