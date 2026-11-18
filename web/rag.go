package web

import (
	"context"
	"net/http"
	"net/url"
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
	mux.HandleFunc("GET /rag", s.pageRAG)
	mux.HandleFunc("GET /rag/settings", s.redirectToRAG)
	mux.HandleFunc("GET /rag/test", s.redirectToRAG)
	mux.HandleFunc("POST /rag/settings", s.saveSettings)
	mux.HandleFunc("POST /rag/upload", s.uploadDocument)
	mux.HandleFunc("POST /rag/query", s.askQuestion)
	mux.HandleFunc("GET /rag/documents/{id}/chunks", s.documentChunks)
	mux.HandleFunc("POST /rag/documents/{id}/delete", s.deleteDocument)
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

func (s *ragController) pageRAG(w http.ResponseWriter, r *http.Request) {
	data, err := s.pageData(r.Context(), r.URL.Query().Get("project_id"))
	if err != nil {
		s.renderToast(w, "#rag-error", err.Error())
		return
	}
	s.render(w, "page_rag", data)
}

// redirectToRAG keeps the settings/test bookmarks working after the merge.
func (s *ragController) redirectToRAG(w http.ResponseWriter, r *http.Request) {
	target := "/rag"
	if pid := strings.TrimSpace(r.URL.Query().Get("project_id")); pid != "" {
		target += "?project_id=" + url.QueryEscape(pid)
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}

func (s *ragController) pageData(ctx context.Context, requested string) (map[string]any, error) {
	projects, pid, err := s.selection(ctx, requested)
	if err != nil {
		return nil, err
	}
	data := map[string]any{
		"Active":            "rag",
		"Title":             "RAG",
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
	documents, err := s.rag.Documents(ctx, pid)
	if err != nil {
		return nil, err
	}
	data["Settings"] = settings
	data["EmbedChoices"] = withCurrentChoice(choices, settings.EmbeddingModel)
	data["ChunkingChoices"] = domain.RAGChunkingChoices
	data["RetrievalChoices"] = domain.RAGRetrievalChoices
	data["RerankChoices"] = domain.RAGRerankChoices
	data["Status"] = status
	data["ProjectID"] = pid
	data["Documents"] = documents
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
		s.renderToast(w, "#rag-error", msg)
		return
	}
	saved, err := s.rag.SaveSettings(r.Context(), settings)
	if err != nil {
		s.renderToast(w, "#rag-error", err.Error())
		return
	}
	choices, err := s.embeddingChoices(r.Context())
	if err != nil {
		s.renderToast(w, "#rag-error", err.Error())
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	status, err := s.rag.IndexStatus(r.Context(), saved.ProjectID)
	if err != nil {
		s.renderToast(w, "#rag-error", err.Error())
		return
	}
	s.render(w, "rag_settings_form", map[string]any{
		"Settings":         saved,
		"EmbedChoices":     withCurrentChoice(choices, saved.EmbeddingModel),
		"ChunkingChoices":  domain.RAGChunkingChoices,
		"RetrievalChoices": domain.RAGRetrievalChoices,
		"RerankChoices":    domain.RAGRerankChoices,
		"Status":           status,
		"ProjectID":        saved.ProjectID,
		"Saved":            true,
	})
}

func parseRAGSettings(r *http.Request) (*domain.RAGSettings, string) {
	if err := r.ParseForm(); err != nil {
		return nil, "requête invalide"
	}
	shape, msg := parseShapeForm(r)
	if msg != "" {
		return nil, msg
	}
	hnsw, msg := parseHNSWForm(r)
	if msg != "" {
		return nil, msg
	}
	pipeline, msg := parsePipelineForm(r)
	if msg != "" {
		return nil, msg
	}
	return &domain.RAGSettings{
		ProjectID:                 strings.TrimSpace(r.FormValue("project_id")),
		EmbeddingModel:            strings.TrimSpace(r.FormValue("embedding_model")),
		TopK:                      shape.TopK,
		ChunkSize:                 shape.ChunkSize,
		ChunkOverlap:              shape.Overlap,
		ChunkingMethod:            strings.TrimSpace(r.FormValue("chunking_method")),
		RetrievalMode:             pipeline.Mode,
		RetrievalCandidates:       pipeline.Candidates,
		Reranker:                  pipeline.Reranker,
		MMRLambda:                 pipeline.Lambda,
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

type shapeFormValues struct {
	TopK      int
	ChunkSize int
	Overlap   int
}

func parseShapeForm(r *http.Request) (shapeFormValues, string) {
	var values shapeFormValues
	var msg string
	if values.TopK, msg = parseIntField(r, "top_k"); msg != "" {
		return values, msg
	}
	if values.ChunkSize, msg = parseIntField(r, "chunk_size"); msg != "" {
		return values, msg
	}
	values.Overlap, msg = parseIntField(r, "chunk_overlap")
	return values, msg
}

type pipelineFormValues struct {
	Mode       string
	Candidates int
	Reranker   string
	Lambda     float64
}

func parsePipelineForm(r *http.Request) (pipelineFormValues, string) {
	values := pipelineFormValues{
		Mode:     strings.TrimSpace(r.FormValue("retrieval_mode")),
		Reranker: strings.TrimSpace(r.FormValue("reranker")),
	}
	var msg string
	if values.Candidates, msg = parseIntField(r, "retrieval_candidates"); msg != "" {
		return values, msg
	}
	values.Lambda, msg = parseFloatField(r, "mmr_lambda")
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
		s.renderToast(w, "#rag-error", "requête invalide")
		return
	}
	pid := strings.TrimSpace(r.FormValue("project_id"))
	status, err := s.rag.RebuildIndex(r.Context(), pid)
	if err != nil {
		s.renderToast(w, "#rag-error", err.Error())
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	s.render(w, "rag_index_card", map[string]any{"Status": status, "ProjectID": pid})
}
