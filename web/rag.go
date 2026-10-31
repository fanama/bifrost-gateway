package web

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	"bridge-gateway/domain"
)

const maxWebRAGUpload = 25 << 20

// ragController exposes the per-project RAG settings and a test bench.
type ragController struct {
	uiShared
	rag      ragService
	projects projectService
}

func (s *ragController) register(mux *http.ServeMux) {
	if s.rag == nil {
		return
	}
	mux.HandleFunc("GET /rag/settings", s.pageSettings)
	mux.HandleFunc("POST /rag/settings", s.saveSettings)
	mux.HandleFunc("GET /rag/test", s.pageTest)
	mux.HandleFunc("POST /rag/test/upload", s.uploadDocument)
	mux.HandleFunc("POST /rag/test/query", s.askQuestion)
	mux.HandleFunc("POST /rag/test/documents/{id}/delete", s.deleteDocument)
}

func (s *ragController) selection(ctx context.Context, requested string) ([]domain.Project, string, error) {
	projects, err := s.projects.List(ctx)
	if err != nil {
		return nil, "", err
	}
	requested = strings.TrimSpace(requested)
	for _, project := range projects {
		if project.ID == requested {
			return projects, project.ID, nil
		}
	}
	if len(projects) > 0 {
		return projects, projects[0].ID, nil
	}
	return projects, "", nil
}

func (s *ragController) pageSettings(w http.ResponseWriter, r *http.Request) {
	projects, pid, err := s.selection(r.Context(), r.URL.Query().Get("project_id"))
	if err != nil {
		s.renderToast(w, "#rag-settings-error", err.Error())
		return
	}
	data, err := s.settingsData(r.Context(), projects, pid)
	if err != nil {
		s.renderToast(w, "#rag-settings-error", err.Error())
		return
	}
	s.render(w, "page_rag_settings", data)
}

func (s *ragController) settingsData(ctx context.Context, projects []domain.Project, pid string) (map[string]any, error) {
	data := map[string]any{
		"Active":            "rag-settings",
		"Title":             "RAG — Paramètres",
		"Projects":          projects,
		"SelectedProjectID": pid,
	}
	if pid == "" {
		return data, nil
	}
	settings, err := s.rag.Settings(ctx, pid)
	if err != nil {
		return nil, err
	}
	choices, err := s.embeddingChoices(ctx)
	if err != nil {
		return nil, err
	}
	data["Settings"] = settings
	data["EmbedChoices"] = withCurrentChoice(choices, settings.EmbeddingModel)
	return data, nil
}

func withCurrentChoice(choices []domain.EmbeddingChoice, model string) []domain.EmbeddingChoice {
	for _, choice := range choices {
		if strings.EqualFold(choice.Model, model) {
			return choices
		}
	}
	return append(choices, domain.EmbeddingChoice{Model: model, Label: model + " (actuel)"})
}

func (s *ragController) saveSettings(w http.ResponseWriter, r *http.Request) {
	settings, msg := parseRAGSettings(r)
	if msg != "" {
		s.renderToast(w, "#rag-settings-error", msg)
		return
	}
	saved, err := s.rag.SaveSettings(r.Context(), settings)
	if err != nil {
		s.renderToast(w, "#rag-settings-error", err.Error())
		return
	}
	choices, err := s.embeddingChoices(r.Context())
	if err != nil {
		s.renderToast(w, "#rag-settings-error", err.Error())
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	s.render(w, "rag_settings_form", map[string]any{
		"Settings":     saved,
		"EmbedChoices": withCurrentChoice(choices, saved.EmbeddingModel),
		"Saved":        true,
	})
}

func parseRAGSettings(r *http.Request) (*domain.RAGSettings, string) {
	if err := r.ParseForm(); err != nil {
		return nil, "requête invalide"
	}
	topK, err := strconv.Atoi(strings.TrimSpace(r.FormValue("top_k")))
	if err != nil {
		return nil, "top_k doit être un nombre"
	}
	chunkSize, err := strconv.Atoi(strings.TrimSpace(r.FormValue("chunk_size")))
	if err != nil {
		return nil, "chunk_size doit être un nombre"
	}
	overlap, err := strconv.Atoi(strings.TrimSpace(r.FormValue("chunk_overlap")))
	if err != nil {
		return nil, "chunk_overlap doit être un nombre"
	}
	return &domain.RAGSettings{
		ProjectID:      strings.TrimSpace(r.FormValue("project_id")),
		EmbeddingModel: strings.TrimSpace(r.FormValue("embedding_model")),
		TopK:           topK,
		ChunkSize:      chunkSize,
		ChunkOverlap:   overlap,
		SystemPrompt:   r.FormValue("system_prompt"),
	}, ""
}
