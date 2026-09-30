package domain

type EnrichmentService struct{}

func NewEnrichmentService() *EnrichmentService {
	return &EnrichmentService{}
}

func (s *EnrichmentService) Enrich(req *ChatRequest) (*EnrichedRequest, error) {
	rawMetadata := ExtractMetadata(req)
	var attribution AttributionMetadata
	if rawMetadata != nil {
		attribution = rawMetadata.resolve()
	}

	messages := make([]ChatMessage, len(req.Messages))
	copy(messages, req.Messages)

	if attribution.SystemPrompt != "" && !hasSystemMessage(messages) {
		messages = prependSystemMessage(messages, attribution.SystemPrompt)
	}

	model := req.Model
	if model == "" && attribution.DefaultModel != "" {
		model = attribution.DefaultModel
	}

	respFormat := req.ResponseFormat
	if respFormat == nil && attribution.ResponseFormat != nil {
		respFormat = attribution.ResponseFormat
	}

	return &EnrichedRequest{
		Model:          model,
		Messages:       messages,
		ResponseFormat: respFormat,
		Labels:         req.Labels,
		Temperature:    req.Temperature,
		MaxTokens:      req.MaxTokens,
		Stream:         req.Stream,
	}, nil
}

func (s *EnrichmentService) ValidateAttribution(req *ChatRequest) error {
	return nil
}

func hasSystemMessage(messages []ChatMessage) bool {
	for _, m := range messages {
		if m.Role == "system" {
			return true
		}
	}
	return false
}

func prependSystemMessage(messages []ChatMessage, prompt string) []ChatMessage {
	result := make([]ChatMessage, 0, len(messages)+1)
	result = append(result, ChatMessage{Role: "system", Content: prompt})
	result = append(result, messages...)
	return result
}
