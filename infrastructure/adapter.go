package infrastructure

import (
	"context"

	"bridge-gateway/domain"
)

// Adapter traduit les ports de persistance generiques du domaine
// (CrudRepository[T], UpdatableRepository[T]) vers le SQLStore[T].
//
// C'est la realisation Go de l'Adapter pattern : le SQLStore expose des
// operations bas niveau sur une table (All, FindBySQL, Insert, Replace, Remove)
// sans erreurs de domaine ; l'adapteur les convertit en signatures attendues
// par l'application (notFound quand l'identifiant est absent, invariants de
// creation via reject).
//
// Les stores par entite embarquent cet adapteur et n'ajoutent que leurs
// operations specifiques (SetActive, ListByProject, FindByDigest...).
type Adapter[T any] struct {
	store    *SQLStore[T]
	idOf     func(*T) string
	notFound error
	reject   func(item T, items []T) error
}

func NewAdapter[T any](store *SQLStore[T], idOf func(*T) string, notFound error, reject func(item T, items []T) error) *Adapter[T] {
	return &Adapter[T]{store: store, idOf: idOf, notFound: notFound, reject: reject}
}

// Store expose le SQLStore sous-jacent aux stores d'entite, pour requeter la
// table via des clauses SQL indexees (ListByProject, FindByDigest...).
func (a *Adapter[T]) Store() *SQLStore[T] { return a.store }

func (a *Adapter[T]) List(ctx context.Context) ([]T, error) {
	return a.store.All(ctx)
}

func (a *Adapter[T]) Get(ctx context.Context, id string) (*T, error) {
	item, ok, err := a.store.FindBySQL(ctx, "id = ?", id)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, a.notFound
	}
	return item, nil
}

func (a *Adapter[T]) Create(ctx context.Context, item *T) error {
	return a.store.Insert(ctx, item, func(items []T) error {
		if a.reject != nil {
			return a.reject(*item, items)
		}
		return nil
	})
}

func (a *Adapter[T]) Delete(ctx context.Context, id string) error {
	found, err := a.store.Remove(ctx, id)
	if err != nil {
		return err
	}
	if !found {
		return a.notFound
	}
	return nil
}

func (a *Adapter[T]) Update(ctx context.Context, item *T) error {
	found, err := a.store.Replace(ctx, a.idOf(item), item)
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
