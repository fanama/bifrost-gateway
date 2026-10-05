package domain

import (
	"errors"
	"fmt"
)

var (
	ErrConfigNotFound       = errors.New("configuration not found")
	ErrProjectNotFound      = errors.New("project not found")
	ErrAPIKeyNotFound       = errors.New("api key not found")
	ErrProviderNotFound     = errors.New("provider not found")
	ErrCatalogModelNotFound = errors.New("catalog model not found")
	ErrNoActiveConfig       = errors.New("no active configuration for this project")
	ErrInvalidAPIKey        = errors.New("invalid or missing API key")
)

type DomainError struct {
	Message string
}

func (e *DomainError) Error() string {
	return e.Message
}

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

// InvalidRequestError signale un parametre de requete rejete, en distinguant le
// champ fautif. Les handlers le traduisent en 400 param=..., la ou une
// ValidationError concerne une configuration enregistree.
type InvalidRequestError struct {
	Param  string
	Reason string
}

func (e *InvalidRequestError) Error() string {
	return e.Reason
}

type MissingAttributionError struct {
	MissingFields []string
}

func (e *MissingAttributionError) Error() string {
	return fmt.Sprintf("Missing required attribution: %s — configure on team metadata.", joinFields(e.MissingFields))
}

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
