package infrastructure

import (
	"context"
	"fmt"

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
		msg := "unknown error"
		if bifrostErr.Error != nil && bifrostErr.Error.Message != "" {
			msg = bifrostErr.Error.Message
		}
		return nil, fmt.Errorf("bifrost error: %s", msg)
	}
	return resp, nil
}
