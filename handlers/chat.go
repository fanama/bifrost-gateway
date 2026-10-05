package handlers

import (
	"crypto/rand"
	"encoding/hex"
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

func buildCompletionResponse(model, content string) ChatCompletionResponse {
	promptTokens, completionTokens := estimateTokens(content)
	return ChatCompletionResponse{
		ID:      newCompletionID(),
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
		ID:      newCompletionID(),
		Object:  "chat.completion",
		Created: time.Now().Unix(),
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

// writeChatError traduit une erreur du cas d'usage en reponse HTTP.
//
// Une surcharge invalide (response_format inconnu, schema mal forme) vient de la
// requete et doit donc etre un 400 param=response_format, pas une panne interne :
// le client peut la corriger, et OpenAI la classe de la meme facon.
func writeChatError(w http.ResponseWriter, err error) {
	var invalid *domain.InvalidRequestError
	if errors.As(err, &invalid) {
		writeAPIError(w, http.StatusBadRequest, errTypeInvalidRequest, invalid.Param, "", invalid.Error())
		return
	}

	var missingErr *domain.MissingAttributionError
	if errors.As(err, &missingErr) {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeError(w, http.StatusInternalServerError, err.Error())
}

// validMessages verifie la presence de messages, comme le chemin cle de projet.
//
// Sans cette verification, une requete vide sur le chemin master renvoyait 200
// et un flux SSE sans aucun texte : le client ne pouvait pas distinguer une
// reponse legitement vide d'une requete mal formee. Les deux chemins, stream ou
// non, repondent donc la meme erreur.
func validMessages(w http.ResponseWriter, req *domain.ChatRequest) bool {
	if len(req.Messages) > 0 {
		return true
	}
	writeAPIError(w, http.StatusBadRequest, errTypeInvalidRequest, "messages", "", "messages is required")
	return false
}

// newCompletionID fournit un identifiant unique par reponse, au format
// "chatcmpl-<hex>" comme chez OpenAI. Un identifiant constant casse les clients
// qui indexent leurs traces ou leur cache dessus. domain.NewID() n'est pas
// reutilise ici : son prefixe "cfg-" est un detail interne qui n'a rien a
// faire dans une reponse publique.
func newCompletionID() string {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		return "chatcmpl-000000000000000000000000"
	}
	return "chatcmpl-" + hex.EncodeToString(b)
}

// Types d'erreur du paquet d'erreur OpenAI. Les clients OpenAI les attendent
// tels quels dans le champ "type".
const (
	errTypeInvalidRequest = "invalid_request_error"
	errTypeAuthentication = "authentication_error"
	errTypeNotFound       = "not_found_error"
)

// Codes stables du champ "code", lus par plusieurs SDK.
const (
	codeInvalidAPIKey = "invalid_api_key"
	codeInvalidURL    = "invalid_url"
)

// writeError conserve le comportement historique (400/500 sur une requete
// malformee) en déléguant à l'enveloppe OpenAI standard.
func writeError(w http.ResponseWriter, code int, message string) {
	writeAPIError(w, code, errTypeInvalidRequest, "", "", message)
}

// writeAPIError écrit l'enveloppe d'erreur OpenAI : un objet "error" contenant
// message, type, param et code. param et code valent null quand ils ne
// s'appliquent pas, ce qui est la forme attendue par les SDK.
func writeAPIError(w http.ResponseWriter, status int, errType, param, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	body := map[string]any{
		"message": message,
		"type":    errType,
		"param":   nil,
		"code":    nil,
	}
	if param != "" {
		body["param"] = param
	}
	if code != "" {
		body["code"] = code
	}

	json.NewEncoder(w).Encode(map[string]any{"error": body})
}
