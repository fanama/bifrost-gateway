package infrastructure

import (
	"context"
	"time"

	"bridge-gateway/domain"
	"github.com/maximhq/bifrost/core/schemas"
)

// DynamicAccount est l'adaptateur schemas.Account du SDK Bifrost : une instance
// par configuration active, qui expose le provider, la cle et le BaseURL de
// cette configuration. Cela evite de reconfigurer un compte global a chaque
// requete alors que chaque projet a sa propre configuration.
type DynamicAccount struct {
	cfg     *domain.ChatConfig
	apiKey  string
	baseURL string
	// retries vaut 0 tant qu'il n'a pas ete fixe, ce qui laisse GetConfigForProvider
	// appliquer defaultMaxRetries.
	retries int
}

func NewDynamicAccount(cfg *domain.ChatConfig, customParams ...string) *DynamicAccount {
	apiKey := cfg.APIKey
	baseURL := cfg.BaseURL
	if len(customParams) > 0 && customParams[0] != "" {
		apiKey = customParams[0]
	}
	if len(customParams) > 1 && customParams[1] != "" {
		baseURL = customParams[1]
	}
	return &DynamicAccount{cfg: cfg, apiKey: apiKey, baseURL: baseURL}
}

func (a *DynamicAccount) withMaxRetries(n int) *DynamicAccount {
	a.retries = n
	return a
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
		Value:  schemas.EnvVar{Val: ResolveEnv(a.apiKey)},
		Models: []string{},
		Weight: 1.0,
	}}, nil
}

func (a *DynamicAccount) GetConfigForProvider(provider schemas.ModelProvider) (*schemas.ProviderConfig, error) {
	baseURL := normalizeBaseURL(ResolveEnv(a.baseURL))
	if baseURL == "" {
		baseURL = normalizeBaseURL(defaultBaseURLFor(provider))
	}
	retries := a.retries
	if retries <= 0 {
		retries = defaultMaxRetries
	}
	return &schemas.ProviderConfig{
		NetworkConfig: schemas.NetworkConfig{
			BaseURL:                        baseURL,
			DefaultRequestTimeoutInSeconds: 60,
			MaxRetries:                     retries,
			RetryBackoffInitial:            500 * time.Millisecond,
			RetryBackoffMax:                5 * time.Second,
		},
		ConcurrencyAndBufferSize: schemas.DefaultConcurrencyAndBufferSize,
	}, nil
}
