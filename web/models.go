package web

import "net/http"

// modelsController regroupe la page Modeles : consultation du catalogue
// fusionne et ajout/suppression de modeles.
type modelsController struct {
	uiShared
	catalog catalogService
}

// ---- Modeles ----

func (s *modelsController) pageModels(w http.ResponseWriter, r *http.Request) {
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

func (s *modelsController) renderModelsList(w http.ResponseWriter, r *http.Request) {
	models, err := s.models.List(r.Context())
	if err != nil {
		s.renderToast(w, "#models-error", err.Error())
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	s.render(w, "models_list", map[string]any{"Models": models})
}

func (s *modelsController) createModel(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		s.renderToast(w, "#models-error", "requête invalide")
		return
	}
	name := r.FormValue("name")
	provider := r.FormValue("provider")
	kind := r.FormValue("kind")
	if provider == "" {
		provider = "ollama"
	}
	if _, err := s.catalog.Create(r.Context(), name, provider, kind); err != nil {
		s.renderToast(w, "#models-error", err.Error())
		return
	}
	s.renderModelsList(w, r)
}

func (s *modelsController) deleteModel(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.catalog.Delete(r.Context(), id); err != nil {
		s.renderToast(w, "#models-error", err.Error())
		return
	}
	s.renderModelsList(w, r)
}
