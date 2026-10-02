package infrastructure

import (
	"sync"
	"time"
)

// cooldownStore memorise les cibles temporairement ecartees de l'echelle de
// routage.
//
// L'etat est volontairement en memoire et non dans SQLite : un cooldown perdu au
// redemarrage ne coute qu'une tentative supplementaire, alors qu'une ecriture en
// base pour un evenement ephemere ajouterait de la latence sur le chemin
// critique d'une requete.
type cooldownStore struct {
	mu   sync.Mutex
	ttl  time.Duration
	now  func() time.Time
	seen map[string]time.Time
}

func newCooldownStore(ttl time.Duration) *cooldownStore {
	if ttl < 0 {
		ttl = 0
	}
	return &cooldownStore{
		ttl:  ttl,
		now:  time.Now,
		seen: map[string]time.Time{},
	}
}

// Mark ecarte une cle pour la duree configuree.
func (c *cooldownStore) Mark(key string) {
	if c == nil || c.ttl <= 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.seen[key] = c.now().Add(c.ttl)
}

// Active indique si la cle est encore ecartee. L'entree est retiree au passage,
// donc le store ne grossit pas avec les cles expirees.
func (c *cooldownStore) Active(key string) bool {
	if c == nil || c.ttl <= 0 {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	until, ok := c.seen[key]
	if !ok {
		return false
	}
	if !c.now().Before(until) {
		delete(c.seen, key)
		return false
	}
	return true
}

// len exposes la taille du store pour les tests.
func (c *cooldownStore) size() int {
	if c == nil {
		return 0
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.seen)
}
