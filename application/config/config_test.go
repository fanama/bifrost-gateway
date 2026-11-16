package config

import (
	"context"
	"reflect"
	"testing"

	"bridge-gateway/domain"
)

type memConfigRepo struct {
	items map[string]*domain.ChatConfig
}

func newMemConfigRepo() *memConfigRepo {
	return &memConfigRepo{items: make(map[string]*domain.ChatConfig)}
}

func (m *memConfigRepo) List(context.Context) ([]domain.ChatConfig, error) {
	out := make([]domain.ChatConfig, 0, len(m.items))
	for _, item := range m.items {
		out = append(out, *item)
	}
	return out, nil
}

func (m *memConfigRepo) ListByProject(_ context.Context, projectID string) ([]domain.ChatConfig, error) {
	out := make([]domain.ChatConfig, 0)
	for _, item := range m.items {
		if item.ProjectID == projectID {
			out = append(out, *item)
		}
	}
	return out, nil
}

func (m *memConfigRepo) GetActiveByProject(_ context.Context, projectID string) (*domain.ChatConfig, error) {
	for _, item := range m.items {
		if item.ProjectID == projectID && item.Active {
			return item, nil
		}
	}
	return nil, domain.ErrNoActiveConfig
}

func (m *memConfigRepo) Get(_ context.Context, id string) (*domain.ChatConfig, error) {
	if item, ok := m.items[id]; ok {
		cp := *item
		return &cp, nil
	}
	return nil, domain.ErrConfigNotFound
}

func (m *memConfigRepo) Create(_ context.Context, cfg *domain.ChatConfig) error {
	cp := *cfg
	m.items[cfg.ID] = &cp
	return nil
}

func (m *memConfigRepo) Update(_ context.Context, cfg *domain.ChatConfig) error {
	if _, ok := m.items[cfg.ID]; !ok {
		return domain.ErrConfigNotFound
	}
	cp := *cfg
	m.items[cfg.ID] = &cp
	return nil
}

func (m *memConfigRepo) Delete(_ context.Context, id string) error {
	delete(m.items, id)
	return nil
}

func (m *memConfigRepo) SetActive(_ context.Context, projectID, id string) error {
	for k, v := range m.items {
		if v.ProjectID == projectID {
			v.Active = (k == id)
		}
	}
	return nil
}

func TestUpdatePersistsFallbackModels(t *testing.T) {
	repo := newMemConfigRepo()
	uc := NewConfigUseCase(repo)
	ctx := context.Background()

	created, err := uc.Create(ctx, &domain.ChatConfig{
		ProjectID: "p1",
		Name:      "Primary Only",
		Provider:  "mistral",
		Model:     "mistral-large",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if len(created.FallbackModels) != 0 {
		t.Fatalf("expected no initial fallbacks, got %#v", created.FallbackModels)
	}

	// Update to add fallback models
	patch := &domain.ChatConfig{
		Name:     "With Fallback",
		Provider: "mistral",
		Model:    "mistral-large",
		FallbackModels: []domain.ModelChoice{
			{Provider: "mistral", Model: "mistral-small"},
			{Provider: "openai", Model: "gpt-4o"},
		},
	}
	updated, err := uc.Update(ctx, created.ID, patch)
	if err != nil {
		t.Fatalf("update: %v", err)
	}

	wantFallbacks := []domain.ModelChoice{
		{Provider: "mistral", Model: "mistral-small"},
		{Provider: "openai", Model: "gpt-4o"},
	}
	if !reflect.DeepEqual(updated.FallbackModels, wantFallbacks) {
		t.Errorf("updated.FallbackModels = %#v, want %#v", updated.FallbackModels, wantFallbacks)
	}

	// Fetch from repo to ensure persistence
	stored, err := uc.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if !reflect.DeepEqual(stored.FallbackModels, wantFallbacks) {
		t.Errorf("stored.FallbackModels = %#v, want %#v", stored.FallbackModels, wantFallbacks)
	}

	// Update again to clear fallback models
	patchClear := &domain.ChatConfig{
		Name:     "Cleared Fallback",
		Provider: "mistral",
		Model:    "mistral-large",
	}
	cleared, err := uc.Update(ctx, created.ID, patchClear)
	if err != nil {
		t.Fatalf("update clear: %v", err)
	}
	if cleared.FallbackModels != nil {
		t.Errorf("expected cleared fallbacks to be nil, got %#v", cleared.FallbackModels)
	}
}
