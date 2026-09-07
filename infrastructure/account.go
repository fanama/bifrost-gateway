package infrastructure

import (
	"context"
	"fmt"

	"github.com/maximhq/bifrost/core/schemas"
)

type GatewayAccount struct{}

func (a *GatewayAccount) GetConfiguredProviders() ([]schemas.ModelProvider, error) {
	return []schemas.ModelProvider{
		schemas.Ollama,
		schemas.OpenAI,
		schemas.Azure,
	}, nil
}

func (a *GatewayAccount) GetKeysForProvider(_ context.Context, provider schemas.ModelProvider) ([]schemas.Key, error) {
	switch provider {
	case schemas.Ollama, schemas.OpenAI, schemas.Azure:
		return []schemas.Key{{
			Models: []string{},
			Weight: 1.0,
		}}, nil
	default:
		return nil, fmt.Errorf("provider %s not supported", provider)
	}
}

func (a *GatewayAccount) GetConfigForProvider(provider schemas.ModelProvider) (*schemas.ProviderConfig, error) {
	switch provider {
	case schemas.Ollama:
		return &schemas.ProviderConfig{
			NetworkConfig: schemas.NetworkConfig{
				BaseURL:                        "http://localhost:11434",
				DefaultRequestTimeoutInSeconds: 30,
			},
			ConcurrencyAndBufferSize: schemas.DefaultConcurrencyAndBufferSize,
		}, nil
	case schemas.OpenAI, schemas.Azure:
		return &schemas.ProviderConfig{
			NetworkConfig:            schemas.DefaultNetworkConfig,
			ConcurrencyAndBufferSize: schemas.DefaultConcurrencyAndBufferSize,
		}, nil
	default:
		return nil, fmt.Errorf("provider %s not supported", provider)
	}
}
