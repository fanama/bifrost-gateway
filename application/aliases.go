package application

import (
	"bridge-gateway/application/auth"
	"bridge-gateway/application/catalog"
	"bridge-gateway/application/chat"
	"bridge-gateway/application/config"
	"bridge-gateway/application/embedding"
	"bridge-gateway/application/entity"
	"bridge-gateway/application/project"
	"bridge-gateway/application/provider"
	"bridge-gateway/domain"
)

type (
	ChatUseCase         = chat.ChatUseCase
	ChatResult          = chat.ChatResult
	ConfigUseCase       = config.ConfigUseCase
	ProjectUseCase      = project.ProjectUseCase
	APIKeyUseCase       = project.APIKeyUseCase
	AuthUseCase         = auth.AuthUseCase
	AuthResult          = auth.AuthResult
	ProviderUseCase     = provider.ProviderUseCase
	ModelUseCase        = catalog.ModelUseCase
	ModelCatalogUseCase = catalog.ModelCatalogUseCase
	EmbeddingUseCase    = embedding.EmbeddingUseCase
)

type EntityUseCase[T any, R domain.CrudRepository[T]] = entity.EntityUseCase[T, R]

func NewChatUseCase(configs domain.ChatConfigRepository, enrich *domain.EnrichmentService, llm domain.LLMProvider) *ChatUseCase {
	return chat.NewChatUseCase(configs, enrich, llm)
}

func NewConfigUseCase(repo domain.ChatConfigRepository) *ConfigUseCase {
	return config.NewConfigUseCase(repo)
}

func NewProjectUseCase(projects domain.ProjectRepository) *ProjectUseCase {
	return project.NewProjectUseCase(projects)
}

func NewAPIKeyUseCase(keys domain.APIKeyRepository, projects domain.ProjectRepository) *APIKeyUseCase {
	return project.NewAPIKeyUseCase(keys, projects)
}

func NewAuthUseCase(masterKey string, keys domain.APIKeyRepository, projects domain.ProjectRepository) *AuthUseCase {
	return auth.NewAuthUseCase(masterKey, keys, projects)
}

func NewProviderUseCase(providers domain.ProviderRepository) *ProviderUseCase {
	return provider.NewProviderUseCase(providers)
}

func NewModelUseCase(gatewayModels []domain.ModelInfo, configs domain.ChatConfigRepository, catalogRepo domain.ModelCatalogRepository) *ModelUseCase {
	return catalog.NewModelUseCase(gatewayModels, configs, catalogRepo)
}

func NewModelCatalogUseCase(catalogRepo domain.ModelCatalogRepository) *ModelCatalogUseCase {
	return catalog.NewModelCatalogUseCase(catalogRepo)
}

func NewEmbeddingUseCase(configs domain.ChatConfigRepository, provider domain.EmbeddingProvider) *EmbeddingUseCase {
	return embedding.NewEmbeddingUseCase(configs, provider)
}

func NewEntityUseCase[T any, R domain.CrudRepository[T]](repo R) EntityUseCase[T, R] {
	return entity.NewEntityUseCase[T, R](repo)
}
