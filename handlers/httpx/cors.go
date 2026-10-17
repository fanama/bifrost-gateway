package httpx

import (
	"net/http"
	"strings"
)

// CORS opens the documented API/health/swagger routes to browser cross-origin
// calls while keeping unauthenticated web UI routes closed.
func CORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !corsRoute(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		h := w.Header()
		h.Set("Access-Control-Allow-Origin", "*")
		h.Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
		h.Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
		h.Set("Access-Control-Max-Age", "600")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func corsRoute(path string) bool {
	if path == "/openapi.yaml" || path == "/swagger" {
		return true
	}
	return strings.HasPrefix(path, "/v1/") || strings.HasPrefix(path, "/health/")
}
