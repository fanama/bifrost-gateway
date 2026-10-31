package rag

import (
	"context"
	"errors"
	"strings"

	"bridge-gateway/domain"
	"bridge-gateway/domain/errs"
	domainrag "bridge-gateway/domain/rag"
)

// Settings returns the stored parameters of a project, or the domain defaults
// when the project never saved them.
func (s *Service) Settings(ctx context.Context, projectID string) (*domainrag.Settings, error) {
	projectID = trimProject(projectID)
	if projectID == "" {
		return nil, &domain.ValidationError{Fields: []string{"project_id"}}
	}
	if s.settings == nil {
		defaults := domainrag.DefaultSettings(projectID)
		return &defaults, nil
	}
	stored, err := s.settings.Get(ctx, projectID)
	if errors.Is(err, domainrag.ErrSettingsNotFound) {
		defaults := domainrag.DefaultSettings(projectID)
		return &defaults, nil
	}
	if err != nil {
		return nil, err
	}
	stored.Normalize()
	return stored, nil
}

// SaveSettings validates and upserts the RAG parameters of a project.
func (s *Service) SaveSettings(ctx context.Context, settings *domainrag.Settings) (*domainrag.Settings, error) {
	if settings == nil {
		return nil, &domain.ValidationError{Fields: []string{"settings"}}
	}
	saved := *settings
	saved.Normalize()
	if err := saved.Validate(); err != nil {
		return nil, err
	}
	if s.settings == nil {
		return nil, errs.New(errs.CodeInternal, "rag settings store is not configured")
	}
	saved.UpdatedAt = s.now().UTC()
	if err := s.settings.Save(ctx, &saved); err != nil {
		return nil, err
	}
	return &saved, nil
}

func (s *Service) resolvedSettings(ctx context.Context, projectID string) (*domainrag.Settings, error) {
	return s.Settings(ctx, projectID)
}

func applyIngestSettings(settings *domainrag.Settings, opts IngestOptions) IngestOptions {
	if strings.TrimSpace(opts.EmbeddingModel) == "" {
		opts.EmbeddingModel = settings.EmbeddingModel
	}
	if opts.ChunkSize <= 0 {
		opts.ChunkSize = settings.ChunkSize
	}
	if opts.ChunkOverlap <= 0 {
		opts.ChunkOverlap = settings.ChunkOverlap
	}
	return opts
}

func applyQuerySettings(settings *domainrag.Settings, opts QueryOptions) QueryOptions {
	if strings.TrimSpace(opts.EmbeddingModel) == "" {
		opts.EmbeddingModel = settings.EmbeddingModel
	}
	if opts.TopK <= 0 {
		opts.TopK = settings.TopK
	}
	if strings.TrimSpace(opts.SystemPrompt) == "" {
		opts.SystemPrompt = settings.SystemPrompt
	}
	return opts
}
