package handlers

import (
	"context"

	"bridge-gateway/application"
	"bridge-gateway/domain"
)

// Ports du presentoir : interfaces declarees cote consommateur, avec
// exactement les operations dont chaque handler a besoin (ISP), et sans
// reference aux types concrets des cas d'usage (DIP). Les implementations
// concretes du paquet application les satisfont automatiquement ; un stub de
// test aussi, sans avoir a instancier tout un cas d'usage.

// Authenticator authentifie un appel Bearer (master key ou cle de projet).
type Authenticator interface {
	Authenticate(ctx context.Context, bearer string) (*application.AuthResult, error)
}

// Enricher applique le pipeline d'enrichissement a une requete entrante.
type Enricher interface {
	Enrich(req *domain.ChatRequest) (*domain.EnrichedRequest, error)
}

// ChatCompletion ouvre les deux chemins de reponse du cas d'usage chat.
type ChatCompletion interface {
	Send(ctx context.Context, cfgID string, history []domain.ChatMessage, message string, opts *domain.RequestOptions) (application.ChatResult, error)
	Stream(ctx context.Context, cfgID string, history []domain.ChatMessage, message string, opts *domain.RequestOptions) (<-chan domain.StreamEvent, error)
}

// ActiveConfigLoader resout la configuration active d'un projet.
type ActiveConfigLoader interface {
	Active(ctx context.Context, projectID string) (*domain.ChatConfig, error)
}

// ConfigQuerier etend ActiveConfigLoader avec la liste complete des
// configurations (repli du chemin master, sans projet).
type ConfigQuerier interface {
	ActiveConfigLoader
	List(ctx context.Context) ([]domain.ChatConfig, error)
}

// ModelLister rend la liste fusionnee des modeles connus.
type ModelLister interface {
	List(ctx context.Context) ([]domain.ModelInfo, error)
}

// Embedder execute un embedding pour une configuration donnee.
type Embedder interface {
	EmbedWithConfig(ctx context.Context, cfg *domain.ChatConfig, req *domain.EmbeddingRequest) (*domain.EmbeddingResponse, error)
}
