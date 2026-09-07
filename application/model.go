package application

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
	models := make([]domain.ModelInfo, 0, len(u.gatewayModels)+8)

	seen := make(map[string]bool)
	mark := func(provider, name string) {
		seen[strings.ToLower(provider+":"+name)] = true
	}
	for _, m := range u.gatewayModels {
		mark(m.Provider, m.Name)
	}
	models = append(models, u.gatewayModels...)

	catalog, err := u.catalog.List(ctx)
	if err != nil {
		return nil, err
	}
	for _, m := range catalog {
		if seen[strings.ToLower(m.Provider+":"+m.Name)] {
			continue
		}
		mark(m.Provider, m.Name)
		models = append(models, domain.ModelInfo{
			ID:       m.ID,
			Name:     m.Name,
			Provider: m.Provider,
			Source:   "catalogue",
		})
	}

	configs, err := u.configs.List(ctx)
	if err != nil {
		return nil, err
	}
	for _, c := range configs {
		key := strings.ToLower(c.Provider + ":" + c.Model)
		if seen[key] {
			continue
		}
		mark(c.Provider, c.Model)
		models = append(models, domain.ModelInfo{
			Name:     c.Model,
			Provider: c.Provider,
			Source:   c.Name,
		})
	}

	return models, nil
}
