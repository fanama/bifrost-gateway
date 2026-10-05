package domain

type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ChatRequest struct {
	Model            string            `json:"model"`
	Messages         []ChatMessage     `json:"messages"`
	Metadata         *RequestMetadata  `json:"metadata,omitempty"`
	BifrostMetadata  *RequestMetadata  `json:"bifrost_metadata,omitempty"`
	ResponseFormat   map[string]any    `json:"response_format,omitempty"`
	Labels           map[string]string `json:"labels,omitempty"`
	Temperature      *float64          `json:"temperature,omitempty"`
	TopP             *float64          `json:"top_p,omitempty"`
	MaxTokens        *int              `json:"max_tokens,omitempty"`
	FrequencyPenalty *float64          `json:"frequency_penalty,omitempty"`
	PresencePenalty  *float64          `json:"presence_penalty,omitempty"`
	Stream           bool              `json:"stream,omitempty"`
	StreamOptions    *StreamOptions    `json:"stream_options,omitempty"`
	// MaxCompletionTokens est le successeur de max_tokens. Il n'est lu que
	// lorsqu'il est present, pour ne pas ecraser un max_tokens du client.
	MaxCompletionTokens *int `json:"max_completion_tokens,omitempty"`
}

// StreamOptions regle le comportement du flux SSE, comme chez OpenAI.
// IncludeUsage demande une trame finale de consommation, distincte des autres.
type StreamOptions struct {
	IncludeUsage bool `json:"include_usage,omitempty"`
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

// RequestOptions regroupe les reglages qu'un appelant peut surcharger pour un
// appel. Ils l'emportent sur la configuration active, comme chez OpenAI, ou la
// requete prime toujours sur la configuration.
//
// Chaque champ est un pointeur pour distinguer "absent" de "explicitement mis a
// zero" : sans cela, une requete ne pourrait pas forcer temperature a 0.
type RequestOptions struct {
	Temperature         *float64
	TopP                *float64
	MaxTokens           *int
	FrequencyPenalty    *float64
	PresencePenalty     *float64
	ResponseFormat      map[string]any
	MaxCompletionTokens *int
}

// OptionsFrom isole les reglages surchargeables d'une requete complete.
func OptionsFrom(req *ChatRequest) *RequestOptions {
	if req == nil {
		return nil
	}
	return &RequestOptions{
		Temperature:         req.Temperature,
		TopP:                req.TopP,
		MaxTokens:           req.MaxTokens,
		FrequencyPenalty:    req.FrequencyPenalty,
		PresencePenalty:     req.PresencePenalty,
		ResponseFormat:      req.ResponseFormat,
		MaxCompletionTokens: req.MaxCompletionTokens,
	}
}

// Apply rend une copie de la configuration sur laquelle les reglages de la
// requete ont ete appliques.
//
// La surcharge passe par une copie plutot que par une nouvelle forme transportee
// jusqu'au provider : la configuration reste l'unique representation en
// circulation, donc cache, routeur et client Bifrost n'ont pas a savoir qu'une
// requete a surcharge quelque chose. Le modele, lui, n'est jamais surcharge : il
// reste celui de la configuration active.
func (o *RequestOptions) Apply(cfg *ChatConfig) (*ChatConfig, error) {
	out := *cfg
	if o == nil {
		return &out, nil
	}

	if o.Temperature != nil {
		out.Temperature = o.Temperature
	}
	if o.TopP != nil {
		out.TopP = o.TopP
	}
	if o.MaxTokens != nil {
		out.MaxTokens = o.MaxTokens
	}
	// max_completion_tokens est le successeur de max_tokens : les modeles de
	// raisonnement refusent l'ancien, ils n'acceptent que le nouveau.
	if o.MaxCompletionTokens != nil {
		out.MaxTokens = o.MaxCompletionTokens
	}
	if o.FrequencyPenalty != nil {
		out.FrequencyPenalty = o.FrequencyPenalty
	}
	if o.PresencePenalty != nil {
		out.PresencePenalty = o.PresencePenalty
	}
	if o.ResponseFormat != nil {
		format, schema, err := ParseResponseFormat(o.ResponseFormat)
		if err != nil {
			return nil, &InvalidRequestError{Param: "response_format", Reason: err.Error()}
		}
		out.ResponseFormat = format
		out.ResponseSchema = schema
	}

	return &out, nil
}
