package entity

import (
	"context"
	"time"

	"bridge-gateway/domain"
)

// EntityUseCase fournit les operations CRUD generiques partagees par les
// entites gerees depuis l'UI. R est le port de persistance (CrudRepository)
// de l'entite T. Les use cases par entite l'imbriquent (embedding) et
// ajoutent leurs operations specifiques (ex. ProviderUseCase.Update).
// Le champ repo reste accessible depuis le type embarquant.
type EntityUseCase[T any, R domain.CrudRepository[T]] struct {
	Repo  R
	IDGen func() string
	Now   func() time.Time
}

func NewEntityUseCase[T any, R domain.CrudRepository[T]](repo R) EntityUseCase[T, R] {
	return EntityUseCase[T, R]{Repo: repo, IDGen: domain.NewID, Now: time.Now}
}

func (u EntityUseCase[T, R]) List(ctx context.Context) ([]T, error) {
	return u.Repo.List(ctx)
}

func (u EntityUseCase[T, R]) Get(ctx context.Context, id string) (*T, error) {
	return u.Repo.Get(ctx, id)
}

func (u EntityUseCase[T, R]) Delete(ctx context.Context, id string) error {
	return u.Repo.Delete(ctx, id)
}

func (u EntityUseCase[T, R]) NewID(prefix string) string {
	return prefix + "-" + u.IDGen()
}
