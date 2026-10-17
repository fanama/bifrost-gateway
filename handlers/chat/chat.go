package chat

import (
	"encoding/json"
	"net/http"

	"bridge-gateway/domain"
	"bridge-gateway/handlers/httpx"
)

// ChatHandler serves OpenAI-compatible chat completion and health endpoints.
type ChatHandler struct {
	enrichment httpx.Enricher
	auth       httpx.Authenticator
	chat       httpx.ChatCompletion
	configs    httpx.ActiveConfigLoader
}

// NewChatHandler constructs a ChatHandler with narrow consumer-side ports.
func NewChatHandler(
	enrichment httpx.Enricher,
	auth httpx.Authenticator,
	chat httpx.ChatCompletion,
	configs httpx.ActiveConfigLoader,
) *ChatHandler {
	return &ChatHandler{enrichment: enrichment, auth: auth, chat: chat, configs: configs}
}

// HandleChatCompletion handles POST /v1/chat/completions (both JSON and SSE).
func (h *ChatHandler) HandleChatCompletion(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httpx.WriteMethodNotAllowed(w)
		return
	}

	var req domain.ChatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteInvalidBody(w)
		return
	}

	authResult, err := h.auth.Authenticate(r.Context(), r.Header.Get("Authorization"))
	if err != nil {
		httpx.WriteUnauthorized(w)
		return
	}

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
	if !httpx.ValidMessages(w, req) {
		return
	}

	enriched, err := h.enrichment.Enrich(req)
	if err != nil {
		httpx.WriteDomainError(w, err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(buildResponse(enriched))
}

func (h *ChatHandler) handleProjectRequest(w http.ResponseWriter, r *http.Request, req *domain.ChatRequest, projectID string) {
	cfg, err := h.configs.Active(r.Context(), projectID)
	if err != nil {
		httpx.WriteDomainError(w, err)
		return
	}
	if !httpx.ValidMessages(w, req) {
		return
	}

	messages := req.Messages
	history := messages[:len(messages)-1]
	last := messages[len(messages)-1]

	result, err := h.chat.Send(r.Context(), cfg.ID, history, last.Content, domain.OptionsFrom(req))
	if err != nil {
		httpx.WriteDomainError(w, err)
		return
	}

	reply := ""
	if msgs := result.Messages; len(msgs) > 0 {
		reply = msgs[len(msgs)-1].Content
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(buildCompletionResponse(result.Model, reply))
}

// HandleHealth responds to liveness, readiness, and connection probes.
func (h *ChatHandler) HandleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}
