package handlers

import (
	"net/http"
	"strings"
)

// CORS ouvre la surface documentee aux appels cross-origin de navigateur.
//
// Swagger UI peut etre servi depuis une origine differente de celle de
// l'API : la specification declare a la fois http://localhost:4000 (docker)
// et http://localhost:8080 (repli sans BRIDGE_PORT), et la page ouverte sur l'une
// appelle l'autre. Sans ces en-tetes, le navigateur rejete le preflight
// OPTIONS (rejete ici meme en 405) et lit impossible la reponse :
// "Try it out" echoue avant meme d'atteindre le handler.
//
// Seules les routes sans secret sont ouvertes : la specification, l'UI
// Swagger, les sondes de sante et l'API /v1 — laquelle exige une cle
// Bearer, donc ne livre rien a un tiers. Les routes de l'UI web ne sont
// pas concernees : elles n'ont aucune authentification et un site tiers
// ne doit pas pouvoir lire leurs reponses.
func CORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !corsRoute(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		h := w.Header()
		// Wildcard : l'API s'appelle avec une cle Bearer, jamais avec des
		// cookies, donc aucun ambient credential a proteger ici.
		h.Set("Access-Control-Allow-Origin", "*")
		h.Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
		h.Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
		h.Set("Access-Control-Max-Age", "600")

		// Le preflight est repondu ici : le routage rejetterait OPTIONS en
		// 405 et le navigateur n'enverrait jamais la requete reelle.
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// corsRoute decide si la route appartient a la surface ouverte. Le routage
// reste celui de net/http : le repli "/v1/" (404 JSON d'OpenAI compris) est
// couvert par son prefixe, comme les trois sondes de sante.
func corsRoute(path string) bool {
	if path == "/openapi.yaml" || path == "/swagger" {
		return true
	}
	return strings.HasPrefix(path, "/v1/") || strings.HasPrefix(path, "/health/")
}
