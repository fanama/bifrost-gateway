package infrastructure

import (
	"errors"

	"bridge-gateway/domain"
)

type FileProviderStore struct {
	*Adapter[domain.Provider]
}

// NewFileProviderStore charge le store depuis path. Si le fichier n'existe pas
// encore, le store est initialise avec seeds (defauts) puis persiste.
func NewFileProviderStore(path string, seeds []domain.Provider) (*FileProviderStore, error) {
	if seeds == nil {
		seeds = []domain.Provider{}
	}
	inner, err := NewJSONStore[domain.Provider](path)
	if err != nil {
		return nil, err
	}
	if err := inner.Seed(func() []domain.Provider { return seeds }); err != nil {
		return nil, err
	}
	return &FileProviderStore{Adapter: NewAdapter(
		inner,
		func(p *domain.Provider) string { return p.ID },
		domain.ErrProviderNotFound,
		func(item domain.Provider, items []domain.Provider) error {
			for i := range items {
				if items[i].ID == item.ID {
					return errors.New("provider already exists")
				}
			}
			return nil
		},
	)}, nil
}

var _ domain.ProviderRepository = (*FileProviderStore)(nil)
