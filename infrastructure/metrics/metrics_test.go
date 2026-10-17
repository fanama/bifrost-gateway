package metrics

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"bridge-gateway/domain"
)

func TestMetricsCollectorRecordsRequestsAndRoutes(t *testing.T) {
	collector := NewMetricsCollector()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /items/{id}", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("GET /fail", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	handler := collector.Middleware(mux)

	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/items/42", nil))
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/fail", nil))

	snapshot := collector.Snapshot()

	if got, want := snapshot.Requests.Total, int64(2); got != want {
		t.Fatalf("total requests = %d, want %d", got, want)
	}
	if got, want := snapshot.Requests.Success2xx, int64(1); got != want {
		t.Fatalf("2xx = %d, want %d", got, want)
	}
	if got, want := snapshot.Requests.ServerErrors5xx, int64(1); got != want {
		t.Fatalf("5xx = %d, want %d", got, want)
	}
	if got, want := snapshot.Requests.ErrorRate, 50.0; got != want {
		t.Fatalf("error rate = %v, want %v", got, want)
	}
	if snapshot.Requests.InFlight != 0 {
		t.Fatalf("in-flight = %d, want 0", snapshot.Requests.InFlight)
	}
	if got, want := snapshot.Latency.Samples, 2; got != want {
		t.Fatalf("latency samples = %d, want %d", got, want)
	}
	if got, want := len(snapshot.Routes), 2; got != want {
		t.Fatalf("routes = %d, want %d", got, want)
	}

	// Le motif du mux est conserve, pas l'URL brute : /items/42 et /items/43
	// partagent la meme ligne.
	foundPattern := false
	for _, route := range snapshot.Routes {
		if route.Path == "/items/{id}" {
			foundPattern = true
		}
	}
	if !foundPattern {
		t.Fatalf("route pattern /items/{id} missing from %+v", snapshot.Routes)
	}

	if got, want := len(snapshot.Errors), 1; got != want {
		t.Fatalf("errors = %d, want %d", got, want)
	}
	if snapshot.Errors[0].Status != http.StatusInternalServerError {
		t.Fatalf("error status = %d, want 500", snapshot.Errors[0].Status)
	}

	lastBucket := snapshot.Timeline[len(snapshot.Timeline)-1]
	if lastBucket.Total != 2 || lastBucket.Errors != 1 {
		t.Fatalf("last timeline bucket = %+v, want total=2 errors=1", lastBucket)
	}
	if got, want := snapshot.Requests.RequestsPerMinute, 2.0; got != want {
		t.Fatalf("requests per minute = %v, want %v", got, want)
	}
}

func TestMetricsMiddlewareRecoversPanic(t *testing.T) {
	// La pile de panique est journalisee volontairement : on la rend muette
	// pour garder la sortie de test lisible.
	previous := log.Writer()
	log.SetOutput(io.Discard)
	t.Cleanup(func() { log.SetOutput(previous) })

	collector := NewMetricsCollector()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /boom", func(w http.ResponseWriter, r *http.Request) {
		panic("kaboom")
	})

	recorder := httptest.NewRecorder()
	collector.Middleware(mux).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/boom", nil))

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", recorder.Code)
	}

	snapshot := collector.Snapshot()
	if got, want := snapshot.Requests.ServerErrors5xx, int64(1); got != want {
		t.Fatalf("5xx = %d, want %d", got, want)
	}
	if got, want := len(snapshot.Errors), 1; got != want {
		t.Fatalf("errors = %d, want %d", got, want)
	}
	if !strings.Contains(snapshot.Errors[0].Message, "kaboom") {
		t.Fatalf("error message = %q, want it to mention the panic", snapshot.Errors[0].Message)
	}
	if snapshot.Requests.InFlight != 0 {
		t.Fatalf("in-flight leaked after panic: %d", snapshot.Requests.InFlight)
	}
}

