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
	logs     []domain.CallLog
}

func (s stubMetrics) Snapshot() domain.MetricsSnapshot { return s.snapshot }

func (s stubMetrics) Logs() []domain.CallLog { return s.logs }

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
	}, logs: []domain.CallLog{
		{
			Time: time.Date(2026, 10, 30, 10, 0, 0, 0, time.Local), Provider: "ollama", Model: "gemma",
			Duration: 3 * time.Millisecond, Input: "user: Bonjour", Output: "Bonjour !",
		},
		{
			Time: time.Date(2026, 10, 31, 12, 0, 0, 0, time.Local), Provider: "openai", Model: "gpt-4o",
			Duration: 5 * time.Millisecond, Input: "user: Salut", Error: "provider down",
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
	// La modale de logs et son declencheur voyagent avec la page.
	for _, want := range []string{`id="logs-modal"`, `hx-get="/monitoring/logs"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("page is missing %q", want)
		}
	}
}

func TestMonitoringLogsModalShowsInputsAndOutputs(t *testing.T) {
	mux, _ := newMonitoringServer(t)

	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/monitoring/logs", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	body := recorder.Body.String()
	if strings.Contains(body, "<html") || strings.Contains(body, "<body") {
		t.Fatal("logs endpoint must return a fragment, not a full page")
	}
	for _, want := range []string{
		"Logs des appels LLM", "ollama", "gemma", "Entrée", "Sortie",
		"user: Bonjour", "Bonjour !",
		"/monitoring/logs.json", "/monitoring/logs.csv",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("logs fragment is missing %q", want)
		}
	}
}

func TestMonitoringLogsCSVExport(t *testing.T) {
	mux, _ := newMonitoringServer(t)

	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/monitoring/logs.csv", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	if got := recorder.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/csv") {
		t.Fatalf("content type = %q, want text/csv", got)
	}
	if got := recorder.Header().Get("Content-Disposition"); !strings.Contains(got, "attachment") {
		t.Fatalf("content disposition = %q, want an attachment", got)
	}

	lines := strings.Split(strings.TrimSpace(recorder.Body.String()), "\n")
	if len(lines) != 3 {
		t.Fatalf("csv lines = %d, want a header and two entries: %q", len(lines), recorder.Body.String())
	}
	if got, want := lines[0], "time,provider,model,stream,duration_ms,input,output,error"; got != want {
		t.Fatalf("csv header = %q, want %q", got, want)
	}
	if !strings.Contains(recorder.Body.String(), "ollama,gemma,false,3.00,user: Bonjour,Bonjour !,") {
		t.Fatalf("csv is missing the successful call: %q", recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "openai,gpt-4o,false,5.00,user: Salut,,provider down") {
		t.Fatalf("csv is missing the failed call: %q", recorder.Body.String())
	}
}

func TestMonitoringLogsJSONExport(t *testing.T) {
	mux, _ := newMonitoringServer(t)

	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/monitoring/logs.json", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	if got := recorder.Header().Get("Content-Type"); !strings.HasPrefix(got, "application/json") {
		t.Fatalf("content type = %q, want application/json", got)
	}

	var decoded []domain.CallLog
	if err := json.Unmarshal(recorder.Body.Bytes(), &decoded); err != nil {
		t.Fatalf("decode logs: %v", err)
	}
	if len(decoded) != 2 {
		t.Fatalf("logs = %d, want 2", len(decoded))
	}
	if decoded[0].Input != "user: Bonjour" || decoded[0].Output != "Bonjour !" {
		t.Fatalf("exported entry = %+v, want the logged input and output", decoded[0])
	}
}

func TestMonitoringLogsFilters(t *testing.T) {
	mux, _ := newMonitoringServer(t)

	cases := []struct{ query, want, unwanted string }{
		{"?provider=openai", "provider down", "Bonjour !"},
		{"?model=gpt-4o", "provider down", "Bonjour !"},
		{"?status=error", "provider down", "Bonjour !"},
		{"?status=ok", "Bonjour !", "provider down"},
		{"?provider=openai&status=ok", "Aucun appel", "Bonjour !"},
		// Bornes de date sur un jour entier, incluses des deux cotes.
		{"?from=2026-10-31", "provider down", "Bonjour !"},
		{"?to=2026-10-30", "Bonjour !", "provider down"},
		{"?from=2026-10-30&to=2026-10-30", "Bonjour !", "provider down"},
		{"?from=2026-11-01", "Aucun appel", "Bonjour !"},
	}
	for _, tc := range cases {
		recorder := httptest.NewRecorder()
		mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/monitoring/logs"+tc.query, nil))
		if recorder.Code != http.StatusOK {
			t.Fatalf("%s: status = %d, want 200", tc.query, recorder.Code)
		}
		body := recorder.Body.String()
		if !strings.Contains(body, tc.want) {
			t.Fatalf("%s: fragment is missing %q", tc.query, tc.want)
		}
		if strings.Contains(body, tc.unwanted) {
			t.Fatalf("%s: fragment should not contain %q", tc.query, tc.unwanted)
		}
	}
}

func TestMonitoringLogsExportHonoursFilters(t *testing.T) {
	mux, _ := newMonitoringServer(t)

	// Le filtre voyage avec le lien d'export : ce que la modale montre est ce
	// que le fichier contient.
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/monitoring/logs?status=error", nil))
	if body := recorder.Body.String(); !strings.Contains(body, "/monitoring/logs.csv?status=error") {
		t.Fatalf("export link does not carry the filter: %q", body)
	}

	recorder = httptest.NewRecorder()
	mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/monitoring/logs.csv?status=error", nil))
	lines := strings.Split(strings.TrimSpace(recorder.Body.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("csv lines = %d, want a header and the single failed call: %q", len(lines), recorder.Body.String())
	}
	if strings.Contains(recorder.Body.String(), "Bonjour !") {
		t.Fatal("filtered csv still contains a successful call")
	}

	recorder = httptest.NewRecorder()
	mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/monitoring/logs.csv?from=2026-10-30&to=2026-10-30", nil))
	if lines := strings.Split(strings.TrimSpace(recorder.Body.String()), "\n"); len(lines) != 2 {
		t.Fatalf("date-filtered csv lines = %d, want a header and one entry: %q", len(lines), recorder.Body.String())
	}

	recorder = httptest.NewRecorder()
	mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/monitoring/logs.json?provider=ollama", nil))
	var decoded []domain.CallLog
	if err := json.Unmarshal(recorder.Body.Bytes(), &decoded); err != nil {
		t.Fatalf("decode logs: %v", err)
	}
	if len(decoded) != 1 || decoded[0].Provider != "ollama" {
		t.Fatalf("filtered json = %+v, want the single ollama call", decoded)
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
