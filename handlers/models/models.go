package models

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"bridge-gateway/domain"
	"bridge-gateway/handlers/httpx"
)

// ModelsHandler exposes OpenAI-compatible model discovery endpoints.
type ModelsHandler struct {
	auth   httpx.Authenticator
	models httpx.ModelLister
}

// NewModelsHandler constructs a ModelsHandler with narrow consumer-side ports.
func NewModelsHandler(auth httpx.Authenticator, models httpx.ModelLister) *ModelsHandler {
	return &ModelsHandler{auth: auth, models: models}
}

// ModelList is the response payload for GET /v1/models.
type ModelList struct {
	Object string       `json:"object"`
	Data   []ModelEntry `json:"data"`
}

// ModelEntry is a single model item in the OpenAI models list response.
type ModelEntry struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	OwnedBy string `json:"owned_by"`
}

// HandleList serves GET /v1/models.
func (h *ModelsHandler) HandleList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httpx.WriteMethodNotAllowed(w)
		return
	}
	if _, err := h.auth.Authenticate(r.Context(), r.Header.Get("Authorization")); err != nil {
		httpx.WriteUnauthorized(w)
		return
	}

	models, err := h.models.List(r.Context())
	if err != nil {
		httpx.WriteDomainError(w, err)
		return
	}

	created := time.Now().Unix()
	data := make([]ModelEntry, 0, len(models))
	for _, m := range models {
		data = append(data, newModelEntry(m, created))
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(ModelList{Object: "list", Data: data})
}

// HandleRetrieve serves GET /v1/models/{model} (case-insensitive match).
func (h *ModelsHandler) HandleRetrieve(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httpx.WriteMethodNotAllowed(w)
		return
	}
	if _, err := h.auth.Authenticate(r.Context(), r.Header.Get("Authorization")); err != nil {
		httpx.WriteUnauthorized(w)
		return
	}

	wanted := strings.TrimSpace(r.PathValue("model"))
	if wanted == "" {
		httpx.WriteAPIError(w, http.StatusBadRequest, httpx.ErrTypeInvalidRequest, "model", "", "model is required")
		return
	}

	models, err := h.models.List(r.Context())
	if err != nil {
		httpx.WriteDomainError(w, err)
		return
	}

	for _, m := range models {
		if matchesModel(m, wanted) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(newModelEntry(m, time.Now().Unix()))
			return
		}
	}

	httpx.WriteAPIError(w, http.StatusNotFound, httpx.ErrTypeNotFound, "model", httpx.CodeModelNotFound,
		"The model '"+wanted+"' does not exist")
}

func matchesModel(m domain.ModelInfo, wanted string) bool {
	return strings.EqualFold(m.Name, wanted) || (m.ID != "" && strings.EqualFold(m.ID, wanted))
}

func newModelEntry(m domain.ModelInfo, created int64) ModelEntry {
	owner := strings.TrimSpace(m.Provider)
	if owner == "" {
		owner = "bridge-gateway"
	}
	return ModelEntry{
		ID:      m.Name,
		Object:  "model",
		Created: created,
		OwnedBy: owner,
	}
}

// HandleNotFound responds to unknown /v1/ routes with an OpenAI JSON error envelope.
func (h *ModelsHandler) HandleNotFound(w http.ResponseWriter, r *http.Request) {
	httpx.WriteAPIError(w, http.StatusNotFound, httpx.ErrTypeInvalidRequest, "", httpx.CodeInvalidURL,
		"Invalid URL ("+r.Method+" "+r.URL.Path+")")
}