func TestMetricsLatencyPercentiles(t *testing.T) {
	collector := NewMetricsCollector()
	request := httptest.NewRequest(http.MethodGet, "/x", nil)

	for i := 1; i <= 100; i++ {
		collector.finishRequest(request, http.StatusOK, time.Duration(i)*time.Millisecond, "")
	}

	latency := collector.Snapshot().Latency
	if latency.Samples != 100 {
		t.Fatalf("samples = %d, want 100", latency.Samples)
	}
	if latency.P50 != 50*time.Millisecond {
		t.Fatalf("p50 = %v, want 50ms", latency.P50)
	}
	if latency.P95 != 95*time.Millisecond {
		t.Fatalf("p95 = %v, want 95ms", latency.P95)
	}
	if latency.P99 != 99*time.Millisecond {
		t.Fatalf("p99 = %v, want 99ms", latency.P99)
	}
	if latency.Min != time.Millisecond || latency.Max != 100*time.Millisecond {
		t.Fatalf("min/max = %v/%v, want 1ms/100ms", latency.Min, latency.Max)
	}
	if latency.Avg != 50500*time.Microsecond {
		t.Fatalf("avg = %v, want 50.5ms", latency.Avg)
	}
}

func TestMetricsRoutesAreBounded(t *testing.T) {
	collector := NewMetricsCollector()

	for i := 0; i < metricsMaxRoutes+50; i++ {
		request := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/unknown/%d", i), nil)
		collector.finishRequest(request, http.StatusNotFound, time.Millisecond, "")
	}

	snapshot := collector.Snapshot()
	if len(snapshot.Routes) > metricsMaxRoutes+2 {
		t.Fatalf("routes = %d, want at most %d", len(snapshot.Routes), metricsMaxRoutes+2)
	}

	foundOther := false
	for _, route := range snapshot.Routes {
		if route.Path == "(autres)" {
			foundOther = true
		}
	}
	if !foundOther {
		t.Fatal("overflowing routes were not aggregated under (autres)")
	}
}

func TestMetricsResponseWriterPreservesFlusher(t *testing.T) {
	collector := NewMetricsCollector()
	flushed := false
	handler := collector.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("middleware writer does not implement http.Flusher (SSE would break)")
		}
		flusher.Flush()
		flushed = true
	}))

	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/stream", nil))
	if !flushed {
		t.Fatal("handler never reached Flush")
	}
}

type fakeLLM struct {
	result domain.LLMResult
	err    error
}

func (f fakeLLM) Chat(context.Context, *domain.ChatConfig, []domain.ChatMessage) (domain.LLMResult, error) {
	return f.result, f.err
}

type fakeStreamingLLM struct {
	fakeLLM
}

func (f fakeStreamingLLM) ChatStream(context.Context, *domain.ChatConfig, []domain.ChatMessage) (<-chan domain.StreamEvent, error) {
	events := make(chan domain.StreamEvent, 2)
	events <- domain.StreamEvent{Role: "assistant", Delta: "hello", Model: "gemma"}
	events <- domain.StreamEvent{Delta: "!", Model: "gemma", Done: true}
	close(events)
	return events, nil
}

func TestMeasuredLLMProviderCountsCallsStreamsAndErrors(t *testing.T) {
	collector := NewMetricsCollector()
	cfg := &domain.ChatConfig{Provider: "ollama", Model: "gemma"}

	provider := NewMeasuredLLMProvider(fakeStreamingLLM{fakeLLM{result: domain.LLMResult{Content: "hi", Model: "gemma"}}}, collector)
	if _, err := provider.Chat(context.Background(), cfg, nil); err != nil {
		t.Fatalf("chat: %v", err)
	}
	stream, err := provider.ChatStream(context.Background(), cfg, nil)
	if err != nil {
		t.Fatalf("chat stream: %v", err)
	}
	for range stream {
	}

	failing := NewMeasuredLLMProvider(fakeLLM{err: errors.New("provider down")}, collector)
	if _, err := failing.Chat(context.Background(), cfg, nil); err == nil {
		t.Fatal("expected error from failing provider")
	}

	snapshot := collector.Snapshot().LLM
	if got, want := snapshot.Calls, int64(3); got != want {
		t.Fatalf("calls = %d, want %d", got, want)
	}
	if got, want := snapshot.Streams, int64(1); got != want {
		t.Fatalf("streams = %d, want %d", got, want)
	}
	if got, want := snapshot.Errors, int64(1); got != want {
		t.Fatalf("errors = %d, want %d", got, want)
	}
	if snapshot.InFlight != 0 {
		t.Fatalf("in-flight = %d, want 0", snapshot.InFlight)
	}
	if got, want := len(snapshot.Models), 1; got != want {
		t.Fatalf("models = %d, want %d", got, want)
	}
	if snapshot.Models[0].Provider != "ollama" || snapshot.Models[0].Model != "gemma" {
		t.Fatalf("model metric = %+v, want ollama/gemma", snapshot.Models[0])
	}
}
