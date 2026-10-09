// Package docs publie la documentation de l'API : la specification OpenAPI 3
// est embarquee dans le binaire (go:embed) et servie en local, l'interface
// Swagger UI l'affiche depuis ce meme point d'entree. Aucune dependance
// d'execution — l'UI charge ses assets depuis un CDN.
package docs

import (
	"embed"
	"net/http"
)

//go:embed openapi.yaml
var spec embed.FS

// SpecHandler sert openapi.yaml (type YAML) : la source de verite de la
// documentation, consommable par Swagger UI, Redoc ou un generateur de SDK.
func SpecHandler() http.Handler {
	data, err := spec.ReadFile("openapi.yaml")
	if err != nil {
		// Impossible : le fichier est compile dans le binaire. Une panne ici
		// est un defaut de build, pas une erreur de requete.
		panic("docs: openapi.yaml absent du binaire: " + err.Error())
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		w.Write(data)
	})
}

// swaggerUIPage est l'interface servie sur /swagger. Swagger UI est charge
// depuis unpkg (CDN) : sans reseau, la page reste ouvrable mais vide — la
// specification, elle, est servie en local sur /openapi.yaml.
//
// Le requestInterceptor rapatrie chaque "Try it out" sur l'origine qui sert
// la page : la specification declare http://localhost:4000 (docker) et
// http://localhost:8080 (repli sans BRIDGE_PORT), dont une seule — parfois aucune —
// repond selon le mode de demarrage. Appeler l'origine de la page, c'est
// appeler la passerelle qui sert cette documentation, sans preflight CORS.
const swaggerUIPage = `<!doctype html>
<html lang="fr">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Bridge Gateway — API</title>
<link rel="stylesheet" href="https://unpkg.com/swagger-ui-dist@5/swagger-ui.css">
<style>body{margin:0;background:#fafafa}</style>
</head>
<body>
<div id="swagger-ui"></div>
<script src="https://unpkg.com/swagger-ui-dist@5/swagger-ui-bundle.js"></script>
<script>
SwaggerUIBundle({
  url: "/openapi.yaml",
  dom_id: "#swagger-ui",
  deepLinking: true,
  tryItOutEnabled: true,
  presets: [SwaggerUIBundle.presets.apis],
  layout: "BaseLayout",
  requestInterceptor: function (req) {
    // Reecrit toute URL d'une autre origine vers celle de la page :
    // spec fetch (relatif, deja bon) et requetes d'essai vers :4000/:8080.
    try {
      var target = new URL(req.url, window.location.origin);
      if (target.origin !== window.location.origin) {
        req.url = window.location.origin + target.pathname + target.search;
      }
    } catch (e) {}
    return req;
  }
});
</script>
</body>
</html>
`

// UIHandler sert la page Swagger UI sur /swagger.
func UIHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(swaggerUIPage))
	})
}

// Register branche les deux routes sur le mux de l'application :
//
//	GET /swagger       interface Swagger UI
//	GET /openapi.yaml  specification OpenAPI 3
//
// Elles sont sans authentification : la documentation decrit l'API, elle ne
// l'expose pas — chaque route /v1/ reste protegee par sa propre cle.
func Register(mux *http.ServeMux) {
	mux.Handle("/openapi.yaml", SpecHandler())
	mux.Handle("/swagger", UIHandler())
}
