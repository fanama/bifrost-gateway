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
	retrieved, err := s.search(ctx, projectID, question, opts)
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
	messages := domainrag.BuildMessages(question, retrieved, opts.SystemPrompt)
	reply, err := s.llm.Chat(ctx, cfg, messages)
	if err != nil {
		return nil, err
	}
	return &domainrag.Answer{
		Answer:     reply.Content,
		Sources:    domainrag.Sources(retrieved),
		HasContext: len(retrieved) > 0,
		Model:      reply.Model,
	}, nil
}

// Search returns the top passages for a question without calling the model.
func (s *Service) Search(ctx context.Context, projectID, question string, opts QueryOptions) ([]domainrag.RetrievedChunk, error) {
	projectID = trimProject(projectID)
	settings, err := s.resolvedSettings(ctx, projectID)
	if err != nil {
		return nil, err
	}
	opts = applyQuerySettings(settings, opts)
	return s.search(ctx, projectID, question, opts)
}

func (s *Service) search(ctx context.Context, projectID, question string, opts QueryOptions) ([]domainrag.RetrievedChunk, error) {
	projectID = trimProject(projectID)
	question = strings.TrimSpace(question)
	if projectID == "" {
		return nil, &domain.ValidationError{Fields: []string{"project_id"}}
	}
	if question == "" {
		return nil, &domain.ValidationError{Fields: []string{"question"}}
	}
	if len([]rune(question)) > domainrag.MaxQuestionChars {
		return nil, &domain.InvalidRequestError{Param: "question", Reason: "question is too long"}
	}
	chunks, err := s.chunks.ListByProject(ctx, projectID)
	if err != nil {
		return nil, err
	}
	if len(chunks) == 0 {
		return nil, nil
	}
	model := retrievalModel(opts.EmbeddingModel, chunks)
	vector, usedModel, err := s.embedTexts(ctx, projectID, []string{question}, model)
	if err != nil {
		return nil, err
	}
	return domainrag.RankChunks(vector[0], compatible(chunks, usedModel, len(vector[0])), opts.TopK), nil
}

func retrievalModel(requested string, chunks []domainrag.Chunk) string {
	if model := strings.TrimSpace(requested); model != "" {
		return model
	}
	return chunks[0].EmbeddingModel
}

// compatible drops chunks embedded with another model or width: mixing those
// vectors would compare unrelated spaces.
func compatible(chunks []domainrag.Chunk, model string, width int) []domainrag.Chunk {
	out := make([]domainrag.Chunk, 0, len(chunks))
	for _, chunk := range chunks {
		if chunk.EmbeddingModel == model && len(chunk.Vector) == width {
			out = append(out, chunk)
		}
	}
	return out
}

func (s *Service) activeConfig(ctx context.Context, projectID string) (*domain.ChatConfig, error) {
	if s.configs == nil {
		return nil, domain.ErrNoActiveConfig
	}
	return s.configs.GetActiveByProject(ctx, projectID)
}
