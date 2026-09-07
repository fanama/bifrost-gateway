package infrastructure

import (
	"context"

	"bridge-gateway/domain"
)

// Adapter traduit les ports de persistance generiques du domaine
// (CrudRepository[T], UpdatableRepository[T]) vers le JSONStore[T] (adaptee).
// C'est la realisation Go de l'Adapter pattern : le JSONStore expose des
// operations bas niveau (FindWhere, Insert, Update, Remove... sans erreurs de
// domaine) ; l'adapteur les convertit en signatures attendues par
// l'application (erreur notFound, invariants de creation via reject).
//
// Les stores par entite embarquent cet adapteur et n'ajoutent que leurs
// operations specifiques (SetActive, ListByProject, FindByDigest...).
type Adapter[T any] struct {
	store    *JSONStore[T]
	idOf     func(*T) string
	notFound error
	reject   func(item T, items []T) error
}

func NewAdapter[T any](store *JSONStore[T], idOf func(*T) string, notFound error, reject func(item T, items []T) error) *Adapter[T] {
	return &Adapter[T]{store: store, idOf: idOf, notFound: notFound, reject: reject}
}

func (a *Adapter[T]) List(_ context.Context) ([]T, error) {
	return a.store.All()
}

func (a *Adapter[T]) Get(_ context.Context, id string) (*T, error) {
	item, ok := a.store.FindWhere(func(v T) bool { return a.idOf(&v) == id })
	if !ok {
		return nil, a.notFound
	}
	return item, nil
}

func (a *Adapter[T]) Create(_ context.Context, item *T) error {
	return a.store.Insert(*item, func(items []T) error {
		if a.reject != nil {
			return a.reject(*item, items)
		}
		return nil
	})
}

func (a *Adapter[T]) Delete(_ context.Context, id string) error {
	found, err := a.store.Remove(a.idOf, id)
	if err != nil {
		return err
	}
	if !found {
		return a.notFound
	}
	return nil
}

func (a *Adapter[T]) Update(_ context.Context, item *T) error {
	found, err := a.store.Update(a.idOf, a.idOf(item), func(cur *T) { *cur = *item })
	if err != nil {
		return err
	}
	if !found {
		return a.notFound
	}
	return nil
}

var _ domain.CrudRepository[struct{}] = (*Adapter[struct{}])(nil)
var _ domain.UpdatableRepository[struct{}] = (*Adapter[struct{}])(nil)
