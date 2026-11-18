package rag

import (
	"context"
	"strings"

	"bridge-gateway/domain"
	"bridge-gateway/domain/errs"
	domainrag "bridge-gateway/domain/rag"
)

// Query retrieves passages and asks the configured chat model to answer from
// them. When no passage matches, the model still receives the explicit
// no-context instruction instead of the bare question.
func (s *Service) Query(ctx context.Context, projectID, question string, opts QueryOptions) (*domainrag.Answer, error) {
	projectID = trimProject(projectID)
	settings, err := s.resolvedSettings(ctx, projectID)
	if err != nil {
		return nil, err
	}
	opts = applyQuerySettings(settings, opts)
	result, err := s.searchWithSettings(ctx, projectID, question, opts, settings)
	if err != nil {
		return nil, err
	}
	cfg, err := s.activeConfig(ctx, projectID)
	if err != nil {
		return nil, err
	}
	if s.llm == nil {
		return nil, errs.New(errs.CodeInternal, "chat provider is not configured")
	}
	messages := domainrag.BuildMessages(question, result.Chunks, opts.SystemPrompt)
	reply, err := s.llm.Chat(ctx, cfg, messages)
	if err != nil {
		return nil, err
	}
	return &domainrag.Answer{
		Answer:     reply.Content,
		Sources:    domainrag.Sources(result.Chunks),
		HasContext: len(result.Chunks) > 0,
		Model:      reply.Model,
		Degraded:   result.Degraded,
		IndexState: result.IndexState,
	}, nil
}

// Search returns the top passages for a question without calling the model.
func (s *Service) Search(ctx context.Context, projectID, question string, opts QueryOptions) (*domainrag.SearchResult, error) {
	projectID = trimProject(projectID)
	settings, err := s.resolvedSettings(ctx, projectID)
	if err != nil {
		return nil, err
	}
	opts = applyQuerySettings(settings, opts)
	return s.searchWithSettings(ctx, projectID, question, opts, settings)
}

// searchWithSettings is the single retrieval path shared by Query and Search.
// It validates the question, embeds it and hands it to the configured pipeline,
// so both entry points always agree on what was retrieved.
func (s *Service) searchWithSettings(
	ctx context.Context,
	projectID, question string,
	opts QueryOptions,
	settings *domainrag.Settings,
) (*domainrag.SearchResult, error) {
	if projectID == "" {
		return nil, &domain.ValidationError{Fields: []string{"project_id"}}
	}
	question = strings.TrimSpace(question)
	if question == "" {
		return nil, &domain.ValidationError{Fields: []string{"question"}}
	}
	if len([]rune(question)) > domainrag.MaxQuestionChars {
		return nil, &domain.InvalidRequestError{Param: "question", Reason: "question is too long"}
	}
	vector, model, err := s.embedTexts(ctx, projectID, []string{question}, opts.EmbeddingModel)
	if err != nil {
		return nil, err
	}
	k := normalizeTopK(opts.TopK)
	chunks, state, degraded, err := s.retrieve(ctx, projectID, question, model, vector[0], k, settings)
	if err != nil {
		return nil, err
	}
	return &domainrag.SearchResult{Chunks: chunks, Degraded: degraded, IndexState: state}, nil
}

func (s *Service) activeConfig(ctx context.Context, projectID string) (*domain.ChatConfig, error) {
	if s.configs == nil {
		return nil, domain.ErrNoActiveConfig
	}
	return s.configs.GetActiveByProject(ctx, projectID)
}
