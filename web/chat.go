package web

import (
	"encoding/json"
	"net/http"
	"strings"

	"bridge-gateway/domain"
)

// chatController regroupe les routes de la page Discussion : liste des
// configurations, envoi de message et edition de la configuration active.
type chatController struct {
	uiShared
	chat    chatService
	configs configService
}

func (s *chatController) pageChat(w http.ResponseWriter, r *http.Request) {
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

func (s *chatController) editChatConfigForm(w http.ResponseWriter, r *http.Request) {
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

func (s *chatController) updateChatConfig(w http.ResponseWriter, r *http.Request) {
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

func (s *chatController) chatSend(w http.ResponseWriter, r *http.Request) {
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
