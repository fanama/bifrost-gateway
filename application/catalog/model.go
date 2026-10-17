package catalog

import (
	"context"
	"strings"

	"bridge-gateway/domain"
)

type ModelUseCase struct {
	gatewayModels []domain.ModelInfo
	configs       domain.ChatConfigRepository
	catalog       domain.ModelCatalogRepository
}

func NewModelUseCase(
	gatewayModels []domain.ModelInfo,
	configs domain.ChatConfigRepository,
	catalog domain.ModelCatalogRepository,
) *ModelUseCase {
	return &ModelUseCase{gatewayModels: gatewayModels, configs: configs, catalog: catalog}
}

// List fusionne les modeles du catalogue UI, ceux de config.yaml et ceux
// utilises dans les configurations enregistrees (dedupe par provider:name).
func (u *ModelUseCase) List(ctx context.Context) ([]domain.ModelInfo, error) {
	models := append([]domain.ModelInfo{}, u.gatewayModels...)
	seen := make(map[string]bool, len(models)+8)
	for _, m := range u.gatewayModels {
		seen[modelKey(m.Provider, m.Name)] = true
	}

	catalog, err := u.catalog.List(ctx)
	if err != nil {
		return nil, err
	}
	models = appendCatalogModels(models, seen, catalog)

	configs, err := u.configs.List(ctx)
	if err != nil {
		return nil, err
	}
	return appendConfigModels(models, seen, configs), nil
}

func modelKey(provider, name string) string {
	return strings.ToLower(provider + ":" + name)
}

// appendCatalogModels adds catalog entries not already present in the gateway
// model list.
func appendCatalogModels(models []domain.ModelInfo, seen map[string]bool, catalog []domain.CatalogModel) []domain.ModelInfo {
	for _, m := range catalog {
		key := modelKey(m.Provider, m.Name)
		if seen[key] {
			continue
		}
		seen[key] = true
		models = append(models, domain.ModelInfo{
			ID:       m.ID,
			Name:     m.Name,
			Provider: m.Provider,
			Kind:     m.Kind,
			Source:   "catalogue",
		})
	}
	return models
}

// appendConfigModels adds models referenced by saved configurations, labelled
// with the configuration name.
func appendConfigModels(models []domain.ModelInfo, seen map[string]bool, configs []domain.ChatConfig) []domain.ModelInfo {
	for _, c := range configs {
		key := modelKey(c.Provider, c.Model)
		if seen[key] {
			continue
		}
		seen[key] = true
		models = append(models, domain.ModelInfo{
			Name:     c.Model,
			Provider: c.Provider,
			Source:   c.Name,
		})
	}
	return models
}

// ListForProvider returns the models known for a single provider, i.e. the same
// merged list as List restricted to that provider. An empty provider returns
// nil. Matching is case-insensitive because providers are stored
// free-form in the configurations while the catalogue lowercases them.
func (u *ModelUseCase) ListForProvider(ctx context.Context, provider string) ([]domain.ModelInfo, error) {
	provider = strings.TrimSpace(provider)
	if provider == "" {
		return nil, nil
	}

	models, err := u.List(ctx)
	if err != nil {
		return nil, err
	}

	filtered := make([]domain.ModelInfo, 0, len(models))
	for _, m := range models {
		if strings.EqualFold(m.Provider, provider) {
			filtered = append(filtered, m)
		}
	}
	return filtered, nil
}
