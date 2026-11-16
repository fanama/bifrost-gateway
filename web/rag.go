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
	mux.HandleFunc("POST /rag/index/rebuild", s.rebuildIndex)
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
	status, err := s.rag.IndexStatus(ctx, pid)
	if err != nil {
		return nil, err
	}
	data["Settings"] = settings
	data["EmbedChoices"] = withCurrentChoice(choices, settings.EmbeddingModel)
	data["Status"] = status
	data["ProjectID"] = pid
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
	status, err := s.rag.IndexStatus(r.Context(), saved.ProjectID)
	if err != nil {
		s.renderToast(w, "#rag-settings-error", err.Error())
		return
	}
	s.render(w, "rag_settings_form", map[string]any{
		"Settings":     saved,
		"EmbedChoices": withCurrentChoice(choices, saved.EmbeddingModel),
		"Status":       status,
		"ProjectID":    saved.ProjectID,
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
	hnsw, msg := parseHNSWForm(r)
	if msg != "" {
		return nil, msg
	}
	return &domain.RAGSettings{
		ProjectID:                 strings.TrimSpace(r.FormValue("project_id")),
		EmbeddingModel:            strings.TrimSpace(r.FormValue("embedding_model")),
		TopK:                      topK,
		ChunkSize:                 chunkSize,
		ChunkOverlap:              overlap,
		SystemPrompt:              r.FormValue("system_prompt"),
		HNSWM:                     hnsw.M,
		HNSWEfConstruction:        hnsw.EfConstruction,
		HNSWEfSearch:              hnsw.EfSearch,
		HNSWTombstoneRebuildRatio: hnsw.TombstoneRatio,
		HNSWMemoryLimitMB:         hnsw.MemoryLimitMB,
		HNSWRecallSampleRate:      hnsw.RecallSampleRate,
	}, ""
}

type hnswFormValues struct {
	M                int
	EfConstruction   int
	EfSearch         int
	TombstoneRatio   float64
	MemoryLimitMB    int
	RecallSampleRate float64
}

func parseHNSWForm(r *http.Request) (hnswFormValues, string) {
	values, msg := hnswFormValues{}, ""
	if values.M, msg = parseIntField(r, "hnsw_m"); msg != "" {
		return values, msg
	}
	if values.EfConstruction, msg = parseIntField(r, "hnsw_ef_construction"); msg != "" {
		return values, msg
	}
	if values.EfSearch, msg = parseIntField(r, "hnsw_ef_search"); msg != "" {
		return values, msg
	}
	if values.MemoryLimitMB, msg = parseIntField(r, "hnsw_memory_limit_mb"); msg != "" {
		return values, msg
	}
	if values.TombstoneRatio, msg = parseFloatField(r, "hnsw_tombstone_rebuild_ratio"); msg != "" {
		return values, msg
	}
	values.RecallSampleRate, msg = parseFloatField(r, "hnsw_recall_sample_rate")
	return values, msg
}

func parseIntField(r *http.Request, name string) (int, string) {
	value, err := strconv.Atoi(strings.TrimSpace(r.FormValue(name)))
	if err != nil {
		return 0, name + " doit être un nombre"
	}
	return value, ""
}

func parseFloatField(r *http.Request, name string) (float64, string) {
	value, err := strconv.ParseFloat(strings.TrimSpace(r.FormValue(name)), 64)
	if err != nil {
		return 0, name + " doit être un nombre"
	}
	return value, ""
}

func (s *ragController) rebuildIndex(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		s.renderToast(w, "#rag-settings-error", "requête invalide")
		return
	}
	pid := strings.TrimSpace(r.FormValue("project_id"))
	status, err := s.rag.RebuildIndex(r.Context(), pid)
	if err != nil {
		s.renderToast(w, "#rag-settings-error", err.Error())
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	s.render(w, "rag_index_card", map[string]any{"Status": status, "ProjectID": pid})
}
