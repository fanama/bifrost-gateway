package errs

import (
	"context"
	"errors"
	"fmt"
)

// Code is a stable machine-readable error classification shared across domain,
// application, adapters and delivery layers.
type Code string

const (
	CodeValidation          Code = "validation_error"
	CodeInvalidRequest      Code = "invalid_request"
	CodeNotFound            Code = "not_found"
	CodeConflict            Code = "conflict"
	CodeUnauthorized        Code = "unauthorized"
	CodeForbidden           Code = "forbidden"
	CodeRateLimited         Code = "rate_limited"
	CodeProviderUnavailable Code = "provider_unavailable"
	CodeTimeout             Code = "timeout"
	CodeCanceled            Code = "canceled"
	CodeInternal            Code = "internal_error"
)

// Error carries a stable Code, a user-visible message, an optional request
// parameter or field name, and an optional wrapped cause.
type Error struct {
	Code    Code
	Message string
	Param   string
	Cause   error
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	if e.Message != "" {
		return e.Message
	}
	if e.Cause != nil {
		return e.Cause.Error()
	}
	return string(e.Code)
}

func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

// New constructs a typed Error with the given code and message.
func New(code Code, message string) *Error {
	return &Error{Code: code, Message: message}
}

// WithParam constructs a typed Error bound to a request parameter or field.
func WithParam(code Code, param, message string) *Error {
	return &Error{Code: code, Param: param, Message: message}
}

// Wrap attaches a stable Code and message to an underlying cause while keeping
// errors.Is / errors.As traversal intact.
func Wrap(code Code, message string, cause error) *Error {
	if cause == nil {
		return New(code, message)
	}
	if message == "" {
		message = cause.Error()
	}
	return &Error{Code: code, Message: message, Cause: cause}
}

// CodeOf extracts the stable Code from err, falling back to context
// cancellation/timeout inspection and finally CodeInternal.
func CodeOf(err error) Code {
	if err == nil {
		return ""
	}
	var typed *Error
	if errors.As(err, &typed) && typed.Code != "" {
		return typed.Code
	}
	var coded interface{ ErrorCode() Code }
	if errors.As(err, &coded) && coded.ErrorCode() != "" {
		return coded.ErrorCode()
	}
	if errors.Is(err, context.Canceled) {
		return CodeCanceled
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return CodeTimeout
	}
	return CodeInternal
}

// ParamOf returns the parameter name associated with err, if any.
func ParamOf(err error) string {
	if err == nil {
		return ""
	}
	var typed *Error
	if errors.As(err, &typed) && typed.Param != "" {
		return typed.Param
	}
	var paramed interface{ ErrorParam() string }
	if errors.As(err, &paramed) {
		return paramed.ErrorParam()
	}
	return ""
}

// Domain-wide sentinels promoted to typed errors so both errors.Is(err, ErrX)
// and errs.CodeOf(err) work without losing identity.
var (
	ErrConfigNotFound       error = New(CodeNotFound, "configuration not found")
	ErrProjectNotFound      error = New(CodeNotFound, "project not found")
	ErrAPIKeyNotFound       error = New(CodeNotFound, "api key not found")
	ErrProviderNotFound     error = New(CodeNotFound, "provider not found")
	ErrCatalogModelNotFound error = New(CodeNotFound, "catalog model not found")
	ErrNoActiveConfig       error = New(CodeInvalidRequest, "no active configuration for this project")
	ErrInvalidAPIKey        error = New(CodeUnauthorized, "invalid or missing API key")
)

// DomainError is a generic domain-level validation or business rule error.
type DomainError struct {
	Message string
}

func (e *DomainError) Error() string   { return e.Message }
func (e *DomainError) ErrorCode() Code { return CodeValidation }

// ValidationError reports one or more invalid configuration or entity fields.
// User-visible French wording is preserved for UI compatibility.
type ValidationError struct {
	Fields []string
	Reason string
}

func (e *ValidationError) Error() string {
	detail := e.Reason
	if detail == "" {
		detail = "champ(s) requis"
		return fmt.Sprintf("%s: %s", detail, joinFields(e.Fields))
	}
	return fmt.Sprintf("Champ(s) invalide(s) %s : %s", joinFields(e.Fields), detail)
}

func (e *ValidationError) ErrorCode() Code { return CodeValidation }

func (e *ValidationError) ErrorParam() string {
	if len(e.Fields) > 0 {
		return e.Fields[0]
	}
	return ""
}

// InvalidRequestError reports a rejected request parameter (mapped to HTTP 400
// with param set in the OpenAI error envelope).
type InvalidRequestError struct {
	Param  string
	Reason string
}

func (e *InvalidRequestError) Error() string      { return e.Reason }
func (e *InvalidRequestError) ErrorCode() Code    { return CodeInvalidRequest }
func (e *InvalidRequestError) ErrorParam() string { return e.Param }

// MissingAttributionError reports missing required attribution metadata.
type MissingAttributionError struct {
	MissingFields []string
}

func (e *MissingAttributionError) Error() string {
	return fmt.Sprintf("Missing required attribution: %s — configure on team metadata.", joinFields(e.MissingFields))
}

func (e *MissingAttributionError) ErrorCode() Code { return CodeInvalidRequest }

func joinFields(fields []string) string {
	result := ""
	for i, f := range fields {
		if i > 0 {
			result += ", "
		}
		result += f
	}
	return result
}
