package application

import (
	"context"
	"strings"
	"time"

	"bridge-gateway/domain"
)

type ConfigUseCase struct {
	repo  domain.ChatConfigRepository
	idGen func() string
	now   func() time.Time
}

func NewConfigUseCase(repo domain.ChatConfigRepository) *ConfigUseCase {
	return &ConfigUseCase{
		repo:  repo,
		idGen: domain.NewID,
		now:   time.Now,
	}
}

func (u *ConfigUseCase) List(ctx context.Context) ([]domain.ChatConfig, error) {
	return u.repo.List(ctx)
}

func (u *ConfigUseCase) ListByProject(ctx context.Context, projectID string) ([]domain.ChatConfig, error) {
	return u.repo.ListByProject(ctx, projectID)
}

func (u *ConfigUseCase) Active(ctx context.Context, projectID string) (*domain.ChatConfig, error) {
	return u.repo.GetActiveByProject(ctx, projectID)
}

func (u *ConfigUseCase) Get(ctx context.Context, id string) (*domain.ChatConfig, error) {
	return u.repo.Get(ctx, id)
}

func (u *ConfigUseCase) Create(ctx context.Context, cfg *domain.ChatConfig) (*domain.ChatConfig, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	cfg.ID = u.idGen()
	cfg.CreatedAt = u.now().UTC()
	cfg.UpdatedAt = cfg.CreatedAt
	if err := u.repo.Create(ctx, cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

func (u *ConfigUseCase) Update(ctx context.Context, id string, patch *domain.ChatConfig) (*domain.ChatConfig, error) {
	if err := patch.Validate(); err != nil {
		return nil, err
	}
	existing, err := u.repo.Get(ctx, id)
	if err != nil {
		return nil, err
	}

	existing.Name = strings.TrimSpace(patch.Name)
	existing.Provider = strings.ToLower(strings.TrimSpace(patch.Provider))
	existing.Model = strings.TrimSpace(patch.Model)
	existing.BaseURL = strings.TrimSpace(patch.BaseURL)
	existing.APIKey = strings.TrimSpace(patch.APIKey)
	existing.SystemPrompt = patch.SystemPrompt
	existing.CostCenter = strings.TrimSpace(patch.CostCenter)
	existing.Temperature = patch.Temperature
	existing.TopP = patch.TopP
	existing.MaxTokens = patch.MaxTokens
	existing.FrequencyPenalty = patch.FrequencyPenalty
	existing.PresencePenalty = patch.PresencePenalty
	existing.ResponseFormat = patch.ResponseFormat
	existing.UpdatedAt = u.now().UTC()

	if err := u.repo.Update(ctx, existing); err != nil {
		return nil, err
	}
	return existing, nil
}

func (u *ConfigUseCase) Delete(ctx context.Context, id string) error {
	return u.repo.Delete(ctx, id)
}

func (u *ConfigUseCase) SetActive(ctx context.Context, projectID string, id string) (*domain.ChatConfig, error) {
	if err := u.repo.SetActive(ctx, projectID, id); err != nil {
		return nil, err
	}
	return u.repo.Get(ctx, id)
}
