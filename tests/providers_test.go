package tests

import (
	"context"
	"errors"
	"testing"

	"bridge-gateway/application"
	"bridge-gateway/domain"
	"bridge-gateway/infrastructure"
)

func newProviderStore(t *testing.T, seeds []domain.Provider) *infrastructure.ProviderStore {
	t.Helper()
	s, err := infrastructure.NewProviderStore(newTestDB(t), seeds)
	if err != nil {
		t.Fatalf("new provider store: %v", err)
	}
	return s
}

func newCatalogStore(t *testing.T, seeds []domain.CatalogModel) *infrastructure.ModelCatalogStore {
	t.Helper()
	s, err := infrastructure.NewModelCatalogStore(newTestDB(t), seeds)
	if err != nil {
		t.Fatalf("new catalog store: %v", err)
	}
	return s
}

func TestProviderStoreSeedsOnlyOnce(t *testing.T) {
	db := newTestDB(t)
	seeds := []domain.Provider{{ID: "prov-ollama", Name: "ollama", BaseURL: "http://localhost:11434"}}

	s, err := infrastructure.NewProviderStore(db, seeds)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	list, _ := s.List(context.Background())
	if len(list) != 1 || list[0].Name != "ollama" {
		t.Fatalf("expected seeds, got %#v", list)
	}

	// La table est desormais peuplee : un second store sur la meme base ne doit
	// pas re-seeder.
	s2, err := infrastructure.NewProviderStore(db, []domain.Provider{{ID: "x", Name: "autre"}})
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	list2, _ := s2.List(context.Background())
	if len(list2) != 1 || list2[0].Name != "ollama" {
		t.Fatalf("expected persisted data, not re-seeded: %#v", list2)
	}
}

func TestProviderUseCaseCRUD(t *testing.T) {
	uc := application.NewProviderUseCase(newProviderStore(t, nil))
	ctx := context.Background()

	created, err := uc.Create(ctx, " mistral ", "https://api.mistral.ai/v1", "sk-mistral-secret-123456")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if created.Name != "mistral" || created.BaseURL != "https://api.mistral.ai/v1" || created.APIKey != "sk-mistral-secret-123456" || created.ID == "" {
		t.Fatalf("unexpected provider: %#v", created)
	}
	if created.MaskedKey() != "sk-m••••3456" {
		t.Errorf("unexpected masked key: %s", created.MaskedKey())
	}

	updated, err := uc.Update(ctx, created.ID, "mistral-new", "", "sk-new-key")
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.Name != "mistral-new" || updated.BaseURL != "" || updated.APIKey != "sk-new-key" {
		t.Fatalf("unexpected update: %#v", updated)
	}

	list, _ := uc.List(ctx)
	if len(list) != 1 {
		t.Fatalf("expected 1 provider, got %d", len(list))
	}

	if err := uc.Delete(ctx, created.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := uc.Get(ctx, created.ID); !errors.Is(err, domain.ErrProviderNotFound) {
		t.Fatalf("expected ErrProviderNotFound, got %v", err)
	}
}

func TestProviderUseCaseRequiresName(t *testing.T) {
	uc := application.NewProviderUseCase(newProviderStore(t, nil))
	_, err := uc.Create(context.Background(), "   ", "", "")
	if err == nil {
		t.Fatal("expected validation error")
	}
	var verr *domain.ValidationError
	if !errors.As(err, &verr) {
		t.Fatalf("expected *ValidationError, got %T", err)
	}
}

func TestModelCatalogUseCaseDedupesAndRejectsInvalid(t *testing.T) {
	uc := application.NewModelCatalogUseCase(newCatalogStore(t, nil))
	ctx := context.Background()

	_, err := uc.Create(ctx, " qwen3:8b ", "ollama")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	_, err = uc.Create(ctx, "qwen3:8b", "OLLAMA") // doublon (provider case-insensible)
	if err != nil {
		t.Fatalf("duplicate should be silently deduped: %v", err)
	}
	list, _ := uc.List(ctx)
	if len(list) != 1 {
		t.Fatalf("expected dedupe to 1 model, got %d", len(list))
	}

	_, err = uc.Create(ctx, "", "ollama")
	var verr *domain.ValidationError
	if !errors.As(err, &verr) {
		t.Fatalf("expected ValidationError for empty name, got %v", err)
	}
	_, err = uc.Create(ctx, "x", "   ")
	if !errors.As(err, &verr) {
		t.Fatalf("expected ValidationError for empty provider, got %v", err)
	}

	if err := uc.Delete(ctx, list[0].ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if err := uc.Delete(ctx, list[0].ID); !errors.Is(err, domain.ErrCatalogModelNotFound) {
		t.Fatalf("expected ErrCatalogModelNotFound on second delete, got %v", err)
	}
}

func TestModelUseCaseMergesCatalog(t *testing.T) {
	store := NewTestStore(t)
	catalog := newCatalogStore(t, []domain.CatalogModel{
		{ID: "model-1", Name: "qwen3:8b", Provider: "ollama"},
		{ID: "model-2", Name: "gemma4:e4b", Provider: "ollama"}, // doublon gateway
	})
	uc := application.NewModelUseCase([]domain.ModelInfo{
		{Name: "gemma4:e4b", Provider: "ollama", Source: "config.yaml"},
	}, store, catalog)
	ctx := context.Background()

	models, err := uc.List(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(models) != 2 {
		t.Fatalf("expected 2 unique models (gateway + catalog non-deduped), got %d: %#v", len(models), models)
	}
	sources := map[string]string{}
	for _, m := range models {
		sources[m.Name] = m.Source
	}
	if sources["gemma4:e4b"] != "config.yaml" {
		t.Errorf("catalog duplicate must not override gateway source: %#v", sources)
	}
	if sources["qwen3:8b"] != "catalogue" {
		t.Errorf("expected catalogue source: %#v", sources)
	}
}

func TestModelUseCaseListForProvider(t *testing.T) {
	store := NewTestStore(t)
	catalog := newCatalogStore(t, []domain.CatalogModel{
		{ID: "model-1", Name: "qwen3:8b", Provider: "ollama"},
		{ID: "model-2", Name: "mistral-small", Provider: "mistral"},
	})
	uc := application.NewModelUseCase([]domain.ModelInfo{
		{Name: "gpt-4o", Provider: "openai", Source: "config.yaml"},
	}, store, catalog)
	ctx := context.Background()

	// Empty provider returns no models
	emptyList, err := uc.ListForProvider(ctx, "")
	if err != nil {
		t.Fatalf("list for empty provider: %v", err)
	}
	if len(emptyList) != 0 {
		t.Fatalf("expected 0 models for empty provider, got %d", len(emptyList))
	}

	// Specific provider returns only its models
	ollamaModels, err := uc.ListForProvider(ctx, "ollama")
	if err != nil {
		t.Fatalf("list for ollama: %v", err)
	}
	if len(ollamaModels) != 1 || ollamaModels[0].Name != "qwen3:8b" {
		t.Fatalf("expected only qwen3:8b for ollama, got %#v", ollamaModels)
	}

	// Case-insensitivity
	mistralModels, err := uc.ListForProvider(ctx, "MISTRAL")
	if err != nil {
		t.Fatalf("list for MISTRAL: %v", err)
	}
	if len(mistralModels) != 1 || mistralModels[0].Name != "mistral-small" {
		t.Fatalf("expected only mistral-small for MISTRAL, got %#v", mistralModels)
	}
}
