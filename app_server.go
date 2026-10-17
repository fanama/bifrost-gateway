package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"bridge-gateway/docs"
	"bridge-gateway/handlers"
	"bridge-gateway/web"
)

// routes builds the HTTP mux shared by the API and the HTMX interface.
func (a *gatewayApp) routes() (http.Handler, error) {
	mux := http.NewServeMux()
	docs.Register(mux)
	handlers.RegisterRoutes(mux, a.chat, a.models, a.embed)
	mux.HandleFunc("/chat/completions", a.chat.HandleChatCompletion)
	mux.HandleFunc("/embeddings", a.embed.HandleEmbedding)
	mux.HandleFunc("/health/liveness", a.chat.HandleHealth)
	mux.HandleFunc("/health/readiness", a.chat.HandleHealth)
	mux.HandleFunc("/health/test_connection", a.chat.HandleHealth)

	ui, err := web.NewServer(
		a.useCases.chat, a.useCases.config, a.useCases.model, a.useCases.project,
		a.useCases.key, a.useCases.provider, a.useCases.catalog, a.useCases.embedding,
		a.metrics, ollamaBaseURL(),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to init UI: %w", err)
	}
	ui.Register(mux)
	return mux, nil
}

// serve starts the HTTP server and blocks until SIGINT/SIGTERM or a listener
// error.
func (a *gatewayApp) serve(port int) error {
	mux, err := a.routes()
	if err != nil {
		return err
	}
	handler := loggingMiddleware(a.metrics.Middleware(handlers.CORS(mux)))
	server := &http.Server{
		Addr:         fmt.Sprintf(":%d", port),
		Handler:      handler,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 120 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	log.Printf("Models configured: %d", len(a.cfg.ModelList))
	for _, m := range a.cfg.ModelList {
		log.Printf("  - %s", m.ModelName)
	}
	log.Printf("Server listening on :%d", port)

	errCh := make(chan error, 1)
	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	select {
	case err := <-errCh:
		return fmt.Errorf("server error: %w", err)
	case <-quit:
		log.Println("Shutting down gracefully...")
		server.Close()
		log.Println("Server stopped")
		return nil
	}
}

func (a *gatewayApp) close() {
	if a.storeCache != nil && a.cfg.Cache.Stores.Enabled {
		a.storeCache.Stop()
	}
	if a.llmCache != nil && a.cfg.Cache.LLMResponse.Enabled {
		a.llmCache.Stop()
	}
	if a.db != nil {
		a.db.Close()
	}
}
