package web

import (
	"context"
	"embed"
	"errors"
	"html/template"
	"log"
	"net/http"
	"strconv"

	"bridge-gateway/domain"
)

//go:embed templates/*.html
//go:embed static/*
var files embed.FS

// uiShared regroupe ce que tous les sous-contrôleurs partagent : le rendu des
// templates et les listes (providers, modeles) utilisées par les formulaires.
// Chaque contrôleur par domaine l'embarque et ne détient en plus que ses
// propres ports : c'est le découpage en responsabilités (SRP) de la couche
// delivery, sans dupliquer le rendu.
type uiShared struct {
	templates  *template.Template
	providers  providerService
	models     modelService
	ollamaBase string
}

// Server est la façade du serveur UI HTMX : elle assemble les sous-contrôleurs
// par domaine et publie le routing. Elle ne dépend que des ports déclarés dans
// ports.go (interfaces de consommateur), pas des types concrets des cas
// d'usage : c'est l'inversion de dépendance au niveau de la couche delivery.
type Server struct {
	chat       chatController
	embeddings embeddingsController
	providers  providersController
	models     modelsController
	projects   projectsController
}

func NewServer(
	chat chatService,
	configs configService,
	models modelService,
	projects projectService,
	keys keyService,
	providers providerService,
	catalog catalogService,
	embeddings embeddingService,
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
	shared := uiShared{templates: tmpl, providers: providers, models: models, ollamaBase: ollamaBase}
	return &Server{
		chat:       chatController{uiShared: shared, chat: chat, configs: configs},
		embeddings: embeddingsController{uiShared: shared, embeddings: embeddings},
		providers:  providersController{uiShared: shared},
		models:     modelsController{uiShared: shared, catalog: catalog},
		projects:   projectsController{uiShared: shared, projects: projects, configs: configs, keys: keys},
	}
}

// Register publie le routing de l'UI : chaque route est deleguee au
// sous-contrôleur du domaine concerne.
func (s *Server) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /{$}", s.chat.pageChat)
	mux.HandleFunc("POST /chat/send", s.chat.chatSend)
	mux.HandleFunc("GET /chat/configs/{cid}/edit", s.chat.editChatConfigForm)
	mux.HandleFunc("POST /chat/configs/{cid}", s.chat.updateChatConfig)

	mux.HandleFunc("GET /embeddings-test", s.embeddings.pageEmbeddings)
	mux.HandleFunc("POST /embeddings-test/compute", s.embeddings.computeEmbeddings)

	mux.HandleFunc("GET /models", s.models.pageModels)
	mux.HandleFunc("POST /models", s.models.createModel)
	mux.HandleFunc("DELETE /models/{id}", s.models.deleteModel)

	mux.HandleFunc("GET /providers", s.providers.pageProviders)
	mux.HandleFunc("POST /providers", s.providers.createProvider)
	mux.HandleFunc("GET /providers/{id}/edit", s.providers.editProviderForm)
	mux.HandleFunc("POST /providers/{id}", s.providers.updateProvider)
	mux.HandleFunc("DELETE /providers/{id}", s.providers.deleteProvider)

	mux.HandleFunc("GET /projects", s.projects.pageProjects)
	mux.HandleFunc("POST /projects", s.projects.createProject)
	mux.HandleFunc("GET /projects/{pid}", s.projects.pageProjectDetail)
	mux.HandleFunc("POST /projects/{pid}/configs", s.projects.createProjectConfig)
	mux.HandleFunc("GET /projects/{pid}/configs/models", s.projects.projectConfigModels)
	mux.HandleFunc("GET /projects/{pid}/configs/{cid}/edit", s.projects.editProjectConfigForm)
	mux.HandleFunc("POST /projects/{pid}/configs/{cid}", s.projects.updateProjectConfig)
	mux.HandleFunc("POST /projects/{pid}/configs/{cid}/activate", s.projects.activateProjectConfig)
	mux.HandleFunc("POST /projects/{pid}/configs/{cid}/duplicate", s.projects.duplicateProjectConfig)
	mux.HandleFunc("DELETE /projects/{pid}/configs/{cid}", s.projects.deleteProjectConfig)
	mux.HandleFunc("POST /projects/{pid}/keys", s.projects.createProjectKey)
	mux.HandleFunc("DELETE /projects/{pid}/keys/{kid}", s.projects.deleteProjectKey)

	mux.HandleFunc("GET /configs", s.redirectToProjects)

	mux.Handle("GET /static/", http.FileServer(http.FS(files)))
}

func (s *Server) redirectToProjects(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/projects", http.StatusSeeOther)
}

// ---- Helpers partages (uiShared) ----

func (s *uiShared) render(w http.ResponseWriter, name string, data any) {
	if err := s.templates.ExecuteTemplate(w, name, data); err != nil {
		log.Printf("template %s error: %v", name, err)
		http.Error(w, "internal error", http.StatusInternalServerError)
	}
}

func (s *uiShared) renderToast(w http.ResponseWriter, retarget, message string) {
	w.Header().Set("HX-Retarget", retarget)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	s.render(w, "toast_error", message)
}

// ---- Helpers UI ----

func (s *uiShared) providerList(ctx context.Context) []domain.Provider {
	list, err := s.providers.List(ctx)
	if err != nil {
		return nil
	}
	return list
}

func (s *uiShared) providerNames(ctx context.Context) []string {
	list := s.providerList(ctx)
	names := make([]string, 0, len(list))
	for _, p := range list {
		names = append(names, p.Name)
	}
	return names
}

func (s *uiShared) baseForm(ctx context.Context, pid string, editing *domain.ChatConfig) configFormData {
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

func (s *uiShared) modelHints(ctx context.Context, provider string) []string {
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

func IsNotFound(err error) bool {
	return errors.Is(err, domain.ErrConfigNotFound)
}

var _ = IsNotFound
