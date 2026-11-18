package web

import (
	"context"
	"net/http"
	"strings"

	"bridge-gateway/domain"
)

// modelsController regroupe la page Modeles : consultation du catalogue
// fusionne et ajout/suppression de modeles.
type modelsController struct {
	uiShared
	catalog catalogService
}

// ---- Modeles ----

// pageModels keeps /models bookmarks working after the merge with /providers.
func (s *modelsController) pageModels(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/providers#models", http.StatusSeeOther)
}

func (s *modelsController) renderModelsList(w http.ResponseWriter, r *http.Request) {
	groups, err := s.catalogGroups(r.Context())
	if err != nil {
		s.renderToast(w, "#models-error", err.Error())
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	s.render(w, "catalog_groups", map[string]any{"Groups": groups})
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

// providerGroup rassemble un provider et les modeles qui lui sont rattaches.
type providerGroup struct {
	Provider *domain.Provider
	Name     string
	Models   []domain.ModelInfo
}

// catalogGroups fusionne le catalogue des providers et celui des modeles.
func (s *uiShared) catalogGroups(ctx context.Context) ([]providerGroup, error) {
	providers, err := s.providers.List(ctx)
	if err != nil {
		return nil, err
	}
	models, err := s.models.List(ctx)
	if err != nil {
		return nil, err
	}
	return groupModelsByProvider(providers, models), nil
}

// groupModelsByProvider garde l'ordre des providers, puis ajoute ceux qui
// n'existent que dans le catalogue de modeles.
func groupModelsByProvider(providers []domain.Provider, models []domain.ModelInfo) []providerGroup {
	groups := make([]providerGroup, 0, len(providers))
	index := make(map[string]int, len(providers))
	for i := range providers {
		index[providerKey(providers[i].Name)] = len(groups)
		groups = append(groups, providerGroup{Provider: &providers[i], Name: providers[i].Name})
	}
	for _, model := range models {
		name := strings.TrimSpace(model.Provider)
		if name == "" {
			name = "Sans provider"
		}
		pos, ok := index[providerKey(name)]
		if !ok {
			pos = len(groups)
			index[providerKey(name)] = pos
			groups = append(groups, providerGroup{Name: name})
		}
		groups[pos].Models = append(groups[pos].Models, model)
	}
	return groups
}

func providerKey(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}
