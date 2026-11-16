package web

import (
	"context"
	"net/http"
	"strings"
	"time"

	"bridge-gateway/domain"
)

// Options des configurations rattachees a un projet : creation, edition,
// suppression, duplication, activation et selecteurs de modeles.

func (s *projectsController) createProjectConfig(w http.ResponseWriter, r *http.Request) {
	pid := r.PathValue("pid")
	cfg := parseConfigForm(r, s.providerNames(r.Context()))
	cfg.ResponseSchema = parseSchemaRows(r)
	cfg.ProjectID = pid
	if _, err := s.configs.Create(r.Context(), cfg); err != nil {
		s.renderToast(w, "#project-config-error", err.Error())
		return
	}
	s.renderProjectConfigsSection(w, r, pid)
}

func (s *projectsController) updateProjectConfig(w http.ResponseWriter, r *http.Request) {
	pid := r.PathValue("pid")
	cid := r.PathValue("cid")
	patch := parseConfigForm(r, s.providerNames(r.Context()))
	patch.ResponseSchema = parseSchemaRows(r)
	patch.ProjectID = pid
	if _, err := s.configs.Update(r.Context(), cid, patch); err != nil {
		s.renderToast(w, "#project-config-error", err.Error())
		return
	}
	s.renderProjectConfigsSection(w, r, pid)
}

func (s *projectsController) deleteProjectConfig(w http.ResponseWriter, r *http.Request) {
	pid := r.PathValue("pid")
	cid := r.PathValue("cid")
	if err := s.configs.Delete(r.Context(), cid); err != nil {
		s.renderToast(w, "#project-config-error", err.Error())
		return
	}
	s.renderProjectConfigsSection(w, r, pid)
}

func (s *projectsController) duplicateProjectConfig(w http.ResponseWriter, r *http.Request) {
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

func (s *projectsController) activateProjectConfig(w http.ResponseWriter, r *http.Request) {
	pid := r.PathValue("pid")
	cid := r.PathValue("cid")
	if _, err := s.configs.SetActive(r.Context(), pid, cid); err != nil {
		s.renderToast(w, "#project-config-error", err.Error())
		return
	}
	s.renderProjectConfigsSection(w, r, pid)
}

func (s *projectsController) editProjectConfigForm(w http.ResponseWriter, r *http.Request) {
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

// projectConfigModels rend le select de modeles d'un provider. Les lignes de
// repli l'appellent avec leurs propres champs : elles n'ont pas d'identifiant
// (#cfg-model est unique) et leur modele n'est pas obligatoire.
func (s *projectsController) projectConfigModels(w http.ResponseWriter, r *http.Request) {
	pid := r.PathValue("pid")
	if pid == "" {
		http.NotFound(w, r)
		return
	}
	_ = r.ParseForm()

	hints, err := s.providerModels(r.Context(), fallbackProvider(r))
	if err != nil {
		s.renderToast(w, "#project-config-error", err.Error())
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	s.render(w, "config_model_select", map[string]any{
		"Models":        hints,
		"SelectedModel": "",
		"SelectID":      requestedSelectID(r),
		"ModelField":    requestedModelField(r),
		"Required":      requestedRequired(r),
	})
}

// newFallbackRow ajoute une ligne vierge au selecteur de repli.
func (s *projectsController) newFallbackRow(w http.ResponseWriter, r *http.Request) {
	pid := r.PathValue("pid")
	if pid == "" {
		http.NotFound(w, r)
		return
	}
	provider := strings.TrimSpace(r.FormValue("provider"))
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	s.render(w, "config_fallback_row", s.fallbackRow(r.Context(), pid, domain.ModelChoice{Provider: provider}))
}

func (s *projectsController) providerModels(ctx context.Context, provider string) ([]string, error) {
	if provider == "" {
		return nil, nil
	}
	models, err := s.models.ListForProvider(ctx, provider)
	if err != nil {
		return nil, err
	}
	hints := make([]string, 0, len(models))
	for _, model := range models {
		hints = append(hints, model.Name)
	}
	return hints, nil
}

// fallbackProvider accepte le provider sous ses deux noms : la ligne de repli
// est un select nomme fallback_provider[], que htmx transmet tel quel.
func fallbackProvider(r *http.Request) string {
	if provider := strings.TrimSpace(r.FormValue("provider")); provider != "" {
		return provider
	}
	if values := r.Form["fallback_provider[]"]; len(values) > 0 {
		return strings.TrimSpace(values[0])
	}
	return ""
}

func requestedSelectID(r *http.Request) string {
	if !r.Form.Has("select_id") {
		return "cfg-model"
	}
	return strings.TrimSpace(r.Form.Get("select_id"))
}

func requestedModelField(r *http.Request) string {
	if field := strings.TrimSpace(r.Form.Get("field")); field != "" {
		return field
	}
	return "model"
}

func requestedRequired(r *http.Request) bool {
	return r.Form.Get("required") != "0"
}
