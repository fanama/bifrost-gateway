package web

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"html/template"
	"log"
	"net/http"
	"sort"
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
	mux.HandleFunc("GET /chat/configs/{cid}/edit", s.editChatConfigForm)
	mux.HandleFunc("POST /chat/configs/{cid}", s.updateChatConfig)

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
	mux.HandleFunc("GET /projects/{pid}/configs/models", s.projectConfigModels)
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

func (s *Server) baseForm(ctx context.Context, pid string, editing *domain.ChatConfig) configFormData {
	providers := s.providerList(ctx)
	data := configFormData{
		ProjectID:   pid,
		Action:      "/projects/" + pid + "/configs",
		Editing:     editing,
		IsCreate:    editing == nil,
		UseDefaults: editing == nil,
		Providers:   providers,
	}
	if editing != nil {
		data.Action += "/" + editing.ID
		data.SelectedProvider = editing.Provider
		data.SelectedModel = editing.Model
	}
	hints := s.modelHints(ctx, data.SelectedProvider)
	if editing != nil && editing.Model != "" {
		found := false
		for _, h := range hints {
			if h == editing.Model {
				found = true
				break
			}
		}
		if !found {
			hints = append([]string{editing.Model}, hints...)
		}
	}
	data.Models = hints
	data.SchemaFieldTypes = domain.SchemaFieldTypes()
	data.SchemaFields = schemaRowsFromStored(editingSchema(editing))
	data.ResponseSchema = ""
	if editing != nil {
		data.ResponseSchema = editing.ResponseSchema
		data.EditingFormat = editing.EffectiveResponseFormat()
	}
	return data
}

func (s *Server) modelHints(ctx context.Context, provider string) []string {
	models, err := s.models.ListForProvider(ctx, provider)
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

	// L'exemple curl propose par defaut le modele de la configuration active,
	// sinon celui de la premiere configuration disponible.
	defaultModel := ""
	hasActive := false
	for i := range configs {
		if configs[i].Active {
			hasActive = true
			defaultModel = configs[i].Model
			break
		}
	}
	if defaultModel == "" && len(configs) > 0 {
		defaultModel = configs[0].Model
	}

	s.render(w, "page_chat", map[string]any{
		"Active":       "chat",
		"Title":        "Discussion",
		"Configs":      configs,
		"HasActive":    hasActive,
		"BaseURL":      chatAPIBaseURL(r),
		"DefaultModel": defaultModel,
	})
}

func (s *Server) editChatConfigForm(w http.ResponseWriter, r *http.Request) {
	cid := r.PathValue("cid")
	cfg, err := s.configs.Get(r.Context(), cid)
	if err != nil {
		s.renderToast(w, "#chat-error", err.Error())
		return
	}
	form := s.baseForm(r.Context(), cfg.ProjectID, cfg)
	form.Action = "/chat/configs/" + cid
	form.Target = "#chat-config-selector"
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	s.render(w, "config_form", form)
}

func (s *Server) updateChatConfig(w http.ResponseWriter, r *http.Request) {
	cid := r.PathValue("cid")
	existing, err := s.configs.Get(r.Context(), cid)
	if err != nil {
		s.renderToast(w, "#chat-error", err.Error())
		return
	}
	patch := parseConfigForm(r)
	patch.ResponseSchema = parseSchemaRows(r)
	patch.ProjectID = existing.ProjectID
	if _, err := s.configs.Update(r.Context(), cid, patch); err != nil {
		s.renderToast(w, "#chat-error", err.Error())
		return
	}
	configs, err := s.configs.List(r.Context())
	if err != nil {
		s.renderToast(w, "#chat-error", err.Error())
		return
	}
	hasActive := false
	for _, c := range configs {
		if c.Active {
			hasActive = true
			break
		}
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	s.render(w, "chat_config_select", map[string]any{
		"Configs":          configs,
		"SelectedConfigID": cid,
		"HasActive":        hasActive,
	})
}

// chatAPIBaseURL reconstruit l'origine publique de la passerelle, pour que
// l'exemple curl fonctionne tel quel derriere un reverse proxy ou sur un port
// different de celui du serveur local.
func chatAPIBaseURL(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if forwarded := strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")); forwarded != "" {
		scheme = strings.TrimSpace(strings.Split(forwarded, ",")[0])
	}
	host := strings.TrimSpace(r.Host)
	if forwarded := strings.TrimSpace(r.Header.Get("X-Forwarded-Host")); forwarded != "" {
		host = strings.TrimSpace(strings.Split(forwarded, ",")[0])
	}
	if host == "" {
		return ""
	}
	return scheme + "://" + host
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

	// L'interface n'expose pas de surcharge : tout vient de la configuration
	// selectionnee.
	result, err := s.chat.Send(r.Context(), cfgID, history, message, nil)
	if err != nil {
		s.renderToast(w, "#chat-error", err.Error())
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// L'historique complet de la conversation est classe, pas seulement le
	// dernier message.
	s.render(w, "chat_messages", map[string]any{
		"Messages":       result.Messages,
		"ServedModel":    result.Model,
		"RequestedModel": result.RequestedModel,
	})
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
		"Form":      s.baseForm(ctx, pid, nil),
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
		"Form":    s.baseForm(r.Context(), pid, nil),
		"Configs": configs,
	})
}

