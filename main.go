package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"bridge-gateway/application"
	"bridge-gateway/domain"
	"bridge-gateway/handlers"
	"bridge-gateway/infrastructure"
	"bridge-gateway/web"
)

const generalProjectID = "project-general"

func main() {
	configPath := flag.String("config", "config.yaml", "path to config file")
	port := flag.Int("port", 8080, "server port")
	storePath := flag.String("store", "data/configs.json", "path to configurations store")
	flag.Parse()

	log.SetFlags(log.LstdFlags | log.Lshortfile)
	log.Printf("Starting Bridge Gateway (BifrostAI + HTMX UI) on port %d", *port)

	cfg, err := infrastructure.LoadConfig(*configPath)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	storeDir := filepath.Dir(*storePath)

	configStore, err := infrastructure.NewFileConfigStore(*storePath)
	if err != nil {
		log.Fatalf("Failed to init config store: %v", err)
	}
	projectStore, err := infrastructure.NewFileProjectStore(filepath.Join(storeDir, "projects.json"))
	if err != nil {
		log.Fatalf("Failed to init project store: %v", err)
	}
	keyStore, err := infrastructure.NewFileAPIKeyStore(filepath.Join(storeDir, "apikeys.json"))
	if err != nil {
		log.Fatalf("Failed to init api key store: %v", err)
	}
	providerStore, err := infrastructure.NewFileProviderStore(filepath.Join(storeDir, "providers.json"), defaultProviders())
	if err != nil {
		log.Fatalf("Failed to init provider store: %v", err)
	}
	catalogStore, err := infrastructure.NewFileModelCatalogStore(filepath.Join(storeDir, "models.json"), catalogModels(cfg))
	if err != nil {
		log.Fatalf("Failed to init model catalog store: %v", err)
	}

	migrateOrphanConfigs(configStore, projectStore)

	llmProvider := infrastructure.NewBifrostLLMProvider(providerStore)

	enrichmentService := domain.NewEnrichmentService()
	projectUseCase := application.NewProjectUseCase(projectStore)
	keyUseCase := application.NewAPIKeyUseCase(keyStore, projectStore)
	authUseCase := application.NewAuthUseCase(os.Getenv("BIFROST_MASTER_KEY"), keyStore, projectStore)
	chatHandler := handlers.NewChatHandler(
		enrichmentService,
		authUseCase,
		application.NewChatUseCase(configStore, enrichmentService, llmProvider),
		application.NewConfigUseCase(configStore),
	)

	configUseCase := application.NewConfigUseCase(configStore)
	chatUseCase := application.NewChatUseCase(configStore, enrichmentService, llmProvider)
	modelUseCase := application.NewModelUseCase(gatewayModels(cfg), configStore, catalogStore)
	providerUseCase := application.NewProviderUseCase(providerStore)
	catalogUseCase := application.NewModelCatalogUseCase(catalogStore)

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/chat/completions", chatHandler.HandleChatCompletion)
	mux.HandleFunc("/chat/completions", chatHandler.HandleChatCompletion)
	mux.HandleFunc("/health/liveness", chatHandler.HandleHealth)
	mux.HandleFunc("/health/readiness", chatHandler.HandleHealth)
	mux.HandleFunc("/health/test_connection", chatHandler.HandleHealth)

	ui := web.NewServer(chatUseCase, configUseCase, modelUseCase, projectUseCase, keyUseCase, providerUseCase, catalogUseCase, ollamaBaseURL())
	ui.Register(mux)

	middleware := loggingMiddleware(mux)

	server := &http.Server{
		Addr:         fmt.Sprintf(":%d", *port),
		Handler:      middleware,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 120 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	log.Printf("Models configured: %d", len(cfg.ModelList))
	for _, m := range cfg.ModelList {
		log.Printf("  - %s", m.ModelName)
	}

	log.Printf("Server listening on :%d", *port)

	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server error: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("Shutting down gracefully...")
	server.Close()
	log.Println("Server stopped")
}

// migrateOrphanConfigs cree le projet "General" au demarrage et rattache
// les configurations existantes qui n'appartiennent encore a aucun projet.
func migrateOrphanConfigs(configStore *infrastructure.FileConfigStore, projectStore *infrastructure.FileProjectStore) {
	ctx := context.Background()
	projectsUC := application.NewProjectUseCase(projectStore)

	if _, err := projectsUC.Get(ctx, generalProjectID); errors.Is(err, domain.ErrProjectNotFound) {
		if _, err := projectsUC.CreateWithID(ctx, generalProjectID, "General", "Projet par defaut pour les configurations existantes"); err != nil {
			log.Printf("Failed to create General project: %v", err)
		} else {
			log.Printf("Created default project: General (%s)", generalProjectID)
		}
	}

	configs, err := configStore.List(ctx)
	if err != nil {
		log.Printf("Failed to list configs during migration: %v", err)
		return
	}
	migrated := 0
	for i := range configs {
		if configs[i].ProjectID != "" {
			continue
		}
		configs[i].ProjectID = generalProjectID
		if err := configStore.Update(ctx, &configs[i]); err != nil {
			log.Printf("Failed to migrate config %s: %v", configs[i].ID, err)
			continue
		}
		migrated++
	}
	if migrated > 0 {
		log.Printf("Migrated %d config(s) to %s", migrated, generalProjectID)
	}
}

func ollamaBaseURL() string {
	base := strings.TrimSpace(os.Getenv("OLLAMA_HOST"))
	if base == "" {
		return "http://localhost:11434"
	}
	return strings.TrimRight(base, "/")
}

// defaultProviders sont les fournisseurs prevus par defaut (seed du premier
// demarrage, lorsque data/providers.json n'existe pas encore).
func defaultProviders() []domain.Provider {
	now := time.Now().UTC()
	return []domain.Provider{
		{ID: "prov-ollama", Name: "ollama", BaseURL: ollamaBaseURL(), CreatedAt: now, UpdatedAt: now},
		{ID: "prov-openai", Name: "openai", BaseURL: "https://api.openai.com/v1", CreatedAt: now, UpdatedAt: now},
		{ID: "prov-mistral", Name: "mistral", BaseURL: "https://api.mistral.ai/v1", CreatedAt: now, UpdatedAt: now},
		{ID: "prov-google", Name: "google", BaseURL: "", CreatedAt: now, UpdatedAt: now},
		{ID: "prov-anthropic", Name: "anthropic", BaseURL: "https://api.anthropic.com/v1", CreatedAt: now, UpdatedAt: now},
		{ID: "prov-groq", Name: "groq", BaseURL: "https://api.groq.com/openai/v1", CreatedAt: now, UpdatedAt: now},
		{ID: "prov-vertex", Name: "vertex", BaseURL: "", CreatedAt: now, UpdatedAt: now},
		{ID: "prov-azure", Name: "azure", BaseURL: "", CreatedAt: now, UpdatedAt: now},
	}
}

// catalogModels amorce le catalogue de modeles gerable depuis l'UI avec ceux
// de config.yaml (seed du premier demarrage uniquement).
func catalogModels(cfg *infrastructure.GatewayConfig) []domain.CatalogModel {
	now := time.Now().UTC()
	out := make([]domain.CatalogModel, 0, len(cfg.ModelList))
	for _, m := range cfg.ModelList {
		provider, _ := m.BifrostParams["provider"].(string)
		out = append(out, domain.CatalogModel{
			ID:        "model-" + domain.NewID(),
			Name:      m.ModelName,
			Provider:  strings.ToLower(strings.TrimSpace(provider)),
			CreatedAt: now,
		})
	}
	return out
}

func gatewayModels(cfg *infrastructure.GatewayConfig) []domain.ModelInfo {
	models := make([]domain.ModelInfo, 0, len(cfg.ModelList))
	for _, m := range cfg.ModelList {
		provider, _ := m.BifrostParams["provider"].(string)
		models = append(models, domain.ModelInfo{
			Name:     m.ModelName,
			Provider: strings.ToLower(strings.TrimSpace(provider)),
			Source:   "config.yaml",
		})
	}
	return models
}

func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		log.Printf("-> %s %s", r.Method, r.URL.Path)
		next.ServeHTTP(w, r)
		log.Printf("<- %s %s (%v)", r.Method, r.URL.Path, time.Since(start))
	})
}
