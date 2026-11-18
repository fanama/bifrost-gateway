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
	rag     ragService
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
	patch := parseConfigForm(r, s.providerNames(r.Context()))
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
	message := strings.TrimSpace(r.FormValue("message"))
	prompt, sources, useRAG, err := s.chatPrompt(r, cfgID, message)
	if err != nil {
		s.renderToast(w, "#chat-error", err.Error())
		return
	}

	result, err := s.chat.Send(r.Context(), cfgID, chatHistory(r), prompt, reasoningOption(r))
	if err != nil {
		// Le formulaire ajoute le fragment a la fin de la discussion ; une
		// erreur doit au contraire remplacer le bandeau d'erreur existant.
		w.Header().Set("HX-Reswap", "innerHTML")
		s.renderToast(w, "#chat-error", err.Error())
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// Seul le nouvel echange est rendu : htmx l'ajoute a la fin du DOM, donc
	// les messages precedents restent affiches et sont renvoyes, intacts, au
	// prochain appel. Remplacer la liste entiere la tronquerait a la fenetre
	// d'historique du modele.
	s.render(w, "chat_turn", map[string]any{
		"UserMessage":      message,
		"AssistantMessage": lastReply(result.Messages),
		"ServedModel":      result.Model,
		"RequestedModel":   result.RequestedModel,
		"ReasoningEffort":  result.ReasoningEffort,
		"RAGUsed":          useRAG,
		"Sources":          sources,
	})
}

// chatHistory decode la conversation que la page renvoie a chaque tour.
func chatHistory(r *http.Request) []domain.ChatMessage {
	var history []domain.ChatMessage
	if raw := strings.TrimSpace(r.FormValue("history")); raw != "" {
		if err := json.Unmarshal([]byte(raw), &history); err != nil {
			return nil
		}
	}
	return history
}

// reasoningOption traduit le selecteur en surcharge ponctuelle : "selon la
// configuration" n'envoie aucune surcharge et ne modifie pas la configuration.
func reasoningOption(r *http.Request) *domain.RequestOptions {
	effort := strings.ToLower(strings.TrimSpace(r.FormValue("reasoning_effort")))
	if effort == "" {
		return nil
	}
	return &domain.RequestOptions{ReasoningEffort: &effort}
}

// lastReply extrait le contenu de la reponse ajoutee par l'use case.
func lastReply(messages []domain.ChatMessage) string {
	if n := len(messages); n > 0 {
		return messages[n-1].Content
	}
	return ""
}
