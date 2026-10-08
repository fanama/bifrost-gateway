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
	dbPath := flag.String("db", "data/bridge.db", "path to the SQLite database file")
	legacyDir := flag.String("import-json", "data", "directory holding legacy JSON stores to migrate on first start (empty to skip)")
	flag.Parse()

	log.SetFlags(log.LstdFlags | log.Lshortfile)
	log.Printf("Starting Bridge Gateway (BifrostAI + HTMX UI) on port %d", *port)

	cfg, err := infrastructure.LoadConfig(*configPath)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	ctx := context.Background()

	db, err := infrastructure.OpenDB(*dbPath)
	if err != nil {
		log.Fatalf("Failed to open database %s: %v", *dbPath, err)
	}
	defer db.Close()

	if err := infrastructure.Migrate(ctx, db); err != nil {
		log.Fatalf("Failed to migrate schema: %v", err)
	}

	if *legacyDir != "" {
		counts, err := infrastructure.ImportLegacyStoreData(ctx, db, *legacyDir)
		if err != nil {
			log.Fatalf("Failed to import legacy JSON stores: %v", err)
		}
		for name, n := range counts {
			log.Printf("Imported %d record(s) from %s.json into SQLite", n, name)
		}
	}

	configStore := infrastructure.NewChatConfigStore(db)
	projectStore := infrastructure.NewProjectStore(db)
	keyStore := infrastructure.NewAPIKeyStore(db)
	providerStore, err := infrastructure.NewProviderStore(db, defaultProviders())
	if err != nil {
		log.Fatalf("Failed to init provider store: %v", err)
	}
	catalogStore, err := infrastructure.NewModelCatalogStore(db, catalogModels(cfg))
	if err != nil {
		log.Fatalf("Failed to init model catalog store: %v", err)
	}

	// Cache local en memoire (remplace Redis).
	//
	//   - cache des lectures de repositories, TTL court (15s) pour que les
	//     modifications faites depuis l'UI soient visibles presque immediatement ;
	//   - cache des reponses LLM, TTL long (5 min) car un hit evite un appel
	//     reseau au provider.
	//
	// Chaque decorateur est omis si sa section est desactivee dans config.yaml,
	// ce qui laisse les calls traverser directement SQLite.
	var uiConfigStore domain.ChatConfigRepository = configStore
	var uiProjectStore domain.ProjectRepository = projectStore
	var uiKeyStore domain.APIKeyRepository = keyStore

	storeCache, llmCache := configureCaches(cfg)

	if cfg.Cache.Stores.Enabled {
		defer storeCache.Stop()
		uiConfigStore = infrastructure.NewCachedConfigStore(configStore, storeCache, cfg.Cache.Stores.CacheTTL(15*time.Second))
		uiProjectStore = infrastructure.NewCachedProjectStore(projectStore, storeCache, cfg.Cache.Stores.CacheTTL(15*time.Second))
		uiKeyStore = infrastructure.NewCachedAPIKeyStore(keyStore, storeCache, cfg.Cache.Stores.CacheTTL(15*time.Second))
	}
	if cfg.Cache.LLMResponse.Enabled {
		defer llmCache.Stop()
	}

	migrateOrphanConfigs(configStore, projectStore)

	bifrostProvider := infrastructure.NewBifrostLLMProvider(providerStore)

	// Le chat doit voir l'etat le plus frais possible des configs, donc recoit
	// le store non decoré ; seul l'appel provider est mis en cache.
	//
	// Ordre des decorateurs, du plus externe au plus interne :
	//
	//   cache -> routeur -> Bifrost
	//
	// Le cache doit entourer le routeur pour que la cle soit calculee sur la
	// configuration active et qu'un hit evite a la fois l'appel au classifieur
	// (~1 s) et l'echelle de bascule. Il memorise le modele qui a reellement
	// repondu, donc un hit reste fidele.
	//
	// Le routeur doit entourer Bifrost et le classifieur doit appeler Bifrost
	// directement : passer par le provider decoré ferait boucler le classifieur
	// sur lui-meme.
	var llmProvider domain.LLMProvider = bifrostProvider

	if cfg.Routing.Enabled {
		// Quand le routeur escalade vers un autre tier, il change de provider :
		// les reessais internes de Bifrost feraient doublonner le travail de
		// l'echelle de bascule.
		bifrostProvider.SetMaxRetries(cfg.Routing.MaxRetries)

		classifier := infrastructure.NewLocalClassifier(bifrostProvider, cfg.Routing.Classifier)
		llmProvider = infrastructure.NewRoutingLLMProvider(
			bifrostProvider,
			classifier,
			configStore,
			providerStore,
			infrastructure.RoutingOptions{
				MinConfidence: cfg.Routing.NormalizedMinConfidence(0.6),
				Cooldown:      cfg.Routing.CooldownTTL(60 * time.Second),
				MaxAttempts:   cfg.Routing.MaxAttempts,
			},
		)
		log.Printf("Routing enabled: tiers=%s, min_confidence=%.2f, cooldown=%s",
			strings.Join(domain.TierOptions(), ">"),
			cfg.Routing.NormalizedMinConfidence(0.6),
			cfg.Routing.CooldownTTL(60*time.Second))
	}

	if cfg.Cache.LLMResponse.Enabled {
		llmProvider = infrastructure.NewCachedLLMProvider(llmProvider, llmCache, cfg.Cache.LLMResponse.CacheTTL(5*time.Minute))
	}

	enrichmentService := domain.NewEnrichmentService()
	projectUseCase := application.NewProjectUseCase(uiProjectStore)
	keyUseCase := application.NewAPIKeyUseCase(uiKeyStore, uiProjectStore)
	authUseCase := application.NewAuthUseCase(os.Getenv("BIFROST_MASTER_KEY"), uiKeyStore, uiProjectStore)
	chatHandler := handlers.NewChatHandler(
		enrichmentService,
		authUseCase,
		application.NewChatUseCase(configStore, enrichmentService, llmProvider),
		application.NewConfigUseCase(configStore),
	)

	configUseCase := application.NewConfigUseCase(uiConfigStore)
	chatUseCase := application.NewChatUseCase(configStore, enrichmentService, llmProvider)
	modelUseCase := application.NewModelUseCase(gatewayModels(cfg), uiConfigStore, catalogStore)
	providerUseCase := application.NewProviderUseCase(providerStore)
	catalogUseCase := application.NewModelCatalogUseCase(catalogStore)
	modelsHandler := handlers.NewModelsHandler(authUseCase, modelUseCase)

	// Le runtime d'embedding local est embarque dans le binaire (Go pur, sans
	// CGO ni serveur compagnon). Les seeds ne courent que sur une base vide :
	// on garantit donc ici la presence du provider "local" et du modele
	// embarque, quel que soit l'etat de la base, pour que le formulaire de
	// configuration (select provider + select modele) les proposent. Les deux
	// operations sont idempotentes.
	if err := ensureLocalProvider(ctx, providerUseCase); err != nil {
		log.Printf("Failed to ensure local provider: %v", err)
	}
	if _, err := catalogUseCase.Create(ctx, infrastructure.LocalEmbeddingModel, infrastructure.LocalProviderName); err != nil {
		log.Printf("Failed to ensure local embedding model: %v", err)
	}

	// Routage des embeddings sur trois moteurs : le runtime Go embarque, le
	// runtime ONNX in-process (artefacts sous models/onnx/, chargement
	// paresseux au premier appel), et Bifrost en repli pour tout le reste.
	// Le choix se fait par le modele demande (liste curatee) ou le provider
	// de la configuration ; un echec local est remonte tel quel, sans bascule
	// silencieuse vers un provider distant.
	localEmbedder := infrastructure.NewLocalEmbedder(infrastructure.LocalEmbedderOptions{})
	onnxEmbedder := infrastructure.NewOnnxEmbedder(infrastructure.DefaultOnnxEmbedderOptions())
	embeddingProvider := infrastructure.NewEmbeddingRouter(localEmbedder, onnxEmbedder, bifrostProvider)
	embeddingUseCase := application.NewEmbeddingUseCase(configStore, embeddingProvider)
	embeddingHandler := handlers.NewEmbeddingHandler(authUseCase, embeddingUseCase, application.NewConfigUseCase(configStore))

	mux := http.NewServeMux()
	handlers.RegisterRoutes(mux, chatHandler, modelsHandler, embeddingHandler)
	mux.HandleFunc("/chat/completions", chatHandler.HandleChatCompletion)
	mux.HandleFunc("/embeddings", embeddingHandler.HandleEmbedding)
	mux.HandleFunc("/health/liveness", chatHandler.HandleHealth)
	mux.HandleFunc("/health/readiness", chatHandler.HandleHealth)
	mux.HandleFunc("/health/test_connection", chatHandler.HandleHealth)

	ui := web.NewServer(chatUseCase, configUseCase, modelUseCase, projectUseCase, keyUseCase, providerUseCase, catalogUseCase, embeddingUseCase, ollamaBaseURL())
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
func migrateOrphanConfigs(configStore *infrastructure.ChatConfigStore, projectStore *infrastructure.ProjectStore) {
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

// ensureLocalProvider cree le provider "local" s'il est absent. Le seed des
// providers ne court que sur une base vide : une base existante ne le verrait
// jamais apparaitre, et le select provider du formulaire de configuration ne
// proposerait pas le runtime embarque. Cette garantie couvre les deux cas.
func ensureLocalProvider(ctx context.Context, uc *application.ProviderUseCase) error {
	list, err := uc.List(ctx)
	if err != nil {
		return err
	}
	for i := range list {
		if strings.EqualFold(list[i].Name, infrastructure.LocalProviderName) {
			return nil
		}
	}
	_, err = uc.Create(ctx, infrastructure.LocalProviderName, "", "")
	return err
}

// defaultProviders sont les fournisseurs prevus par defaut (seed du premier
// demarrage, lorsque data/providers.json n'existe pas encore).
func defaultProviders() []domain.Provider {
	now := time.Now().UTC()
	return []domain.Provider{
		{ID: "prov-ollama", Name: "ollama", BaseURL: ollamaBaseURL(), CreatedAt: now, UpdatedAt: now},
		{ID: "prov-openai", Name: "openai", BaseURL: "https://api.openai.com", CreatedAt: now, UpdatedAt: now},
		{ID: "prov-mistral", Name: "mistral", BaseURL: "https://api.mistral.ai", CreatedAt: now, UpdatedAt: now},
		{ID: "prov-google", Name: "google", BaseURL: "", CreatedAt: now, UpdatedAt: now},
		{ID: "prov-anthropic", Name: "anthropic", BaseURL: "https://api.anthropic.com", CreatedAt: now, UpdatedAt: now},
		{ID: "prov-groq", Name: "groq", BaseURL: "https://api.groq.com/openai", CreatedAt: now, UpdatedAt: now},
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

// configureCaches instancie les deux caches en memoire. Ils sont toujours
// crees (et arretes a la fermeture) pour que le balayage des entrees expirees
// demarre, mais un decorateur n'est installe que si sa section est activee.
func configureCaches(cfg *infrastructure.GatewayConfig) (stores, llm *infrastructure.MemoryCache) {
	stores = infrastructure.NewMemoryCache(infrastructure.MemoryCacheOptions{
		MaxEntries:    cfg.Cache.Stores.MaxEntries,
		SweepInterval: time.Minute,
	})
	llm = infrastructure.NewMemoryCache(infrastructure.MemoryCacheOptions{
		MaxEntries:    cfg.Cache.LLMResponse.MaxEntries,
		SweepInterval: time.Minute,
	})
	return stores, llm
}

func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		log.Printf("-> %s %s", r.Method, r.URL.Path)
		next.ServeHTTP(w, r)
		log.Printf("<- %s %s (%v)", r.Method, r.URL.Path, time.Since(start))
	})
}
