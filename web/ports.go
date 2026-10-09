package web

import (
	"context"

	"bridge-gateway/application"
	"bridge-gateway/domain"
)

// Ports de l'UI : interfaces declarees cote consommateur (le serveur HTMX),
// avec le minimum d'operations utiles a chaque cas d'usage (ISP). Le serveur
// ne depend donc plus des types concrets des cas d'usage, mais d'abstractions
// qu'il declare lui-meme (DIP) : un stub de test remplace un cas d'usage sans
// instancier toute la chaine, et toute nouvelle methode ajoutee a un use case
// n'elargit pas automatiquement la surface du serveur.

// chatService envoie un message et rend la reponse enrichie.
type chatService interface {
	Send(ctx context.Context, cfgID string, history []domain.ChatMessage, message string, opts *domain.RequestOptions) (application.ChatResult, error)
}

// configService gere les configurations LLM depuis l'UI.
type configService interface {
	List(ctx context.Context) ([]domain.ChatConfig, error)
	ListByProject(ctx context.Context, projectID string) ([]domain.ChatConfig, error)
	Get(ctx context.Context, id string) (*domain.ChatConfig, error)
	Create(ctx context.Context, cfg *domain.ChatConfig) (*domain.ChatConfig, error)
	Update(ctx context.Context, id string, patch *domain.ChatConfig) (*domain.ChatConfig, error)
	Delete(ctx context.Context, id string) error
	SetActive(ctx context.Context, projectID string, id string) (*domain.ChatConfig, error)
}

// modelService rend les modeles connus, fusionnes catalogue + config + configs.
type modelService interface {
	List(ctx context.Context) ([]domain.ModelInfo, error)
	ListForProvider(ctx context.Context, provider string) ([]domain.ModelInfo, error)
}

// projectService gere les projets multi-tenant.
type projectService interface {
	List(ctx context.Context) ([]domain.Project, error)
	Get(ctx context.Context, id string) (*domain.Project, error)
	Create(ctx context.Context, name, description string) (*domain.Project, error)
}

// keyService gere les cles API de projet (secret affiche une seule fois).
type keyService interface {
	ListByProject(ctx context.Context, projectID string) ([]domain.APIKey, error)
	Get(ctx context.Context, id string) (*domain.APIKey, error)
	Create(ctx context.Context, projectID, name string) (*domain.APIKey, string, error)
	Delete(ctx context.Context, id string) error
}

// providerService gere les providers (nom, base URL, cle par defaut).
type providerService interface {
	List(ctx context.Context) ([]domain.Provider, error)
	Get(ctx context.Context, id string) (*domain.Provider, error)
	Create(ctx context.Context, name, baseURL, apiKey string) (*domain.Provider, error)
	Update(ctx context.Context, id, name, baseURL, apiKey string) (*domain.Provider, error)
	Delete(ctx context.Context, id string) error
}

// catalogService ajoute et retire des modeles du catalogue UI.
type catalogService interface {
	Create(ctx context.Context, name, provider, kind string) (*domain.CatalogModel, error)
	Delete(ctx context.Context, id string) error
}

// embeddingService execute un embedding pour le testeur.
type embeddingService interface {
	EmbedWithConfig(ctx context.Context, cfg *domain.ChatConfig, req *domain.EmbeddingRequest) (*domain.EmbeddingResponse, error)
}
