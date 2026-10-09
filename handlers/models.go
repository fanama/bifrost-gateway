package handlers

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"bridge-gateway/domain"
)

// ModelsHandler expose la decouverte de modeles sur l'API standard OpenAI.
// La liste provient du meme use case que l'UI : catalogue gerable, modeles de
// config.yaml et modeles utilises par les configurations enregistrees.
type ModelsHandler struct {
	auth   Authenticator
	models ModelLister
}

func NewModelsHandler(auth Authenticator, models ModelLister) *ModelsHandler {
	return &ModelsHandler{auth: auth, models: models}
}

// ModelList est la reponse de GET /v1/models.
type ModelList struct {
	Object string       `json:"object"`
	Data   []ModelEntry `json:"data"`
}

// ModelEntry est un element de la liste : la forme attendue par les SDK
// OpenAI, qui lisent "id" et "object".
type ModelEntry struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	OwnedBy string `json:"owned_by"`
}

// HandleList repond a GET /v1/models : {"object":"list","data":[...]}.
func (h *ModelsHandler) HandleList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	if _, err := h.auth.Authenticate(r.Context(), r.Header.Get("Authorization")); err != nil {
		writeAPIError(w, http.StatusUnauthorized, errTypeAuthentication, "", codeInvalidAPIKey, "invalid or missing API key")
		return
	}

	models, err := h.models.List(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	created := time.Now().Unix()
	data := make([]ModelEntry, 0, len(models))
	for _, m := range models {
		data = append(data, newModelEntry(m, created))
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(ModelList{Object: "list", Data: data})
}

// HandleRetrieve repond a GET /v1/models/{model}. La comparaison est
// insensible a la casse, comme chez OpenAI ou "gpt-4o" et "GPT-4O" designent
// le meme modele.
func (h *ModelsHandler) HandleRetrieve(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	if _, err := h.auth.Authenticate(r.Context(), r.Header.Get("Authorization")); err != nil {
		writeAPIError(w, http.StatusUnauthorized, errTypeAuthentication, "", codeInvalidAPIKey, "invalid or missing API key")
		return
	}

	wanted := strings.TrimSpace(r.PathValue("model"))
	if wanted == "" {
		writeAPIError(w, http.StatusBadRequest, errTypeInvalidRequest, "model", "", "model is required")
		return
	}

	models, err := h.models.List(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	for _, m := range models {
		if strings.EqualFold(m.Name, wanted) || (m.ID != "" && strings.EqualFold(m.ID, wanted)) {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(newModelEntry(m, time.Now().Unix()))
			return
		}
	}

	writeAPIError(w, http.StatusNotFound, errTypeNotFound, "model", "model_not_found",
		"The model '"+wanted+"' does not exist")
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

// HandleNotFound repond aux routes /v1/ inconnues avec l'enveloppe d'erreur
// OpenAI. Sans cela la reponse est le 404 en texte brut de net/http, que les
// SDK ne savent pas lire. Enregistre sur "/v1/", ce qui reste plus specifique
// que les routes declarees (/v1/models/...), donc ne les masque pas.
func (h *ModelsHandler) HandleNotFound(w http.ResponseWriter, r *http.Request) {
	writeAPIError(w, http.StatusNotFound, errTypeInvalidRequest, "", codeInvalidURL,
		"Invalid URL ("+r.Method+" "+r.URL.Path+")")
}
