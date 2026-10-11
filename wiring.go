package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"bridge-gateway/application"
	"bridge-gateway/domain"
	"bridge-gateway/infrastructure"
)

// Ce fichier regroupe les helpers de composition (seeds, caches, middleware)
// utilises par main(). main.go ne garde que le cablage : drapeaux, chargement
// de la configuration, ouverture de la base, assemblage et serveur HTTP.

// seedCatalogModels enregistre au catalogue les modeles locaux disponibles :
// le modele d'embedding embarque, puis les modeles ONNX decouverts (embedding
// et chat) sous models/onnx/. Chaque creation est idempotente, et un
// repertoire absent n'est pas une panne — c'est le cas documente du depot sans
// assets.
func seedCatalogModels(ctx context.Context, catalogUseCase *application.ModelCatalogUseCase) {
	if _, err := catalogUseCase.Create(ctx, infrastructure.LocalEmbeddingModel, infrastructure.LocalProviderName, domain.ModelKindEmbedding); err != nil {
		log.Printf("Failed to ensure local embedding model: %v", err)
	}

	// Les modeles ONNX locaux rejoignent le catalogue comme modeles
	// d'embedding : la page Modeles les montre (et permet de les retirer), le
	// testeur les propose, l'API les route vers le moteur ONNX.
	if ids, err := infrastructure.ListOnnxModels(infrastructure.OnnxModelsDir); err != nil && !errors.Is(err, os.ErrNotExist) {
		log.Printf("ONNX model discovery failed: %v", err)
	} else {
		for _, id := range ids {
			if _, err := catalogUseCase.Create(ctx, domain.OnnxModelPrefix+id, domain.ProviderOnnx, domain.ModelKindEmbedding); err != nil {
				log.Printf("Failed to seed ONNX model %q: %v", id, err)
			}
		}
	}

	// Les modeles de chat ONNX rejoignent le catalogue comme modeles de chat :
	// la page Modeles les propose, et le routeur de chat les execute localement.
	if ids, err := infrastructure.ListOnnxChatModels(infrastructure.OnnxChatModelsDir); err != nil && !errors.Is(err, os.ErrNotExist) {
		log.Printf("ONNX chat model discovery failed: %v", err)
	} else {
		for _, id := range ids {
			if _, err := catalogUseCase.Create(ctx, domain.OnnxModelPrefix+id, domain.ProviderOnnx, domain.ModelKindChat); err != nil {
				log.Printf("Failed to seed ONNX chat model %q: %v", id, err)
			}
		}
	}
}

// buildLLMProvider assemble le provider de chat avec ses decorateurs, du plus
// externe au plus interne : cache -> routeur -> (chat ONNX local ou Bifrost).
//
// Le cache doit entourer le routeur pour que la cle soit calculee sur la
// configuration active et qu'un hit evite a la fois l'appel au classifieur
// (~1 s) et l'echelle de bascule. Le routeur entoure Bifrost, et le classifieur
// recoit Bifrost directement : passer par le provider decore ferait boucler le
// classifieur sur lui-meme.
func buildLLMProvider(
	cfg *infrastructure.GatewayConfig,
	onnxChatProvider *infrastructure.OnnxChatProvider,
	bifrostProvider *infrastructure.BifrostLLMProvider,
	catalogStore domain.ModelCatalogRepository,
	configStore domain.ChatConfigRepository,
	providerStore domain.ProviderRepository,
	llmCache domain.Cache,
) domain.LLMProvider {
	var llm domain.LLMProvider = infrastructure.NewOnnxChatRouter(onnxChatProvider, bifrostProvider).WithCatalog(catalogStore)

	if cfg.Routing.Enabled {
		// Quand le routeur escalade vers un autre tier, il change de provider :
		// les reessais internes de Bifrost feraient doublonner le travail de
		// l'echelle de bascule.
		bifrostProvider.SetMaxRetries(cfg.Routing.MaxRetries)

		classifier := infrastructure.NewLocalClassifier(bifrostProvider, cfg.Routing.Classifier)
		llm = infrastructure.NewRoutingLLMProvider(
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
		llm = infrastructure.NewCachedLLMProvider(llm, llmCache, cfg.Cache.LLMResponse.CacheTTL(5*time.Minute))
	}
	return llm
}

// migrateOrphanConfigs cree le projet "General" au demarrage et rattache
// les configurations existantes qui n'appartiennent encore a aucun projet.
// Elle ne depend que des ports du domaine, pas des adaptateurs SQLite.
func migrateOrphanConfigs(configStore domain.ChatConfigRepository, projectStore domain.ProjectRepository) {
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
	return ensureNamedProvider(ctx, uc, infrastructure.LocalProviderName)
}

// ensureNamedProvider cree le provider s'il est absent (meme garantie que le
// seed du premier demarrage, appliquee a un nom supplementaire).
func ensureNamedProvider(ctx context.Context, uc *application.ProviderUseCase, name string) error {
	list, err := uc.List(ctx)
	if err != nil {
		return err
	}
	for i := range list {
		if strings.EqualFold(list[i].Name, name) {
			return nil
		}
	}
	_, err = uc.Create(ctx, name, "", "")
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