func (s *Server) createProjectConfig(w http.ResponseWriter, r *http.Request) {
	pid := r.PathValue("pid")
	cfg := parseConfigForm(r)
	cfg.ResponseSchema = parseSchemaRows(r)
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
	patch.ResponseSchema = parseSchemaRows(r)
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
	s.render(w, "config_form", s.baseForm(r.Context(), pid, cfg))
}

func (s *Server) projectConfigModels(w http.ResponseWriter, r *http.Request) {
	pid := r.PathValue("pid")
	if pid == "" {
		http.NotFound(w, r)
		return
	}

	provider := strings.TrimSpace(r.FormValue("provider"))
	var hints []string
	if provider != "" {
		models, err := s.models.ListForProvider(r.Context(), provider)
		if err != nil {
			s.renderToast(w, "#project-config-error", err.Error())
			return
		}
		for _, m := range models {
			hints = append(hints, m.Name)
		}
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	s.render(w, "config_model_select", map[string]any{
		"Models":        hints,
		"SelectedModel": "",
	})
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
		Tier:             strings.ToLower(strings.TrimSpace(r.FormValue("tier"))),
		BaseURL:          strings.TrimSpace(r.FormValue("base_url")),
		APIKey:           strings.TrimSpace(r.FormValue("api_key")),
		SystemPrompt:     strings.TrimSpace(r.FormValue("system_prompt")),
		Temperature:      parseFloatOpt(r.FormValue("temperature")),
		TopP:             parseFloatOpt(r.FormValue("top_p")),
		MaxTokens:        parseIntOpt(r.FormValue("max_tokens")),
		FrequencyPenalty: parseFloatOpt(r.FormValue("frequency_penalty")),
		PresencePenalty:  parseFloatOpt(r.FormValue("presence_penalty")),
		ResponseFormat:   strings.TrimSpace(r.FormValue("response_format")),
		ResponseSchema:   strings.TrimSpace(r.FormValue("response_schema")),
	}
}

