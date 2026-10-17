package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type recordingHandler struct {
	calls   int
	methods []string
}

func (h *recordingHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.calls++
	h.methods = append(h.methods, r.Method)
	w.WriteHeader(http.StatusTeapot)
}

func preflight(target string) *http.Request {
	req := httptest.NewRequest(http.MethodOptions, target, nil)
	req.Header.Set("Origin", "http://localhost:8080")
	req.Header.Set("Access-Control-Request-Method", http.MethodPost)
	req.Header.Set("Access-Control-Request-Headers", "authorization,content-type")
	return req
}

func TestCORSPreflightIntercepte(t *testing.T) {
	inner := &recordingHandler{}
	rec := httptest.NewRecorder()
	CORS(inner).ServeHTTP(rec, preflight("/v1/chat/completions"))

	if rec.Code != http.StatusNoContent {
		t.Errorf("preflight status = %d, want 204", rec.Code)
	}
	if inner.calls != 0 {
		t.Errorf("preflight reached inner handler (%d call(s))", inner.calls)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Errorf("Allow-Origin = %q, want *", got)
	}
	if m := rec.Header().Get("Access-Control-Allow-Methods"); !strings.Contains(m, http.MethodPost) {
		t.Errorf("Allow-Methods = %q, missing POST", m)
	}
	if h := rec.Header().Get("Access-Control-Allow-Headers"); !strings.Contains(h, "Authorization") || !strings.Contains(h, "Content-Type") {
		t.Errorf("Allow-Headers = %q, missing Authorization/Content-Type", h)
	}
}

func TestCORSReponseAPI(t *testing.T) {
	inner := &recordingHandler{}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	req.Header.Set("Origin", "http://localhost:8080")
	CORS(inner).ServeHTTP(rec, req)

	if inner.calls != 1 {
		t.Fatalf("handler called %d times, want 1", inner.calls)
	}
	if rec.Code != http.StatusTeapot {
		t.Errorf("status = %d, want handler status", rec.Code)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Errorf("Allow-Origin = %q, want *", got)
	}
}

func TestCORSRoutesWebFermees(t *testing.T) {
	for _, target := range []string{"/projects", "/"} {
		inner := &recordingHandler{}

		rec := httptest.NewRecorder()
		CORS(inner).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
		if inner.calls != 1 {
			t.Errorf("GET %s: handler called %d times, want 1", target, inner.calls)
		}
		if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
			t.Errorf("GET %s: Allow-Origin = %q, want empty", target, got)
		}

		rec = httptest.NewRecorder()
		CORS(inner).ServeHTTP(rec, preflight(target))
		if rec.Code == http.StatusNoContent {
			t.Errorf("OPTIONS %s: preflight intercepted outside CORS surface", target)
		}
		if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
			t.Errorf("OPTIONS %s: Allow-Origin = %q, want empty", target, got)
		}
	}
}

func TestCORSSurfaceCouverte(t *testing.T) {
	covered := []string{
		"/openapi.yaml", "/swagger",
		"/v1/chat/completions", "/v1/embeddings", "/v1/models",
		"/v1/models/onnx/all-MiniLM-L6-v2", "/v1/inconnue",
		"/health/liveness", "/health/readiness", "/health/test_connection",
	}
	closed := []string{
		"/", "/projects", "/models", "/chat/completions", "/embeddings",
	}
	for _, path := range covered {
		inner := &recordingHandler{}
		rec := httptest.NewRecorder()
		CORS(inner).ServeHTTP(rec, preflight(path))
		if rec.Code != http.StatusNoContent || inner.calls != 0 {
			t.Errorf("%s: preflight status=%d handler=%d, want 204 without reaching handler", path, rec.Code, inner.calls)
		}
	}
	for _, path := range closed {
		inner := &recordingHandler{}
		rec := httptest.NewRecorder()
		CORS(inner).ServeHTTP(rec, preflight(path))
		if rec.Code == http.StatusNoContent {
			t.Errorf("%s: preflight answered outside CORS surface", path)
		}
	}
}
