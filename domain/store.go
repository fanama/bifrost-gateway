package domain

import "context"

// CrudRepository est le port generique commun a toute collection persistee.
// Chaque entite expose son interface dediee en la composant (voir project.go,
// provider.go, ...) : dependency inversion sans duplication d'implementation.
type CrudRepository[T any] interface {
	List(ctx context.Context) ([]T, error)
	Get(ctx context.Context, id string) (*T, error)
	Create(ctx context.Context, item *T) error
	Delete(ctx context.Context, id string) error
}

// UpdatableRepository etend CrudRepository avec la mise a jour.
type UpdatableRepository[T any] interface {
	CrudRepository[T]
	Update(ctx context.Context, item *T) error
}
