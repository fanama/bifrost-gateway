package handlers

import (
	"encoding/json"
	"errors"
	"net/http"

	"bridge-gateway/domain"
)

type ChatHandler struct {
	enrichment Enricher
	auth       Authenticator
	chat       ChatCompletion
	configs    ActiveConfigLoader
}

func NewChatHandler(
	enrichment Enricher,
	auth Authenticator,
	chat ChatCompletion,
	configs ActiveConfigLoader,
) *ChatHandler {
	return &ChatHandler{enrichment: enrichment, auth: auth, chat: chat, configs: configs}
}

func (h *ChatHandler) HandleChatCompletion(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req domain.ChatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	authResult, err := h.auth.Authenticate(r.Context(), r.Header.Get("Authorization"))
	if err != nil {
		writeAPIError(w, http.StatusUnauthorized, errTypeAuthentication, "", codeInvalidAPIKey, "invalid or missing API key")
		return
	}

	// Le mode est decide apres l'authentification : une cle invalide doit
	// toujours produire un 401, jamais un flux de donnees.
	if authResult.Scope == "master" {
		if req.Stream {
			h.handleMasterStream(w, r, &req)
			return
		}
		h.handleMasterRequest(w, r, &req)
		return
	}
	if req.Stream {
		h.handleProjectStream(w, r, &req, authResult.Project.ID)
		return
	}
	h.handleProjectRequest(w, r, &req, authResult.Project.ID)
}

func (h *ChatHandler) handleMasterRequest(w http.ResponseWriter, _ *http.Request, req *domain.ChatRequest) {
	if !validMessages(w, req) {
		return
	}

	enriched, err := h.enrichment.Enrich(req)
	if err != nil {
		var missingErr *domain.MissingAttributionError
		if errors.As(err, &missingErr) {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(buildResponse(enriched))
}

func (h *ChatHandler) handleProjectRequest(w http.ResponseWriter, r *http.Request, req *domain.ChatRequest, projectID string) {
	cfg, err := h.configs.Active(r.Context(), projectID)
	if err != nil {
		if errors.Is(err, domain.ErrNoActiveConfig) {
			writeError(w, http.StatusBadRequest, "no active configuration for this project; activate one from the UI")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	messages := req.Messages
	if len(messages) == 0 {
		writeAPIError(w, http.StatusBadRequest, errTypeInvalidRequest, "messages", "", "messages is required")
		return
	}
	history := messages[:len(messages)-1]
	last := messages[len(messages)-1]

	result, err := h.chat.Send(r.Context(), cfg.ID, history, last.Content, domain.OptionsFrom(req))
	if err != nil {
		writeChatError(w, err)
		return
	}

	reply := ""
	if msgs := result.Messages; len(msgs) > 0 {
		reply = msgs[len(msgs)-1].Content
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(buildCompletionResponse(result.Model, reply))
}

func (h *ChatHandler) HandleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}
