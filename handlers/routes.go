package handlers

import "net/http"

// RegisterRoutes registers the OpenAI-compatible /v1 API surface on mux.
// Keeping this isolated from main() ensures contract tests exercise the exact
// same route registrations as production.
func RegisterRoutes(mux *http.ServeMux, chat *ChatHandler, models *ModelsHandler, embeddings *EmbeddingHandler) {
	mux.HandleFunc("/v1/chat/completions", chat.HandleChatCompletion)

	if embeddings != nil {
		mux.HandleFunc("/v1/embeddings", embeddings.HandleEmbedding)
	}

	mux.HandleFunc("/v1/models", models.HandleList)
	mux.HandleFunc("/v1/models/{model}", models.HandleRetrieve)

	// Fallback for unknown /v1/ routes: return an OpenAI JSON error envelope
	// instead of net/http's plain-text 404.
	mux.HandleFunc("/v1/", models.HandleNotFound)
}
