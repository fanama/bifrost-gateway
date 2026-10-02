package infrastructure

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	bifrost "github.com/maximhq/bifrost/core"
	"github.com/maximhq/bifrost/core/schemas"
)

type BifrostClient struct {
	client *bifrost.Bifrost
}

func NewBifrostClient(account schemas.Account) (*BifrostClient, error) {
	client, err := bifrost.Init(context.Background(), schemas.BifrostConfig{
		Account: account,
	})
	if err != nil {
		return nil, err
	}
	return &BifrostClient{client: client}, nil
}

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

// ProviderError conserve les champs structurants d'un echec provider.
//
// L'adaptateur doit pouvoir decider si un echec merite une bascule vers un
// autre modele, ce qui impose de lire le code HTTP et le code fournisseur sans
// analyser une chaine. Le rendu textuel reste identique a celui historique pour
// que les messages de l'UI ne changent pas.
type ProviderError struct {
	StatusCode int
	Code       string
	Message    string
	Detail     string
}

func (e *ProviderError) Error() string {
	if e.Detail != "" {
		return fmt.Sprintf("bifrost error: %s: %s", e.Message, e.Detail)
	}
	if e.StatusCode != 0 {
		return fmt.Sprintf("bifrost error: %s (HTTP %d)", e.Message, e.StatusCode)
	}
	return fmt.Sprintf("bifrost error: %s", e.Message)
}

// formatBifrostError renders a BifrostError into something actionable. The SDK
// collapses a whole class of failures into a generic message ("HTML response
// received from provider"), so the HTTP status and a short excerpt of the body
// are kept to make misconfigured base URLs diagnosable from the UI.
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

// excerpt collapses whitespace and truncates s so a provider error page does not
// flood the UI toast.
func excerpt(s string, max int) string {
	collapsed := strings.Join(strings.Fields(s), " ")
	if max <= 0 || len(collapsed) <= max {
		return collapsed
	}
	return collapsed[:max] + "..."
}
