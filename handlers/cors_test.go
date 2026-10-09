package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// recordingHandler note chaque passage pour prouver ce que le middleware
// intercepte (preflight) et ce qu'il laisse traverser (le reste).
type recordingHandler struct {
	calls   int
	methods []string
}

func (h *recordingHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.calls++
	h.methods = append(h.methods, r.Method)
	w.WriteHeader(http.StatusTeapot)
}

// preflight construit le OPTIONS envoye par le navigateur avant tout appel
// cross-origin porte par Authorization et Content-Type.
func preflight(target string) *http.Request {
	req := httptest.NewRequest(http.MethodOptions, target, nil)
	req.Header.Set("Origin", "http://localhost:8080")
	req.Header.Set("Access-Control-Request-Method", http.MethodPost)
	req.Header.Set("Access-Control-Request-Headers", "authorization,content-type")
	return req
}

// TestCORSPreflightIntercepte : le preflight d'une route /v1/ repond 204
// avec les en-tetes attendus, sans jamais atteindre le handler (qui, sans
// le middleware, le rejeterait en 405 et bloquerait le navigateur).
func TestCORSPreflightIntercepte(t *testing.T) {
	inner := &recordingHandler{}
	rec := httptest.NewRecorder()
	CORS(inner).ServeHTTP(rec, preflight("/v1/chat/completions"))

	if rec.Code != http.StatusNoContent {
		t.Errorf("preflight status = %d, attendu 204", rec.Code)
	}
	if inner.calls != 0 {
		t.Errorf("preflight a atteint le handler (%d appel(s))", inner.calls)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Errorf("Allow-Origin = %q, attendu *", got)
	}
	if m := rec.Header().Get("Access-Control-Allow-Methods"); !strings.Contains(m, http.MethodPost) {
		t.Errorf("Allow-Methods = %q, sans POST", m)
	}
	if h := rec.Header().Get("Access-Control-Allow-Headers"); !strings.Contains(h, "Authorization") || !strings.Contains(h, "Content-Type") {
		t.Errorf("Allow-Headers = %q, sans Authorization/Content-Type", h)
	}
}

// TestCORSReponseAPI : une requete reelle sur /v1/ traverse le handler et
// porte les en-tetes qui rendent la reponse lisible depuis un autre origine.
func TestCORSReponseAPI(t *testing.T) {
	inner := &recordingHandler{}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	req.Header.Set("Origin", "http://localhost:8080")
	CORS(inner).ServeHTTP(rec, req)

	if inner.calls != 1 {
		t.Fatalf("handler appele %d fois, attendu 1", inner.calls)
	}
	if rec.Code != http.StatusTeapot {
		t.Errorf("status = %d, attendu celui du handler", rec.Code)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Errorf("Allow-Origin = %q, attendu *", got)
	}
}

// TestCORSRoutesWebFermees : l'UI web (sans authentification) reste hors
// CORS — un site tiers ne doit ni lire ses reponses ni voir ses preflights
// repondus ici.
func TestCORSRoutesWebFermees(t *testing.T) {
	for _, target := range []string{"/projects", "/"} {
		inner := &recordingHandler{}

		rec := httptest.NewRecorder()
		CORS(inner).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
		if inner.calls != 1 {
			t.Errorf("GET %s: handler appele %d fois, attendu 1", target, inner.calls)
		}
		if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
			t.Errorf("GET %s: Allow-Origin = %q, attendu vide", target, got)
		}

		rec = httptest.NewRecorder()
		CORS(inner).ServeHTTP(rec, preflight(target))
		if rec.Code == http.StatusNoContent {
			t.Errorf("OPTIONS %s: preflight intercepte alors que la route est hors CORS", target)
		}
		if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
			t.Errorf("OPTIONS %s: Allow-Origin = %q, attendu vide", target, got)
		}
	}
}

// TestCORSSurfaceCouverte : exactement la surface documentee repond au
// preflight — ni l'API /v1/ ni les sondes ni la documentation ne doivent
// manquer, ni l'UI web s'y glisser.
func TestCORSSurfaceCouverte(t *testing.T) {
	couvertes := []string{
		"/openapi.yaml", "/swagger",
		"/v1/chat/completions", "/v1/embeddings", "/v1/models",
		"/v1/models/onnx/all-MiniLM-L6-v2", "/v1/inconnue",
		"/health/liveness", "/health/readiness", "/health/test_connection",
	}
	fermees := []string{
		"/", "/projects", "/models", "/chat/completions", "/embeddings",
	}
	for _, path := range couvertes {
		inner := &recordingHandler{}
		rec := httptest.NewRecorder()
		CORS(inner).ServeHTTP(rec, preflight(path))
		if rec.Code != http.StatusNoContent || inner.calls != 0 {
			t.Errorf("%s: preflight status=%d handler=%d, attendu 204 sans passage", path, rec.Code, inner.calls)
		}
	}
	for _, path := range fermees {
		inner := &recordingHandler{}
		rec := httptest.NewRecorder()
		CORS(inner).ServeHTTP(rec, preflight(path))
		if rec.Code == http.StatusNoContent {
			t.Errorf("%s: preflight repondu hors surface CORS", path)
		}
	}
}
