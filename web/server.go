package web

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"html/template"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"bridge-gateway/application"
	"bridge-gateway/domain"
)

//go:embed templates/*.html
//go:embed static/*
var files embed.FS

type Server struct {
	chat       *application.ChatUseCase
	configs    *application.ConfigUseCase
	models     *application.ModelUseCase
	projects   *application.ProjectUseCase
	keys       *application.APIKeyUseCase
	providers  *application.ProviderUseCase
	catalog    *application.ModelCatalogUseCase
	ollamaBase string
	templates  *template.Template
}

func NewServer(
	chat *application.ChatUseCase,
	configs *application.ConfigUseCase,
	models *application.ModelUseCase,
	projects *application.ProjectUseCase,
	keys *application.APIKeyUseCase,
	providers *application.ProviderUseCase,
	catalog *application.ModelCatalogUseCase,
	ollamaBase string,
) *Server {
	funcs := template.FuncMap{
		"f": func(p *float64) string {
			if p == nil {
				return ""
			}
			return strconv.FormatFloat(*p, 'f', -1, 64)
		},
		"i": func(p *int) string {
			if p == nil {
				return ""
			}
			return strconv.Itoa(*p)
		},
	}
	tmpl, err := template.New("").Funcs(funcs).ParseFS(files, "templates/*.html")
	if err != nil {
		log.Fatalf("parse templates: %v", err)
	}
	return &Server{chat: chat, configs: configs, models: models, projects: projects, keys: keys, providers: providers, catalog: catalog, ollamaBase: ollamaBase, templates: tmpl}
}

func (s *Server) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /{$}", s.pageChat)
	mux.HandleFunc("POST /chat/send", s.chatSend)

	mux.HandleFunc("GET /models", s.pageModels)
	mux.HandleFunc("POST /models", s.createModel)
	mux.HandleFunc("DELETE /models/{id}", s.deleteModel)

	mux.HandleFunc("GET /providers", s.pageProviders)
	mux.HandleFunc("POST /providers", s.createProvider)
	mux.HandleFunc("GET /providers/{id}/edit", s.editProviderForm)
	mux.HandleFunc("POST /providers/{id}", s.updateProvider)
	mux.HandleFunc("DELETE /providers/{id}", s.deleteProvider)

	mux.HandleFunc("GET /projects", s.pageProjects)
	mux.HandleFunc("POST /projects", s.createProject)
	mux.HandleFunc("GET /projects/{pid}", s.pageProjectDetail)
	mux.HandleFunc("POST /projects/{pid}/configs", s.createProjectConfig)
	mux.HandleFunc("GET /projects/{pid}/configs/{cid}/edit", s.editProjectConfigForm)
	mux.HandleFunc("POST /projects/{pid}/configs/{cid}", s.updateProjectConfig)
	mux.HandleFunc("POST /projects/{pid}/configs/{cid}/activate", s.activateProjectConfig)
	mux.HandleFunc("POST /projects/{pid}/configs/{cid}/duplicate", s.duplicateProjectConfig)
	mux.HandleFunc("DELETE /projects/{pid}/configs/{cid}", s.deleteProjectConfig)
	mux.HandleFunc("POST /projects/{pid}/keys", s.createProjectKey)
	mux.HandleFunc("DELETE /projects/{pid}/keys/{kid}", s.deleteProjectKey)

	mux.HandleFunc("GET /configs", s.redirectToProjects)

	mux.Handle("GET /static/", http.FileServer(http.FS(files)))
}

func (s *Server) render(w http.ResponseWriter, name string, data any) {
	if err := s.templates.ExecuteTemplate(w, name, data); err != nil {
		log.Printf("template %s error: %v", name, err)
		http.Error(w, "internal error", http.StatusInternalServerError)
	}
}

func (s *Server) renderToast(w http.ResponseWriter, retarget, message string) {
	w.Header().Set("HX-Retarget", retarget)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	s.render(w, "toast_error", message)
}

func (s *Server) redirectToProjects(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/projects", http.StatusSeeOther)
}

// ---- Helpers UI ----

func (s *Server) providerList(ctx context.Context) []domain.Provider {
	list, err := s.providers.List(ctx)
	if err != nil {
		return nil
	}
	return list
}

func (s *Server) providerNames(ctx context.Context) []string {
	list := s.providerList(ctx)
	names := make([]string, 0, len(list))
	for _, p := range list {
		names = append(names, p.Name)
	}
	return names
}

func (s *Server) baseForm(ctx context.Context, action string, editing *domain.ChatConfig) configFormData {
	data := configFormData{
		Action:      action,
		Editing:     editing,
		IsCreate:    editing == nil,
		UseDefaults: editing == nil,
		Providers:   s.providerNames(ctx),
		ModelHints:  s.modelHints(ctx),
	}
	if editing != nil {
		data.SelectedProvider = editing.Provider
	}
	return data
}

