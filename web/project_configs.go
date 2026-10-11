package web

import (
	"net/http"
	"strings"
	"time"
)

// Options des configurations rattachees a un projet : creation, edition,
// suppression, duplication, activation et selecteurs de modeles.

func (s *projectsController) createProjectConfig(w http.ResponseWriter, r *http.Request) {
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

func (s *projectsController) updateProjectConfig(w http.ResponseWriter, r *http.Request) {
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

func (s *projectsController) projectConfigModels(w http.ResponseWriter, r *http.Request) {
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
