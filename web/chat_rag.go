package web

import (
	"context"
	"errors"
	"net/http"

	"bridge-gateway/application"
	"bridge-gateway/domain"
	domainrag "bridge-gateway/domain/rag"
)

// chatPrompt resolves the user message sent to the model: the typed message by
// default, or a grounded prompt when the configuration enables RAG.
func (s *chatController) chatPrompt(r *http.Request, cfgID, message string) (string, []domain.Source, bool, error) {
	if message == "" {
		return message, nil, false, nil
	}
	cfg, err := s.configs.Get(r.Context(), cfgID)
	if err != nil {
		return "", nil, false, err
	}
	if !cfg.UseRAG {
		return message, nil, false, nil
	}
	if s.rag == nil {
		return "", nil, false, errors.New("RAG non configuré pour cette instance")
	}
	prompt, sources, err := s.withRAG(r.Context(), cfg.ProjectID, message)
	if err != nil {
		return "", nil, false, err
	}
	return prompt, sources, true, nil
}

// withRAG retrieves the passages of the configuration's project and returns the
// grounded prompt sent to the model plus the citations shown under the reply.
// The caller keeps rendering the typed message, never the augmented prompt.
func (s *chatController) withRAG(ctx context.Context, projectID, message string) (string, []domain.Source, error) {
	result, err := s.rag.Search(ctx, projectID, message, application.RAGQueryOptions{})
	if err != nil {
		return "", nil, err
	}
	return domainrag.GroundedPrompt(message, result.Chunks), domainrag.Sources(result.Chunks), nil
}
