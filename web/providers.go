package web

import (
	"net/http"

	"bridge-gateway/domain"
)

// providersController regroupe la gestion des providers (nom, base URL,
// cle API par defaut) depuis la page /providers.
type providersController struct {
	uiShared
}

// ---- Providers ----

func (s *providersController) pageProviders(w http.ResponseWriter, r *http.Request) {
	providers := s.providerList(r.Context())
	s.render(w, "page_providers", map[string]any{
		"Active":    "providers",
		"Title":     "Providers",
		"Providers": providers,
		"Create":    providerFormData{Action: "/providers"},
	})
}

func (s *providersController) createProvider(w http.ResponseWriter, r *http.Request) {
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

func (s *providersController) updateProvider(w http.ResponseWriter, r *http.Request) {
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

func (s *providersController) deleteProvider(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.providers.Delete(r.Context(), id); err != nil {
		s.renderToast(w, "#providers-error", err.Error())
		return
	}
	s.renderProvidersList(w, r)
}

func (s *providersController) editProviderForm(w http.ResponseWriter, r *http.Request) {
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

func (s *providersController) renderProvidersList(w http.ResponseWriter, r *http.Request) {
	providers := s.providerList(r.Context())
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	s.render(w, "provider_rows", map[string]any{"Providers": providers})
}

type providerFormData struct {
	Editing *domain.Provider
	Action  string
}
