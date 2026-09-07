package infrastructure

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
)

// JSONStore est le stockage JSON generique partage par toutes les collections
// persistees de l'application. Il gere la serrurage, le chargement/sauvegarde
// atomique du fichier et les invariants transverses (ajout, mise a jour,
// suppression par identifiant). Les adaptations specifiques d'une entite
// (erreurs de domaine, duplicate, listes filtrees) sont portees par les stores
// qui l'enrobent.
type JSONStore[T any] struct {
	mu    sync.RWMutex
	path  string
	items []T
}

// NewJSONStore (anciennement OpenJSONStore) charge la collection depuis path.
// Si le fichier n'existe pas ou est vide, la collection est initialisee vide.
func NewJSONStore[T any](path string) (*JSONStore[T], error) {
	s := &JSONStore[T]{path: path}
	if err := loadJSONFile(path, &s.items); err != nil {
		return nil, err
	}
	if s.items == nil {
		s.items = []T{}
	}
	return s, nil
}

// IsFresh indique si le fichier sous-jacent n'existait pas (le store vient
// d'etre cree a la place d'etre charge depuis un fichier existant).
func (s *JSONStore[T]) IsFresh() bool {
	_, err := os.Stat(s.path)
	return errors.Is(err, os.ErrNotExist)
}

// Seed remplit la collection avec les valeurs produites par fill uniquement
// si le fichier n'existait pas : les seeds sont persistees une fois, jamais
// re-appliquees a un redemarrage suivant.
func (s *JSONStore[T]) Seed(fill func() []T) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.IsFresh() {
		return nil
	}
	s.items = fill()
	if s.items == nil {
		s.items = []T{}
	}
	return s.saveLocked()
}

// All retourne une copie complete de la collection.
func (s *JSONStore[T]) All() ([]T, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]T, len(s.items))
	copy(out, s.items)
	return out, nil
}

// FindWhere retourne (une copie de) la premiere valeur satisfaisant pred.
func (s *JSONStore[T]) FindWhere(pred func(T) bool) (*T, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for i := range s.items {
		if pred(s.items[i]) {
			item := s.items[i]
			return &item, true
		}
	}
	return nil, false
}

// Where retourne toutes les valeurs satisfaisant pred.
func (s *JSONStore[T]) Where(pred func(T) bool) []T {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]T, 0, len(s.items))
	for i := range s.items {
		if pred(s.items[i]) {
			out = append(out, s.items[i])
		}
	}
	return out
}

// Insert ajoute item apres validation par reject (nil si aucun controle).
func (s *JSONStore[T]) Insert(item T, reject func([]T) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if reject != nil {
		if err := reject(s.items); err != nil {
			return err
		}
	}
	s.items = append(s.items, item)
	return s.saveLocked()
}

// Update applique mutate a la valeur dont l'identifiant (via idOf) vaut id.
// Retourne false si aucune valeur ne correspond.
func (s *JSONStore[T]) Update(idOf func(*T) string, id string, mutate func(*T)) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.items {
		if idOf(&s.items[i]) == id {
			mutate(&s.items[i])
			if err := s.saveLocked(); err != nil {
				return true, err
			}
			return true, nil
		}
	}
	return false, nil
}

// Remove supprime la valeur dont l'identifiant (via idOf) vaut id.
// Retourne false si aucune valeur ne correspond.
func (s *JSONStore[T]) Remove(idOf func(*T) string, id string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.items {
		if idOf(&s.items[i]) == id {
			s.items = append(s.items[:i], s.items[i+1:]...)
			if err := s.saveLocked(); err != nil {
				return true, err
			}
			return true, nil
		}
	}
	return false, nil
}

// Mutate applique apply a la collection entiere (transformations portees par
// un store specifique, ex. SetActive) puis persiste uniquement en reussite.
func (s *JSONStore[T]) Mutate(apply func(*[]T) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := apply(&s.items); err != nil {
		return err
	}
	return s.saveLocked()
}

func (s *JSONStore[T]) saveLocked() error {
	data, err := json.MarshalIndent(s.items, "", "  ")
	if err != nil {
		return err
	}
	if err := initFile(filepath.Dir(s.path)); err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}
