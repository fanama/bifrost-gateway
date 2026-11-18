package web

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"bridge-gateway/application"
	"bridge-gateway/domain"
	domainrag "bridge-gateway/domain/rag"
)

// ragRequested reports whether the discussion checkbox asked for retrieval.
func ragRequested(r *http.Request) bool {
	switch strings.ToLower(strings.TrimSpace(r.FormValue("use_rag"))) {
	case "1", "true", "on", "yes":
		return true
	default:
		return false
	}
}

// chatPrompt resolves the user message sent to the model: the typed message by
// default, or a grounded prompt when the RAG checkbox is checked.
func (s *chatController) chatPrompt(r *http.Request, cfgID, message string) (string, []domain.Source, error) {
	if !ragRequested(r) || message == "" {
		return message, nil, nil
	}
	if s.rag == nil {
		return "", nil, errors.New("RAG non configuré pour cette instance")
	}
	return s.withRAG(r.Context(), cfgID, message)
}

// withRAG retrieves the passages of the configuration's project and returns the
// grounded prompt sent to the model plus the citations shown under the reply.
// The caller keeps rendering the typed message, never the augmented prompt.
func (s *chatController) withRAG(ctx context.Context, cfgID, message string) (string, []domain.Source, error) {
	cfg, err := s.configs.Get(ctx, cfgID)
	if err != nil {
		return "", nil, err
	}
	result, err := s.rag.Search(ctx, cfg.ProjectID, message, application.RAGQueryOptions{})
	if err != nil {
		return "", nil, err
	}
	return domainrag.GroundedPrompt(message, result.Chunks), domainrag.Sources(result.Chunks), nil
}
