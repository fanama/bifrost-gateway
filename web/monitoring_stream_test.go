package web

import (
	"bufio"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"bridge-gateway/domain"
)

// streamMetrics remplace le collecteur : le test pilote le flux publie et
// verifie que l'abonnement est bien libere a la deconnexion.
type streamMetrics struct {
	stubMetrics
	feed   chan domain.MetricsSnapshot
	cancel chan struct{}
}

func (s *streamMetrics) Subscribe() (<-chan domain.MetricsSnapshot, func()) {
	return s.feed, func() {
		select {
		case s.cancel <- struct{}{}:
		default:
		}
	}
}

func newStreamingMonitoringServer(t *testing.T) (*httptest.Server, *streamMetrics) {
	t.Helper()

	metrics := &streamMetrics{
		stubMetrics: stubMetrics{snapshot: domain.MetricsSnapshot{
			Requests: domain.RequestMetrics{Total: 7},
		}},
		feed:   make(chan domain.MetricsSnapshot, 1),
		cancel: make(chan struct{}, 1),
	}
	server, err := NewServer(nil, nil, nil, nil, nil, nil, nil, nil, metrics, "http://localhost:11434")
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	mux := http.NewServeMux()
	server.Register(mux)
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return ts, metrics
}

func TestMonitoringEventsStreamsDashboardFrames(t *testing.T) {
	ts, metrics := newStreamingMonitoringServer(t)

	resp, err := http.Get(ts.URL + "/monitoring/events")
	if err != nil {
		t.Fatalf("open stream: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if got := resp.Header.Get("Content-Type"); got != "text/event-stream" {
		t.Fatalf("content type = %q, want text/event-stream", got)
	}
	if got := resp.Header.Get("Cache-Control"); got != "no-store" {
		t.Fatalf("cache control = %q, want no-store", got)
	}

	events := make(chan string, 8)
	done := make(chan struct{})
	go scanSSEEvents(resp.Body, events, done)
	t.Cleanup(func() { close(done) })

	// Premiere trame : l'instantane courant, rendu par le meme template que
	// le fragment HTMX.
	first := nextSSEEvent(t, events)
	if !strings.Contains(first, "event: dashboard") {
		t.Fatalf("first frame is not a dashboard event: %q", first)
	}
	if !strings.Contains(first, ">7<") {
		t.Fatalf("first frame is missing the initial snapshot: %q", first)
	}

	metrics.feed <- domain.MetricsSnapshot{Requests: domain.RequestMetrics{Total: 99}}

	second := nextSSEEvent(t, events)
	if !strings.Contains(second, ">99<") {
		t.Fatalf("pushed frame is missing the published snapshot: %q", second)
	}

	resp.Body.Close()
	select {
	case <-metrics.cancel:
	case <-time.After(2 * time.Second):
		t.Fatal("subscription was not released when the client disconnected")
	}
}

// scanSSEEvents decoupe le flux en trames (une trame = les lignes jusqu'a la
// ligne vide) et les publie jusqu'a la fin du corps ou la fin du test.
func scanSSEEvents(body io.Reader, events chan<- string, done <-chan struct{}) {
	reader := bufio.NewReader(body)
	var frame strings.Builder
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return
		}
		if line == "\n" {
			select {
			case events <- frame.String():
			case <-done:
				return
			}
			frame.Reset()
			continue
		}
		frame.WriteString(line)
	}
}

func nextSSEEvent(t *testing.T, events <-chan string) string {
	t.Helper()
	select {
	case event := <-events:
		return event
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for an SSE frame")
		return ""
	}
}