// parseSchemaRows rend le JSON Schema construit depuis le formulaire visuel.
//
// La textarea response_schema reste prioritaire : elle permet de saisir un
// schema que le constructeur n'exprime pas (types unions, imbrication), sans
// perdre l'assistance visuelle pour le cas courant.
func parseSchemaRows(r *http.Request) string {
	if raw := strings.TrimSpace(r.FormValue("response_schema")); raw != "" {
		return raw
	}

	names := r.Form["schema_field_name[]"]
	required := map[int]bool{}
	for _, v := range r.Form["schema_field_required[]"] {
		if i, err := strconv.Atoi(v); err == nil {
			required[i] = true
		}
	}

	// Chaque colonne du tableau est une liste d'inputs de meme nom, dont l'ordre
	// suit l'ordre des lignes. Les colonnes absentes restent des slices vides :
	// at() sur une slice hors bornes donne la valeur nulle plutot qu'une panique,
	// ce qui evite de dependre du nombre exact de colonnes envoyees.
	at := func(column []string, i int) string {
		if i < len(column) {
			return column[i]
		}
		return ""
	}

	types := r.Form["schema_field_type[]"]
	descriptions := r.Form["schema_field_description[]"]
	enums := r.Form["schema_field_enum[]"]
	mins := r.Form["schema_field_min[]"]
	maxs := r.Form["schema_field_max[]"]
	patterns := r.Form["schema_field_pattern[]"]
	itemTypes := r.Form["schema_field_items[]"]

	fields := make([]domain.SchemaField, 0, len(names))
	for i, name := range names {
		fields = append(fields, domain.SchemaField{
			Name:        name,
			Type:        at(types, i),
			Description: at(descriptions, i),
			Required:    required[i],
			Enum:        at(enums, i),
			Min:         at(mins, i),
			Max:         at(maxs, i),
			Pattern:     at(patterns, i),
			ItemsType:   at(itemTypes, i),
		})
	}

	if len(fields) == 0 {
		return ""
	}

	// Une construction invalide ne doit pas bloquer la sauvegarde du reste de la
	// configuration : on ne retient alors aucun schema, et la validation de la
	// configuration signalera que json_schema en exige un.
	schema, err := domain.BuildJSONSchema(fields)
	if err != nil {
		return ""
	}
	return schema
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

// schemaRow est une ligne du constructeur visuel. Index est la position
// stable de la ligne, reprise dans la case a cocher "obligatoire" pour que
// l'index survive au tri des champs du schema.
type schemaRow struct {
	Index       int
	Name        string
	Type        string
	Description string
	Required    bool
	Enum        string
	Min         string
	Max         string
	Pattern     string
	ItemsType   string
}

type configFormData struct {
	Editing          *domain.ChatConfig
	Action           string
	Target           string
	ProjectID        string
	IsCreate         bool
	UseDefaults      bool
	Providers        []domain.Provider
	SelectedProvider string
	SelectedModel    string
	Models           []string
	// SchemaFields restitue les champs du schema enregistre pour que l'edition
	// parte de l'etat reel plutot que d'un formulaire vide.
	SchemaFields     []schemaRow
	SchemaFieldTypes []string
	ResponseSchema   string
	// EditingFormat est le format reellement applique. Il peut differer du champ
	// brut enregistre : un schema stocke vaut declaration d'intention et impose
	// json_schema meme si le select avait ete laisse sur "aucun". Afficher la
	// valeur brute laisserait croire que le schema est ignore.
	EditingFormat string
}

// schemaRowsFromStored relit un schema enregistre pour reafficher ses champs.
// Un schema illisible ne bloque pas l'edition : le formulaire se presente alors
// avec une seule ligne vide, et la validation signalera le probleme a
// l'enregistrement.
func schemaRowsFromStored(raw string) []schemaRow {
	rows := []schemaRow{}

	schema, err := domain.ParseJSONSchema(raw)
	if err == nil {
		properties, _ := schema["properties"].(map[string]any)
		required := map[string]bool{}
		if list, ok := schema["required"].([]any); ok {
			for _, item := range list {
				if name, ok := item.(string); ok {
					required[name] = true
				}
			}
		}

		names := make([]string, 0, len(properties))
		for name := range properties {
			names = append(names, name)
		}
		// Un schema est un objet : l'ordre de ses cles n'a pas de sens en JSON,
		// mais il en a un dans un formulaire. Le trier rend l'edition stable.
		sort.Strings(names)

		for i, name := range names {
			property, _ := properties[name].(map[string]any)
			row := schemaRow{Index: i, Name: name, Type: "string", Required: required[name]}
			if t, ok := property["type"].(string); ok && t != "" {
				row.Type = t
			}
			if d, ok := property["description"].(string); ok {
				row.Description = d
			}
			// Les contraintes sont relues avec le meme code qui les ecrit : sans
			// cela, un champ enregistre avec un enum ou un minimum ressortirait
			// vide a l'edition, et l'utilisateur perdrait sa saisie.
			row.Enum = domain.EnumValues(property)
			row.Min = domain.MinMaxString(property, "minLength", "minimum")
			row.Max = domain.MinMaxString(property, "maxLength", "maximum")
			if p, ok := property["pattern"].(string); ok {
				row.Pattern = p
			}
			row.ItemsType = domain.ItemsType(property)
			rows = append(rows, row)
		}
	}

	if len(rows) == 0 {
		// Toujours une ligne : un tableau vide n'offre aucun moyen d'ajouter un
		// premier champ.
		rows = append(rows, schemaRow{Index: 0, Type: "string"})
	}
	return rows
}

// editingSchema rend le schema a reafficher, celui d'une configuration en cours
// d'édition.
func editingSchema(editing *domain.ChatConfig) string {
	if editing == nil {
		return ""
	}
	return editing.ResponseSchema
}

func IsNotFound(err error) bool {
	return errors.Is(err, domain.ErrConfigNotFound)
}

var _ = IsNotFound
