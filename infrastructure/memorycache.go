package infrastructure

import (
	"container/list"
	"strings"
	"sync"
	"time"

	"bridge-gateway/domain"
)

// memoryCache est l'adaptateur du port domain.Cache : un cache local en
// memoire, TTL + LRU, dans un unique processus. Il remplace Redis sans
// dependance externe ni serialisation reseau.
//
// Choix d'implementation :
//   - map + doubly linked list pour un O(1) en lecture, ecriture et eviction ;
//   - expiration paresseuse a la lecture (une entree expiree est supprimee au
//     moment ou on la cherche) et un balayage periodique pour liberer la memoire
//     des entrees jamais relues ;
//   - pas d'ecriture concurrente de la valeur retournee : Get renvoie une copie,
//     donc un cacheur ne peut pas muter une entree deja presente.
type MemoryCache struct {
	mu      sync.Mutex
	entries map[string]*list.Element
	order   *list.List // front = plus recemment utilise
	maxSize int
	now     func() time.Time
	stop    chan struct{}
	stopped sync.Once
}

type cacheEntry struct {
	key       string
	value     []byte
	expiresAt time.Time // zero = pas d'expiration
}

// MemoryCacheOptions parametrise le cache.
type MemoryCacheOptions struct {
	// MaxEntries borne la memoire utilisee. Zero ou negatif signifie illimite.
	MaxEntries int
	// SweepInterval fixe la periode de balayage des entrees expirees. Zero
	// desactive le balayage (l'expiration reste appliquee a la lecture).
	SweepInterval time.Duration
	// Now permet d'injecter une horloge dans les tests. Nil utilise time.Now.
	Now func() time.Time
}

// NewMemoryCache construit un cache en memoire et demarre son balayage
// periodique si besoin. Appeler Stop pour liberer la goroutine et le cache.
func NewMemoryCache(opts MemoryCacheOptions) *MemoryCache {
	if opts.Now == nil {
		opts.Now = time.Now
	}
	c := &MemoryCache{
		entries: map[string]*list.Element{},
		order:   list.New(),
		maxSize: opts.MaxEntries,
		now:     opts.Now,
	}
	if opts.SweepInterval > 0 {
		c.stop = make(chan struct{})
		go c.sweep(opts.SweepInterval)
	}
	return c
}

func (c *MemoryCache) Get(key string) ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	el, ok := c.entries[key]
	if !ok {
		return nil, false
	}
	entry := el.Value.(*cacheEntry)
	if c.expired(entry) {
		c.remove(el)
		return nil, false
	}
	c.order.MoveToFront(el)
	return append([]byte(nil), entry.value...), true
}

func (c *MemoryCache) Set(key string, value []byte, ttl time.Duration) {
	if ttl <= 0 {
		// TTL invalide : entree desactivee plutot que de mise en cache infinie.
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	var expiresAt time.Time
	if ttl > 0 {
		expiresAt = c.now().Add(ttl)
	}

	if el, ok := c.entries[key]; ok {
		el.Value.(*cacheEntry).value = append([]byte(nil), value...)
		el.Value.(*cacheEntry).expiresAt = expiresAt
		c.order.MoveToFront(el)
		return
	}

	c.entries[key] = c.order.PushFront(&cacheEntry{
		key:       key,
		value:     append([]byte(nil), value...),
		expiresAt: expiresAt,
	})
	c.evict()
}

// Delete invalide une cle. Une entree absente n'est pas une erreur : les
// invalidations sont idempotentes.
func (c *MemoryCache) Delete(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.entries[key]; ok {
		c.remove(el)
	}
}

// DeletePrefix invalide toutes les cles d'un namespace.
func (c *MemoryCache) DeletePrefix(prefix string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for el := c.order.Front(); el != nil; {
		next := el.Next()
		if strings.HasPrefix(el.Value.(*cacheEntry).key, prefix) {
			c.remove(el)
		}
		el = next
	}
}

func (c *MemoryCache) Purge() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries = map[string]*list.Element{}
	c.order.Init()
}

// Stop arrete la goroutine de balayage.
func (c *MemoryCache) Stop() {
	if c.stop == nil {
		return
	}
	c.stopped.Do(func() { close(c.stop) })
}

func (c *MemoryCache) expired(entry *cacheEntry) bool {
	return !entry.expiresAt.IsZero() && c.now().After(entry.expiresAt)
}

func (c *MemoryCache) remove(el *list.Element) {
	c.order.Remove(el)
	delete(c.entries, el.Value.(*cacheEntry).key)
}

// evict applique la borne LRU, toujours depuis la queue (moins recemment
// utilise). Appelee sous verrou.
func (c *MemoryCache) evict() {
	if c.maxSize <= 0 {
		return
	}
	for c.order.Len() > c.maxSize {
		el := c.order.Back()
		if el == nil {
			return
		}
		c.remove(el)
	}
}

// sweep supprime periodiquement les entrees expirees non relues.
func (c *MemoryCache) sweep(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-c.stop:
			return
		case <-ticker.C:
			c.mu.Lock()
			for el := c.order.Front(); el != nil; {
				next := el.Next()
				if c.expired(el.Value.(*cacheEntry)) {
					c.remove(el)
				}
				el = next
			}
			c.mu.Unlock()
		}
	}
}

var _ domain.Cache = (*MemoryCache)(nil)
