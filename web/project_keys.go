package web

import "net/http"

// Gestion des cles API d'un projet : affichage de la section apres une
// creation (le secret n'est montre qu'une fois) ou une revocation.

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
