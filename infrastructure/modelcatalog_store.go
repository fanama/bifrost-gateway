package infrastructure

import (
	"bridge-gateway/domain"
)

type FileModelCatalogStore struct {
	*Adapter[domain.CatalogModel]
}

func NewFileModelCatalogStore(path string, seeds []domain.CatalogModel) (*FileModelCatalogStore, error) {
	if seeds == nil {
		seeds = []domain.CatalogModel{}
	}
	inner, err := NewJSONStore[domain.CatalogModel](path)
	if err != nil {
		return nil, err
	}
	if err := inner.Seed(func() []domain.CatalogModel { return seeds }); err != nil {
		return nil, err
	}
	return &FileModelCatalogStore{Adapter: NewAdapter(
		inner,
		func(m *domain.CatalogModel) string { return m.ID },
		domain.ErrCatalogModelNotFound,
		nil,
	)}, nil
}

var _ domain.ModelCatalogRepository = (*FileModelCatalogStore)(nil)