func (s *Server) modelHints(ctx context.Context) []string {
	models, err := s.models.List(ctx)
	if err != nil {
		return nil
	}
	hints := make([]string, 0, len(models))
	for _, m := range models {
		hints = append(hints, m.Name)
	}
	return hints
}

func (s *Server) pageChat(w http.ResponseWriter, r *http.Request) {
	configs, err := s.configs.List(r.Context())
	if err != nil {
		s.renderToast(w, "#chat-error", err.Error())
		return
	}
	s.render(w, "page_chat", map[string]any{
		"Active":  "chat",
		"Title":   "Discussion",
		"Configs": configs,
	})
}

func (s *Server) chatSend(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		s.renderToast(w, "#chat-error", "requête invalide")
		return
	}

	cfgID := r.FormValue("config_id")
	message := r.FormValue("message")

	var history []domain.ChatMessage
	if raw := strings.TrimSpace(r.FormValue("history")); raw != "" {
		if err := json.Unmarshal([]byte(raw), &history); err != nil {
			history = nil
		}
	}

	messages, err := s.chat.Send(r.Context(), cfgID, history, message)
	if err != nil {
		s.renderToast(w, "#chat-error", err.Error())
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	s.render(w, "chat_messages", map[string]any{"Messages": messages})
}

// ---- Providers ----

func (s *Server) pageProviders(w http.ResponseWriter, r *http.Request) {
	providers := s.providerList(r.Context())
	s.render(w, "page_providers", map[string]any{
		"Active":    "providers",
		"Title":     "Providers",
		"Providers": providers,
		"Create":    providerFormData{Action: "/providers"},
	})
}

func (s *Server) createProvider(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		s.renderToast(w, "#providers-error", "requête invalide")
		return
	}
	if _, err := s.providers.Create(r.Context(), r.FormValue("name"), r.FormValue("base_url"), r.FormValue("api_key")); err != nil {
		s.renderToast(w, "#providers-error", err.Error())
		return
	}
	s.renderProvidersList(w, r)
}

func (s *Server) updateProvider(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		s.renderToast(w, "#providers-error", "requête invalide")
		return
	}
	id := r.PathValue("id")
	if _, err := s.providers.Update(r.Context(), id, r.FormValue("name"), r.FormValue("base_url"), r.FormValue("api_key")); err != nil {
		s.renderToast(w, "#providers-error", err.Error())
		return
	}
	s.renderProvidersList(w, r)
}

func (s *Server) deleteProvider(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.providers.Delete(r.Context(), id); err != nil {
		s.renderToast(w, "#providers-error", err.Error())
		return
	}
	s.renderProvidersList(w, r)
}

