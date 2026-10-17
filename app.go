package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"time"

	"bridge-gateway/application"
	"bridge-gateway/domain"
	"bridge-gateway/handlers"
	"bridge-gateway/infrastructure"
)

// gatewayStores holds the raw persistence adapters plus the repositories the
// UI reads through (wrapped by caches when enabled).
type gatewayStores struct {
	configs   *infrastructure.ChatConfigStore
	projects  *infrastructure.ProjectStore
	keys      *infrastructure.APIKeyStore
	providers *infrastructure.ProviderStore
	catalog   *infrastructure.ModelCatalogStore

	uiConfigs  domain.ChatConfigRepository
	uiProjects domain.ProjectRepository
	uiKeys     domain.APIKeyRepository
}

// gatewayUseCases groups the application services wired at startup.
type gatewayUseCases struct {
	chat      *application.ChatUseCase
	config    *application.ConfigUseCase
	model     *application.ModelUseCase
	project   *application.ProjectUseCase
	key       *application.APIKeyUseCase
	auth      *application.AuthUseCase
	provider  *application.ProviderUseCase
	catalog   *application.ModelCatalogUseCase
	embedding *application.EmbeddingUseCase
}

// gatewayApp owns every long-lived dependency of the running server.
type gatewayApp struct {
	cfg     *infrastructure.GatewayConfig
	db      *sql.DB
	metrics *infrastructure.MetricsCollector

	storeCache *infrastructure.MemoryCache
	llmCache   *infrastructure.MemoryCache

	stores   gatewayStores
	useCases gatewayUseCases
	bifrost  *infrastructure.BifrostLLMProvider
	chat     *handlers.ChatHandler
	models   *handlers.ModelsHandler
	embed    *handlers.EmbeddingHandler
}

// newGatewayApp opens storage, builds the domain services, and returns a fully
// wired application. On any failure the partially built app is closed.
func newGatewayApp(ctx context.Context, opts appOptions) (*gatewayApp, error) {
	cfg, err := infrastructure.LoadConfig(opts.configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}

	// First run: ONNX artifacts are downloaded when missing. A network failure
	// is not blocking; the server starts and explicit errors cite the missing
	// file until the next attempt.
	if err := infrastructure.EnsureOnnxAssets(ctx); err != nil {
		log.Printf("ONNX assets incomplete (will retry on next start): %v", err)
	}

	db, err := openAndMigrate(ctx, opts)
	if err != nil {
		return nil, err
	}

	app := &gatewayApp{cfg: cfg, db: db, metrics: infrastructure.NewMetricsCollector()}
	if err := app.buildStores(); err != nil {
		app.close()
		return nil, err
	}
	app.storeCache, app.llmCache = configureCaches(cfg)
	app.applyCaches()
	migrateOrphanConfigs(app.stores.configs, app.stores.projects)

	if err := app.buildChatServices(); err != nil {
		app.close()
		return nil, err
	}
	app.seedProviders(ctx)
	app.buildEmbeddingService()
	return app, nil
}

// openAndMigrate opens the SQLite database, applies the schema, and imports any
// legacy JSON stores on first start.
func openAndMigrate(ctx context.Context, opts appOptions) (*sql.DB, error) {
	db, err := infrastructure.OpenDB(opts.dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database %s: %w", opts.dbPath, err)
	}
	if err := infrastructure.Migrate(ctx, db); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to migrate schema: %w", err)
	}
	if err := importLegacyStores(ctx, db, opts.legacyDir); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

func importLegacyStores(ctx context.Context, db *sql.DB, legacyDir string) error {
	if legacyDir == "" {
		return nil
	}
	counts, err := infrastructure.ImportLegacyStoreData(ctx, db, legacyDir)
	if err != nil {
		return fmt.Errorf("failed to import legacy JSON stores: %w", err)
	}
	for name, n := range counts {
		log.Printf("Imported %d record(s) from %s.json into SQLite", n, name)
	}
	return nil
}

