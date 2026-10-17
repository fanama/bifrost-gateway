package handlers

import (
	"net/http"

	"bridge-gateway/domain"
	"bridge-gateway/handlers/httpx"
)

const (
	errTypeInvalidRequest = httpx.ErrTypeInvalidRequest
	errTypeAuthentication = httpx.ErrTypeAuthentication
	errTypeNotFound       = httpx.ErrTypeNotFound

	codeInvalidAPIKey = httpx.CodeInvalidAPIKey
	codeInvalidURL    = httpx.CodeInvalidURL
)

func writeError(w http.ResponseWriter, code int, message string) {
	httpx.WriteError(w, code, message)
}

func writeAPIError(w http.ResponseWriter, status int, errType, param, code, message string) {
	httpx.WriteAPIError(w, status, errType, param, code, message)
}

func writeChatError(w http.ResponseWriter, err error) {
	httpx.WriteDomainError(w, err)
}

func validMessages(w http.ResponseWriter, req *domain.ChatRequest) bool {
	return httpx.ValidMessages(w, req)
}
