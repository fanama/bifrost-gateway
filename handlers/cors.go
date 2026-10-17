package handlers

import (
	"net/http"

	"bridge-gateway/handlers/httpx"
)

// CORS opens documented API, health, and Swagger routes to browser
// cross-origin requests while keeping unauthenticated web UI routes closed.
func CORS(next http.Handler) http.Handler {
	return httpx.CORS(next)
}
