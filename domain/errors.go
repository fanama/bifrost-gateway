package domain

import "bridge-gateway/domain/errs"

// Error aliases re-exported from domain/errs so existing domain callers keep
// working while new code can reference domain/errs directly.
type (
	ErrorCode               = errs.Code
	Error                   = errs.Error
	DomainError             = errs.DomainError
	ValidationError         = errs.ValidationError
	InvalidRequestError     = errs.InvalidRequestError
	MissingAttributionError = errs.MissingAttributionError
)

var (
	ErrConfigNotFound       = errs.ErrConfigNotFound
	ErrProjectNotFound      = errs.ErrProjectNotFound
	ErrAPIKeyNotFound       = errs.ErrAPIKeyNotFound
	ErrProviderNotFound     = errs.ErrProviderNotFound
	ErrCatalogModelNotFound = errs.ErrCatalogModelNotFound
	ErrNoActiveConfig       = errs.ErrNoActiveConfig
	ErrInvalidAPIKey        = errs.ErrInvalidAPIKey
)
