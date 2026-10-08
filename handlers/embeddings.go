package handlers

import (
	"encoding/json"
	"errors"
	"net/http"

	"bridge-gateway/application"
	"bridge-gateway/domain"
)

type EmbeddingHandler struct {
	auth       *application.AuthUseCase
	embeddings *application.EmbeddingUseCase
	configs    *application.ConfigUseCase
}

func NewEmbeddingHandler(
	auth *application.AuthUseCase,
	embeddings *application.EmbeddingUseCase,
	configs *application.ConfigUseCase,
) *EmbeddingHandler {
	return &EmbeddingHandler{auth: auth, embeddings: embeddings, configs: configs}
}

func (h *EmbeddingHandler) HandleEmbedding(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	authResult, err := h.auth.Authenticate(r.Context(), r.Header.Get("Authorization"))
	if err != nil {
		writeAPIError(w, http.StatusUnauthorized, errTypeAuthentication, "", codeInvalidAPIKey, "invalid or missing API key")
		return
	}

	var req domain.EmbeddingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if len(req.Input) == 0 {
		writeAPIError(w, http.StatusBadRequest, errTypeInvalidRequest, "input", "", "input is required")
		return
	}

	var cfg *domain.ChatConfig
	if authResult.Scope == "project" {
		c, err := h.configs.Active(r.Context(), authResult.Project.ID)
		if err != nil {
			if errors.Is(err, domain.ErrNoActiveConfig) {
				writeError(w, http.StatusBadRequest, "no active configuration for this project; activate one from the UI")
				return
			}
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		cfg = c
	} else {
		list, err := h.configs.List(r.Context())
		if err != nil || len(list) == 0 {
			writeError(w, http.StatusBadRequest, "no configuration available for embedding")
			return
		}
		for i := range list {
			if list[i].Active {
				cfg = &list[i]
				break
			}
		}
		if cfg == nil {
			cfg = &list[0]
		}
	}

	resp, err := h.embeddings.EmbedWithConfig(r.Context(), cfg, &req)
	if err != nil {
		var invalid *domain.InvalidRequestError
		if errors.As(err, &invalid) {
			writeAPIError(w, http.StatusBadRequest, errTypeInvalidRequest, invalid.Param, "", invalid.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}
