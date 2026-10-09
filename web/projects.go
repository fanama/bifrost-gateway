package web

import (
	"context"
	"net/http"
	"strings"
	"time"

	"bridge-gateway/domain"
)

// projectsController regroupe les projets multi-tenant : liste, detail,
// configurations rattachees (activation, duplication) et cles API.
type projectsController struct {
	uiShared
	projects projectService
	configs  configService
	keys     keyService
}

// ---- Projects ----

type projectSummary struct {
	domain.Project
	ConfigCount int
}

func (s *projectsController) projectSummaries(ctx context.Context) ([]projectSummary, error) {
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

func (s *projectsController) pageProjects(w http.ResponseWriter, r *http.Request) {
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

func (s *projectsController) createProject(w http.ResponseWriter, r *http.Request) {
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

func (s *projectsController) pageProjectDetail(w http.ResponseWriter, r *http.Request) {
	pid := r.PathValue("pid")
	project, err := s.projects.Get(r.Context(), pid)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	s.render(w, "page_project_detail", s.projectDetailData(r.Context(), pid, project, ""))
}

func (s *projectsController) projectDetailData(ctx context.Context, pid string, project *domain.Project, newKey string) map[string]any {
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

func (s *projectsController) renderProjectConfigsSection(w http.ResponseWriter, r *http.Request, pid string) {
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

func (s *projectsController) renderProjectKeysSection(w http.ResponseWriter, r *http.Request, pid string, newKey string) {
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

func (s *projectsController) createProjectKey(w http.ResponseWriter, r *http.Request) {
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

func (s *projectsController) deleteProjectKey(w http.ResponseWriter, r *http.Request) {
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
