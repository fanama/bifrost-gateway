package handlers

import (
	"encoding/json"
	"errors"
	"net/http"

	"bridge-gateway/domain"
)

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
// reponse legitimement vide d'une requete mal formee. Les deux chemins, stream ou
// non, repondent donc la meme erreur.
func validMessages(w http.ResponseWriter, req *domain.ChatRequest) bool {
	if len(req.Messages) > 0 {
		return true
	}
	writeAPIError(w, http.StatusBadRequest, errTypeInvalidRequest, "messages", "", "messages is required")
	return false
}
