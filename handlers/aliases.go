package handlers

import (
	"bridge-gateway/handlers/chat"
	"bridge-gateway/handlers/embeddings"
	"bridge-gateway/handlers/models"
)

type (
	ChatHandler      = chat.ChatHandler
	EmbeddingHandler = embeddings.EmbeddingHandler
	ModelsHandler    = models.ModelsHandler
)

func NewChatHandler(enrichment Enricher, auth Authenticator, chatCompletion ChatCompletion, configs ActiveConfigLoader) *ChatHandler {
	return chat.NewChatHandler(enrichment, auth, chatCompletion, configs)
}

func NewEmbeddingHandler(auth Authenticator, embeddingsSvc Embedder, configs ConfigQuerier) *EmbeddingHandler {
	return embeddings.NewEmbeddingHandler(auth, embeddingsSvc, configs)
}

func NewModelsHandler(auth Authenticator, modelsSvc ModelLister) *ModelsHandler {
	return models.NewModelsHandler(auth, modelsSvc)
}
