package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"bridge-gateway/application"
	"bridge-gateway/domain"
)

type ChatHandler struct {
	enrichment *domain.EnrichmentService
	auth       *application.AuthUseCase
	chat       *application.ChatUseCase
	configs    *application.ConfigUseCase
}

func NewChatHandler(
	enrichment *domain.EnrichmentService,
	auth *application.AuthUseCase,
	chat *application.ChatUseCase,
	configs *application.ConfigUseCase,
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

	if req.Stream {
		writeError(w, http.StatusBadRequest, "streaming is not supported")
		return
	}

	authResult, err := h.auth.Authenticate(r.Context(), r.Header.Get("Authorization"))
	if err != nil {
		writeError(w, http.StatusUnauthorized, "invalid or missing API key")
		return
	}

	if authResult.Scope == "master" {
		h.handleMasterRequest(w, r, &req)
		return
	}
	h.handleProjectRequest(w, r, &req, authResult.Project.ID)
}

func (h *ChatHandler) handleMasterRequest(w http.ResponseWriter, _ *http.Request, req *domain.ChatRequest) {
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
		writeError(w, http.StatusBadRequest, "messages is required")
		return
	}
	history := messages[:len(messages)-1]
	last := messages[len(messages)-1]

	result, err := h.chat.Send(r.Context(), cfg.ID, history, last.Content)
	if err != nil {
		var missingErr *domain.MissingAttributionError
		if errors.As(err, &missingErr) {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	reply := ""
	if len(result) > 0 {
		reply = result[len(result)-1].Content
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(buildCompletionResponse(cfg.Model, reply))
}

func buildCompletionResponse(model, content string) ChatCompletionResponse {
	promptTokens, completionTokens := estimateTokens(content)
	return ChatCompletionResponse{
		ID:      "cmpl-bridge-gateway",
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   model,
		Choices: []Choice{
			{
				Index: 0,
				Message: domain.ChatMessage{
					Role:    "assistant",
					Content: content,
				},
				FinishReason: "stop",
			},
		},
		Usage: Usage{
			PromptTokens:     promptTokens,
			CompletionTokens: completionTokens,
			TotalTokens:      promptTokens + completionTokens,
		},
	}
}

func estimateTokens(text string) (prompt, completion int) {
	words := len(strings.Fields(text))
	completion = words/2 + 1
	return completion, completion
}

func (h *ChatHandler) HandleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

type ChatCompletionResponse struct {
	ID      string   `json:"id"`
	Object  string   `json:"object"`
	Created int64    `json:"created"`
	Model   string   `json:"model"`
	Choices []Choice `json:"choices"`
	Usage   Usage    `json:"usage"`
}

type Choice struct {
	Index        int                `json:"index"`
	Message      domain.ChatMessage `json:"message"`
	FinishReason string             `json:"finish_reason"`
}

type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

func buildResponse(req *domain.EnrichedRequest) ChatCompletionResponse {
	content := ""
	if len(req.Messages) > 0 {
		content = "Enriched request processed. Model: " + req.Model
	}

	return ChatCompletionResponse{
		ID:      "cmpl-bridge-gateway",
		Object:  "chat.completion",
		Created: 0,
		Model:   req.Model,
		Choices: []Choice{
			{
				Index: 0,
				Message: domain.ChatMessage{
					Role:    "assistant",
					Content: content,
				},
				FinishReason: "stop",
			},
		},
		Usage: Usage{
			PromptTokens:     0,
			CompletionTokens: 0,
			TotalTokens:      0,
		},
	}
}

func writeError(w http.ResponseWriter, code int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]string{
			"message": message,
			"type":    "invalid_request_error",
		},
	})
}
