package domain

import "time"

// Cache est le port d'un cache local (en memoire) substitue a Redis.
//
// Deux usages distincts cohabitent, avec des cles namespaces pour eviter toute
// collision :
//
//   - Cache des lectures de repositories (resolution de cle API par digest,
//     config active d'un projet, catalogue de modeles), pour ne pas requeter
//     SQLite a chaque requete HTTP ;
//   - Cache des reponses LLM, pour ne pas rappeler un provider a l'identique.
//
// Les entrees expirent apres ttl. Une duree nulle ou negative desactive le
// cache correspondant : Get renvoie alors toujours miss et Set est ignoré, ce qui
// permet de desactiver le cache sans changer le code appelant.
type Cache interface {
	// Get retourne l'entree associee a key si elle existe et n'a pas expire.
	Get(key string) ([]byte, bool)
	// Set enregistre value sous key pendant ttl.
	Set(key string, value []byte, ttl time.Duration)
	// Delete invalide une cle. Utilise par les writes pour eviter de servir
	// une valeur perimee.
	Delete(key string)
	// DeletePrefix invalide toutes les cles d'un namespace. Les invalidations
	// par entite (une configuration modifiee, un projet renomme) ne connaissent
	// pas les cles concretes construites par les readers.
	DeletePrefix(prefix string)
	// Purge vide integralement le cache.
	Purge()
}

// CacheKeyFactory construit des cles namespacing a partir d'un nom et d'une ou
// plusieurs parties. Les separateurs evitent les collisions entre
// ("project-general", "p1") et ("project", "general-p1").
type CacheKeyFactory struct {
	prefix string
}

// NewCacheKeyFactory cree une fabrique de cles pour un namespace donne.
func NewCacheKeyFactory(prefix string) CacheKeyFactory {
	return CacheKeyFactory{prefix: prefix}
}

// Key assemble une cle a partir des parties fournies.
func (f CacheKeyFactory) Key(parts ...string) string {
	key := f.prefix
	for _, part := range parts {
		key += "\x00" + part
	}
	return key
}

// Prefix namespace toutes les cles de la fabrique, y compris celles derivables
// par DeletePrefix.
func (f CacheKeyFactory) Prefix() string { return f.prefix + "\x00" }