func (s *Server) editProviderForm(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	p, err := s.providers.Get(r.Context(), id)
	if err != nil {
		s.renderToast(w, "#providers-error", err.Error())
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	s.render(w, "provider_form", providerFormData{
		Editing: p,
		Action:  "/providers/" + p.ID,
	})
}

func (s *Server) renderProvidersList(w http.ResponseWriter, r *http.Request) {
	providers := s.providerList(r.Context())
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	s.render(w, "provider_rows", map[string]any{"Providers": providers})
}

type providerFormData struct {
	Editing *domain.Provider
	Action  string
}

// ---- Modeles ----

func (s *Server) pageModels(w http.ResponseWriter, r *http.Request) {
	models, err := s.models.List(r.Context())
	if err != nil {
		s.renderToast(w, "#models-error", err.Error())
		return
	}
	s.render(w, "page_models", map[string]any{
		"Active":    "models",
		"Title":     "Modeles",
		"Models":    models,
		"Providers": s.providerNames(r.Context()),
	})
}

func (s *Server) renderModelsList(w http.ResponseWriter, r *http.Request) {
	models, err := s.models.List(r.Context())
	if err != nil {
		s.renderToast(w, "#models-error", err.Error())
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	s.render(w, "models_list", map[string]any{"Models": models})
}

func (s *Server) createModel(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		s.renderToast(w, "#models-error", "requête invalide")
		return
	}
	name := r.FormValue("name")
	provider := r.FormValue("provider")
	if provider == "" {
		provider = "ollama"
	}
	if _, err := s.catalog.Create(r.Context(), name, provider); err != nil {
		s.renderToast(w, "#models-error", err.Error())
		return
	}
	s.renderModelsList(w, r)
}

func (s *Server) deleteModel(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.catalog.Delete(r.Context(), id); err != nil {
		s.renderToast(w, "#models-error", err.Error())
		return
	}
	s.renderModelsList(w, r)
}

// ---- Projects ----

type projectSummary struct {
	domain.Project
	ConfigCount int
}

func (s *Server) projectSummaries(ctx context.Context) ([]projectSummary, error) {
	projects, err := s.projects.List(ctx)
	if err != nil {
		return nil, err
	}
	configs, err := s.configs.List(ctx)
	if err != nil {
		return nil, err
	}
	counts := make(map[string]int)
	for _, c := range configs {
		counts[c.ProjectID]++
	}
	out := make([]projectSummary, 0, len(projects))
	for _, p := range projects {
		out = append(out, projectSummary{Project: p, ConfigCount: counts[p.ID]})
	}
	return out, nil
}

func (s *Server) pageProjects(w http.ResponseWriter, r *http.Request) {
	summaries, err := s.projectSummaries(r.Context())
	if err != nil {
		s.renderToast(w, "#projects-error", err.Error())
		return
	}
	s.render(w, "page_projects", map[string]any{
		"Active":   "configs",
		"Title":    "Projets",
		"Projects": summaries,
	})
}

func (s *Server) createProject(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		s.renderToast(w, "#projects-error", "requête invalide")
		return
	}
	name := r.FormValue("name")
	desc := r.FormValue("description")
	if _, err := s.projects.Create(r.Context(), name, desc); err != nil {
		s.renderToast(w, "#projects-error", err.Error())
		return
	}
	summaries, err := s.projectSummaries(r.Context())
	if err != nil {
		s.renderToast(w, "#projects-error", err.Error())
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	s.render(w, "project_rows", map[string]any{"Projects": summaries})
}

func (s *Server) pageProjectDetail(w http.ResponseWriter, r *http.Request) {
	pid := r.PathValue("pid")
	project, err := s.projects.Get(r.Context(), pid)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	s.render(w, "page_project_detail", s.projectDetailData(r.Context(), pid, project, ""))
}

func (s *Server) projectDetailData(ctx context.Context, pid string, project *domain.Project, newKey string) map[string]any {
	configs, err := s.configs.ListByProject(ctx, pid)
	if err != nil {
		configs = nil
	}
	keys, err := s.keys.ListByProject(ctx, pid)
	if err != nil {
		keys = nil
	}
	return map[string]any{
		"Active":    "configs",
		"Title":     project.Name,
		"Project":   project,
		"Form":      s.baseForm(ctx, "/projects/"+pid+"/configs", nil),
		"Configs":   configs,
		"Keys":      keys,
		"NewKey":    newKey,
		"Providers": s.providerList(ctx),
	}
}

func (s *Server) renderProjectConfigsSection(w http.ResponseWriter, r *http.Request, pid string) {
	project, err := s.projects.Get(r.Context(), pid)
	if err != nil {
		s.renderToast(w, "#project-config-error", err.Error())
		return
	}
	configs, err := s.configs.ListByProject(r.Context(), pid)
	if err != nil {
		s.renderToast(w, "#project-config-error", err.Error())
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	s.render(w, "project_configs_section", map[string]any{
		"Project": project,
		"Form":    s.baseForm(r.Context(), "/projects/"+pid+"/configs", nil),
		"Configs": configs,
	})
}

func (s *Server) createProjectConfig(w http.ResponseWriter, r *http.Request) {
	pid := r.PathValue("pid")
	cfg := parseConfigForm(r)
	cfg.ProjectID = pid
	if _, err := s.configs.Create(r.Context(), cfg); err != nil {
		s.renderToast(w, "#project-config-error", err.Error())
		return
	}
	s.renderProjectConfigsSection(w, r, pid)
}

func (s *Server) updateProjectConfig(w http.ResponseWriter, r *http.Request) {
	pid := r.PathValue("pid")
	cid := r.PathValue("cid")
	patch := parseConfigForm(r)
	patch.ProjectID = pid
	if _, err := s.configs.Update(r.Context(), cid, patch); err != nil {
		s.renderToast(w, "#project-config-error", err.Error())
		return
	}
	s.renderProjectConfigsSection(w, r, pid)
}

func (s *Server) deleteProjectConfig(w http.ResponseWriter, r *http.Request) {
	pid := r.PathValue("pid")
	cid := r.PathValue("cid")
	if err := s.configs.Delete(r.Context(), cid); err != nil {
		s.renderToast(w, "#project-config-error", err.Error())
		return
	}
	s.renderProjectConfigsSection(w, r, pid)
}

func (s *Server) duplicateProjectConfig(w http.ResponseWriter, r *http.Request) {
	pid := r.PathValue("pid")
	cid := r.PathValue("cid")
	original, err := s.configs.Get(r.Context(), cid)
	if err != nil {
		s.renderToast(w, "#project-config-error", err.Error())
		return
	}
	clone := *original
	clone.ID = ""
	clone.Name = original.Name + " (copie)"
	clone.Active = false
	clone.CreatedAt = time.Time{}
	clone.UpdatedAt = time.Time{}
	if _, err := s.configs.Create(r.Context(), &clone); err != nil {
		s.renderToast(w, "#project-config-error", err.Error())
		return
	}
	s.renderProjectConfigsSection(w, r, pid)
}

func (s *Server) activateProjectConfig(w http.ResponseWriter, r *http.Request) {
	pid := r.PathValue("pid")
	cid := r.PathValue("cid")
	if _, err := s.configs.SetActive(r.Context(), pid, cid); err != nil {
		s.renderToast(w, "#project-config-error", err.Error())
		return
	}
	s.renderProjectConfigsSection(w, r, pid)
}

func (s *Server) editProjectConfigForm(w http.ResponseWriter, r *http.Request) {
	pid := r.PathValue("pid")
	cid := r.PathValue("cid")
	cfg, err := s.configs.Get(r.Context(), cid)
	if err != nil {
		s.renderToast(w, "#project-config-error", err.Error())
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	s.render(w, "config_form", s.baseForm(r.Context(), "/projects/"+pid+"/configs/"+cfg.ID, cfg))
}

func (s *Server) renderProjectKeysSection(w http.ResponseWriter, r *http.Request, pid string, newKey string) {
	project, err := s.projects.Get(r.Context(), pid)
	if err != nil {
		s.renderToast(w, "#key-error", err.Error())
		return
	}
	keys, err := s.keys.ListByProject(r.Context(), pid)
	if err != nil {
		s.renderToast(w, "#key-error", err.Error())
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	s.render(w, "project_keys_section", map[string]any{
		"Project": project,
		"Keys":    keys,
		"NewKey":  newKey,
	})
}

func (s *Server) createProjectKey(w http.ResponseWriter, r *http.Request) {
	pid := r.PathValue("pid")
	if err := r.ParseForm(); err != nil {
		s.renderToast(w, "#key-error", "requête invalide")
		return
	}
	_, secret, err := s.keys.Create(r.Context(), pid, r.FormValue("name"))
	if err != nil {
		s.renderToast(w, "#key-error", err.Error())
		return
	}
	s.renderProjectKeysSection(w, r, pid, secret)
}

func (s *Server) deleteProjectKey(w http.ResponseWriter, r *http.Request) {
	pid := r.PathValue("pid")
	kid := r.PathValue("kid")
	key, err := s.keys.Get(r.Context(), kid)
	if err != nil || key.ProjectID != pid {
		s.renderToast(w, "#key-error", "clé introuvable")
		return
	}
	if err := s.keys.Delete(r.Context(), kid); err != nil {
		s.renderToast(w, "#key-error", err.Error())
		return
	}
	s.renderProjectKeysSection(w, r, pid, "")
}

func parseConfigForm(r *http.Request) *domain.ChatConfig {
	_ = r.ParseForm()
	return &domain.ChatConfig{
		Name:             strings.TrimSpace(r.FormValue("name")),
		Provider:         strings.ToLower(strings.TrimSpace(r.FormValue("provider"))),
		Model:            strings.TrimSpace(r.FormValue("model")),
		BaseURL:          strings.TrimSpace(r.FormValue("base_url")),
		APIKey:           strings.TrimSpace(r.FormValue("api_key")),
		SystemPrompt:     strings.TrimSpace(r.FormValue("system_prompt")),
		Temperature:      parseFloatOpt(r.FormValue("temperature")),
		TopP:             parseFloatOpt(r.FormValue("top_p")),
		MaxTokens:        parseIntOpt(r.FormValue("max_tokens")),
		FrequencyPenalty: parseFloatOpt(r.FormValue("frequency_penalty")),
		PresencePenalty:  parseFloatOpt(r.FormValue("presence_penalty")),
		ResponseFormat:   strings.TrimSpace(r.FormValue("response_format")),
	}
}

func parseFloatOpt(raw string) *float64 {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	v, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return nil
	}
	return &v
}

func parseIntOpt(raw string) *int {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		return nil
	}
	return &v
}

type configFormData struct {
	Editing          *domain.ChatConfig
	Action           string
	Target           string
	IsCreate         bool
	UseDefaults      bool
	Providers        []string
	SelectedProvider string
	ModelHints       []string
}

func IsNotFound(err error) bool {
	return errors.Is(err, domain.ErrConfigNotFound)
}

var _ = IsNotFound
