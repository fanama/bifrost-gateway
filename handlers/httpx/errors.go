package httpx

import (
	"encoding/json"
	"errors"
	"net/http"

	"bridge-gateway/domain"
	"bridge-gateway/domain/errs"
)

// OpenAI error envelope "type" values expected by SDK clients.
const (
	ErrTypeInvalidRequest = "invalid_request_error"
	ErrTypeAuthentication = "authentication_error"
	ErrTypeNotFound       = "not_found_error"
	ErrTypeRateLimit      = "rate_limit_error"
	ErrTypeServer         = "server_error"
)

// Stable OpenAI error "code" values read by SDK clients.
const (
	CodeInvalidAPIKey = "invalid_api_key"
	CodeInvalidURL    = "invalid_url"
	CodeModelNotFound = "model_not_found"
)

// WriteError writes an OpenAI-compatible JSON error envelope with the given
// HTTP status code and message.
func WriteError(w http.ResponseWriter, status int, message string) {
	WriteAPIError(w, status, ErrTypeInvalidRequest, "", "", message)
}

// WriteAPIError writes the standard OpenAI error envelope:
// {"error":{"message":..., "type":..., "param":..., "code":...}}.
func WriteAPIError(w http.ResponseWriter, status int, errType, param, code, message string) {
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

	_ = json.NewEncoder(w).Encode(map[string]any{"error": body})
}

// WriteUnauthorized writes the standard 401 invalid_api_key response.
func WriteUnauthorized(w http.ResponseWriter) {
	WriteAPIError(w, http.StatusUnauthorized, ErrTypeAuthentication, "", CodeInvalidAPIKey, "invalid or missing API key")
}

// WriteMethodNotAllowed writes the standard 405 response.
func WriteMethodNotAllowed(w http.ResponseWriter) {
	WriteError(w, http.StatusMethodNotAllowed, "method not allowed")
}

// WriteInvalidBody writes the standard 400 malformed JSON response.
func WriteInvalidBody(w http.ResponseWriter) {
	WriteError(w, http.StatusBadRequest, "invalid request body")
}

// WriteDomainError maps a domain/application error to its HTTP status and
// OpenAI error envelope using errs.CodeOf and errs.ParamOf.
func WriteDomainError(w http.ResponseWriter, err error) {
	if errors.Is(err, domain.ErrNoActiveConfig) {
		WriteError(w, http.StatusBadRequest, "no active configuration for this project; activate one from the UI")
		return
	}
	status, errType, apiCode := MapErrorCode(errs.CodeOf(err))
	WriteAPIError(w, status, errType, errs.ParamOf(err), apiCode, err.Error())
}

// MapErrorCode maps a stable domain error code to an HTTP status, OpenAI error
// type, and optional OpenAI error code.
func MapErrorCode(code errs.Code) (status int, errType string, apiCode string) {
	switch code {
	case errs.CodeValidation, errs.CodeInvalidRequest:
		return http.StatusBadRequest, ErrTypeInvalidRequest, ""
	case errs.CodeUnauthorized:
		return http.StatusUnauthorized, ErrTypeAuthentication, CodeInvalidAPIKey
	case errs.CodeForbidden:
		return http.StatusForbidden, ErrTypeAuthentication, ""
	case errs.CodeNotFound:
		return http.StatusNotFound, ErrTypeNotFound, ""
	case errs.CodeConflict:
		return http.StatusConflict, ErrTypeInvalidRequest, ""
	case errs.CodeRateLimited:
		return http.StatusTooManyRequests, ErrTypeRateLimit, ""
	case errs.CodeProviderUnavailable:
		return http.StatusBadGateway, ErrTypeServer, ""
	case errs.CodeTimeout:
		return http.StatusGatewayTimeout, ErrTypeServer, ""
	case errs.CodeCanceled:
		return 499, ErrTypeInvalidRequest, ""
	default:
		return http.StatusInternalServerError, ErrTypeInvalidRequest, ""
	}
}

// ValidMessages checks that req.Messages is non-empty and writes a 400
// OpenAI error envelope when empty.
func ValidMessages(w http.ResponseWriter, req *domain.ChatRequest) bool {
	if len(req.Messages) > 0 {
		return true
	}
	WriteAPIError(w, http.StatusBadRequest, ErrTypeInvalidRequest, "messages", "", "messages is required")
	return false
}