func (a *gatewayApp) buildStores() error {
	configs := infrastructure.NewChatConfigStore(a.db)
	projects := infrastructure.NewProjectStore(a.db)
	keys := infrastructure.NewAPIKeyStore(a.db)
	a.stores = gatewayStores{
		configs:    configs,
		projects:   projects,
		keys:       keys,
		uiConfigs:  configs,
		uiProjects: projects,
		uiKeys:     keys,
	}

	providers, err := infrastructure.NewProviderStore(a.db, defaultProviders())
	if err != nil {
		return fmt.Errorf("failed to init provider store: %w", err)
	}
	a.stores.providers = providers

	catalog, err := infrastructure.NewModelCatalogStore(a.db, catalogModels(a.cfg))
	if err != nil {
		return fmt.Errorf("failed to init model catalog store: %w", err)
	}
	a.stores.catalog = catalog
	return nil
}

// applyCaches replaces the UI-facing repositories with cache-backed wrappers
// when store caching is enabled.
func (a *gatewayApp) applyCaches() {
	a.stores.uiConfigs = a.stores.configs
	a.stores.uiProjects = a.stores.projects
	a.stores.uiKeys = a.stores.keys
	if !a.cfg.Cache.Stores.Enabled {
		return
	}
	ttl := a.cfg.Cache.Stores.CacheTTL(15 * time.Second)
	a.stores.uiConfigs = infrastructure.NewCachedConfigStore(a.stores.configs, a.storeCache, ttl)
	a.stores.uiProjects = infrastructure.NewCachedProjectStore(a.stores.projects, a.storeCache, ttl)
	a.stores.uiKeys = infrastructure.NewCachedAPIKeyStore(a.stores.keys, a.storeCache, ttl)
}

// buildChatServices wires the LLM router, chat/auth services, and the HTTP
// handlers that serve chat completions and models.
func (a *gatewayApp) buildChatServices() error {
	a.bifrost = infrastructure.NewBifrostLLMProvider(a.stores.providers)
	onnxChat := infrastructure.NewOnnxChatProvider(infrastructure.DefaultOnnxChatOptions())
	llm := buildLLMProvider(
		a.cfg, onnxChat, a.bifrost, a.stores.catalog,
		a.stores.configs, a.stores.providers, a.llmCache, a.metrics,
	)

	enrichment := domain.NewEnrichmentService()
	u := &a.useCases
	u.project = application.NewProjectUseCase(a.stores.uiProjects)
	u.key = application.NewAPIKeyUseCase(a.stores.uiKeys, a.stores.uiProjects)
	u.auth = application.NewAuthUseCase(os.Getenv("BIFROST_MASTER_KEY"), a.stores.uiKeys, a.stores.uiProjects)
	u.chat = application.NewChatUseCase(a.stores.configs, enrichment, llm)
	u.config = application.NewConfigUseCase(a.stores.uiConfigs)
	u.model = application.NewModelUseCase(gatewayModels(a.cfg), a.stores.uiConfigs, a.stores.catalog)
	u.provider = application.NewProviderUseCase(a.stores.providers)
	u.catalog = application.NewModelCatalogUseCase(a.stores.catalog)

	a.chat = handlers.NewChatHandler(enrichment, u.auth, u.chat, application.NewConfigUseCase(a.stores.configs))
	a.models = handlers.NewModelsHandler(u.auth, u.model)
	return nil
}

// seedProviders makes sure the built-in local providers exist and loads the
// starter model catalog. Failures are logged, never fatal.
func (a *gatewayApp) seedProviders(ctx context.Context) {
	if err := ensureLocalProvider(ctx, a.useCases.provider); err != nil {
		log.Printf("Failed to ensure local provider: %v", err)
	}
	if err := ensureNamedProvider(ctx, a.useCases.provider, domain.ProviderOnnx); err != nil {
		log.Printf("Failed to ensure onnx provider: %v", err)
	}
	seedCatalogModels(ctx, a.useCases.catalog)
}

func (a *gatewayApp) buildEmbeddingService() {
	local := infrastructure.NewLocalEmbedder(infrastructure.LocalEmbedderOptions{})
	onnx := infrastructure.NewOnnxEmbedder(infrastructure.DefaultOnnxEmbedderOptions())
	router := infrastructure.NewEmbeddingRouter(local, onnx, a.bifrost).WithCatalog(a.stores.catalog)
	a.useCases.embedding = application.NewEmbeddingUseCase(a.stores.configs, router)
	a.embed = handlers.NewEmbeddingHandler(
		a.useCases.auth, a.useCases.embedding,
		application.NewConfigUseCase(a.stores.configs),
	)
}
