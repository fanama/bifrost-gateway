package rag

import (
	"context"

	"bridge-gateway/domain"
	domainrag "bridge-gateway/domain/rag"
)

// indexChunks keeps the graph in sync after a document has been persisted. A
// failure is not fatal: the exact scan remains the correctness backstop.
func (s *Service) indexChunks(
	ctx context.Context,
	projectID, model string,
	chunks []domainrag.Chunk,
	settings *domainrag.Settings,
) {
	if s.index == nil || len(chunks) == 0 || settings == nil {
		return
	}
	_ = s.index.Upsert(ctx, projectID, model, chunks, settings.IndexOptions())
}

// removeIndexedDocument tombstones a deleted document's passages so they stop
// appearing in results immediately.
func (s *Service) removeIndexedDocument(ctx context.Context, projectID, documentID string) {
	if s.index == nil {
		return
	}
	settings, err := s.Settings(ctx, projectID)
	if err != nil {
		return
	}
	_ = s.index.DeleteDocument(ctx, projectID, documentID, settings.IndexOptions())
}

// IndexStatus returns the observable state of a project's index. The status is
// enriched with the settings currently stored, which may not have been applied
// yet if the project has never been queried.
func (s *Service) IndexStatus(ctx context.Context, projectID string) (domainrag.IndexStatus, error) {
	projectID = trimProject(projectID)
	if projectID == "" {
		return domainrag.IndexStatus{}, &domain.ValidationError{Fields: []string{"project_id"}}
	}
	settings, err := s.Settings(ctx, projectID)
	if err != nil {
		return domainrag.IndexStatus{}, err
	}
	opts := settings.IndexOptions()
	status := domainrag.IndexStatus{
		ProjectID:        projectID,
		State:            domainrag.IndexDisabled,
		EmbeddingModel:   settings.EmbeddingModel,
		MemoryLimitBytes: opts.MemoryLimitBytes,
		Params:           paramsOf(opts),
	}
	if s.index != nil {
		status = s.index.Status(ctx, projectID)
		status.ProjectID = projectID
		status.MemoryLimitBytes = opts.MemoryLimitBytes
		status.Params = paramsOf(opts)
		if status.EmbeddingModel == "" {
			status.EmbeddingModel = settings.EmbeddingModel
		}
	}
	status.Degraded = status.State == domainrag.IndexDegraded || status.State == domainrag.IndexOverCapacity
	return status, nil
}

// RebuildIndex triggers a background rebuild and returns the current status.
func (s *Service) RebuildIndex(ctx context.Context, projectID string) (domainrag.IndexStatus, error) {
	projectID = trimProject(projectID)
	if projectID == "" {
		return domainrag.IndexStatus{}, &domain.ValidationError{Fields: []string{"project_id"}}
	}
	settings, err := s.Settings(ctx, projectID)
	if err != nil {
		return domainrag.IndexStatus{}, err
	}
	if s.index != nil {
		if err := s.index.Rebuild(ctx, projectID, settings.EmbeddingModel, settings.IndexOptions()); err != nil {
			return domainrag.IndexStatus{}, err
		}
	}
	return s.IndexStatus(ctx, projectID)
}

func paramsOf(opts domainrag.IndexOptions) domainrag.IndexParams {
	return domainrag.IndexParams{
		M:              opts.M,
		EfConstruction: opts.EfConstruction,
		EfSearch:       opts.EfSearch,
	}
}
