package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"bridge-gateway/domain"
)

type stubMetrics struct {
	snapshot domain.MetricsSnapshot
}

func (s stubMetrics) Snapshot() domain.MetricsSnapshot { return s.snapshot }

func newMonitoringServer(t *testing.T) (*http.ServeMux, domain.MetricsSnapshot) {
	t.Helper()

	metrics := stubMetrics{snapshot: domain.MetricsSnapshot{
		StartedAt: time.Now().Add(-2 * time.Minute),
		Uptime:    2 * time.Minute,
		Requests: domain.RequestMetrics{
			Total:             42,
			Success2xx:        40,
			ClientErrors4xx:   2,
			ErrorRate:         4.76,
			RequestsPerMinute: 7,
		},
		Latency: domain.LatencyMetrics{
			Samples: 42,
			Min:     time.Millisecond,
			Avg:     3 * time.Millisecond,
			P50:     2 * time.Millisecond,
			P95:     8 * time.Millisecond,
			P99:     12 * time.Millisecond,
			Max:     20 * time.Millisecond,
		},
		Statuses: []domain.StatusMetric{{Label: "2xx", Count: 40}, {Label: "4xx", Count: 2}},
		Routes: []domain.RouteMetric{{
			Method: "GET", Path: "/monitoring", Count: 42, LastStatus: 200, AvgLatency: 2 * time.Millisecond,
		}},
		Errors: []domain.ErrorMetric{{
			Time: time.Now(), Method: "POST", Path: "/chat/send", Status: 500, Message: "internal server error",
		}},
		Timeline: []domain.TimelineBucket{
			{Label: "12:00:00", Total: 1},
			{Label: "12:00:01", Total: 3},
		},
		Runtime: domain.RuntimeMetrics{Goroutines: 12, GoVersion: "go1.26", OS: "darwin", Arch: "arm64"},
		LLM: domain.LLMMetrics{
			Calls: 5, Models: []domain.LLMModelMetric{{Provider: "ollama", Model: "gemma", Calls: 5}},
		},
	}}

	server, err := NewServer(nil, nil, nil, nil, nil, nil, nil, nil, metrics, "http://localhost:11434")
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	mux := http.NewServeMux()
	server.Register(mux)
	return mux, metrics.snapshot
}

func TestMonitoringPageRendersDashboard(t *testing.T) {
	mux, _ := newMonitoringServer(t)

	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/monitoring", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	body := recorder.Body.String()
	for _, want := range []string{
		"Requêtes servies", ">42<", "Taux d'erreur", "Latence p95",
		"/monitoring", "ollama", "gemma", "internal server error",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("page is missing %q", want)
		}
	}
	// La page porte l'auto-refresh HTMX et la balise du fragment.
	for _, want := range []string{`hx-get="/monitoring/stats"`, `hx-trigger="every 3s"`, `id="monitoring-dashboard"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("page is missing %q", want)
		}
	}
}

func TestMonitoringStatsReturnsFragmentOnly(t *testing.T) {
	mux, _ := newMonitoringServer(t)

	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/monitoring/stats", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	if got := recorder.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/html") {
		t.Fatalf("content type = %q, want text/html", got)
	}
	body := recorder.Body.String()
	if strings.Contains(body, "<html") || strings.Contains(body, "<body") {
		t.Fatal("stats endpoint must return a fragment, not a full page")
	}
	if !strings.Contains(body, "Requêtes servies") {
		t.Fatal("fragment is missing the dashboard content")
	}
}

func TestMonitoringMetricsJSON(t *testing.T) {
	mux, snapshot := newMonitoringServer(t)

	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/monitoring/metrics.json", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	if got := recorder.Header().Get("Content-Type"); !strings.HasPrefix(got, "application/json") {
		t.Fatalf("content type = %q, want application/json", got)
	}
	if got := recorder.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("cache control = %q, want no-store", got)
	}

	var decoded domain.MetricsSnapshot
	if err := json.Unmarshal(recorder.Body.Bytes(), &decoded); err != nil {
		t.Fatalf("decode snapshot: %v", err)
	}
	if decoded.Requests.Total != snapshot.Requests.Total {
		t.Fatalf("total = %d, want %d", decoded.Requests.Total, snapshot.Requests.Total)
	}
	if got := decoded.LLM.Models; len(got) != 1 || got[0].Model != "gemma" {
		t.Fatalf("llm models = %+v, want one gemma entry", got)
	}
}
