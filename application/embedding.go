package application

import (
	"context"

	"bridge-gateway/domain"
)

type EmbeddingUseCase struct {
	configs  domain.ChatConfigRepository
	provider domain.EmbeddingProvider
}

func NewEmbeddingUseCase(configs domain.ChatConfigRepository, provider domain.EmbeddingProvider) *EmbeddingUseCase {
	return &EmbeddingUseCase{configs: configs, provider: provider}
}

func (u *EmbeddingUseCase) EmbedWithConfig(ctx context.Context, cfg *domain.ChatConfig, req *domain.EmbeddingRequest) (*domain.EmbeddingResponse, error) {
	if cfg == nil {
		return nil, domain.ErrNoActiveConfig
	}
	return u.provider.Embed(ctx, cfg, req)
}

func (u *EmbeddingUseCase) Embed(ctx context.Context, projectID string, req *domain.EmbeddingRequest) (*domain.EmbeddingResponse, error) {
	cfg, err := u.configs.GetActiveByProject(ctx, projectID)
	if err != nil {
		return nil, err
	}
	return u.provider.Embed(ctx, cfg, req)
}

func (u *EmbeddingUseCase) EmbedByConfigID(ctx context.Context, configID string, req *domain.EmbeddingRequest) (*domain.EmbeddingResponse, *domain.ChatConfig, error) {
	var cfg *domain.ChatConfig
	var err error
	if configID != "" {
		cfg, err = u.configs.Get(ctx, configID)
		if err != nil {
			return nil, nil, err
		}
	} else {
		configs, listErr := u.configs.List(ctx)
		if listErr != nil {
			return nil, nil, listErr
		}
		for i := range configs {
			if configs[i].Active {
				cfg = &configs[i]
				break
			}
		}
		if cfg == nil && len(configs) > 0 {
			cfg = &configs[0]
		}
		if cfg == nil {
			return nil, nil, domain.ErrNoActiveConfig
		}
	}
	resp, err := u.provider.Embed(ctx, cfg, req)
	return resp, cfg, err
}
