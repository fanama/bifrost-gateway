package metrics

import (
	"fmt"
	"log"
	"net/http"
	"runtime/debug"
	"strings"
	"time"

	"bridge-gateway/domain"
)

func (m *MetricsCollector) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := m.now()
		m.beginRequest()
		recorder := &metricsResponseWriter{ResponseWriter: w}

		defer func() {
			recovered := recover()
			status := recorder.statusOr(http.StatusOK)

			if recovered != nil {
				if recovered == http.ErrAbortHandler {
					// 499 (client closed request) n'est pas un statut HTTP
					// emis au client, seulement une trace de monitoring.
					m.finishRequest(r, 499, m.now().Sub(started), "client closed request")
					panic(recovered)
				}
				if recorder.status == 0 {
					recorder.WriteHeader(http.StatusInternalServerError)
				}
				status = http.StatusInternalServerError
				log.Printf("panic serving %s %s: %v\n%s", r.Method, r.URL.Path, recovered, debug.Stack())
				m.finishRequest(r, status, m.now().Sub(started), "panic: "+fmt.Sprint(recovered))
				return
			}

			m.finishRequest(r, status, m.now().Sub(started), "")
		}()

		next.ServeHTTP(recorder, r)
	})
}

func (m *MetricsCollector) beginRequest() {
	m.mu.Lock()
	m.inFlight++
	m.mu.Unlock()
}

// finishRequest records a completed request. panicMessage is set when the
// response came from a panic; otherwise the label comes from the status.
func (m *MetricsCollector) finishRequest(r *http.Request, status int, duration time.Duration, panicMessage string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.inFlight > 0 {
		m.inFlight--
	}
	m.recordStatus(status)
	m.recordLatency(duration)

	path := requestPath(r)
	m.recordRoute(r.Method, path, status, duration)
	now := m.now()
	if status >= 400 {
		m.recordError(now, r.Method, path, status, duration, panicMessage)
	}
	m.recordTimeline(now, status)
}

func (m *MetricsCollector) recordStatus(status int) {
	m.total++
	switch {
	case status >= 500:
		m.serverError++
	case status >= 400:
		m.clientError++
	case status >= 300:
		m.redirects++
	default:
		m.success++
	}
}

func (m *MetricsCollector) recordLatency(duration time.Duration) {
	m.latencies[m.latencyCount%metricsLatencySamples] = duration
	m.latencyCount++
}

// requestPath strips the HTTP method from r.Pattern (Go 1.23 includes it) so
// the route table does not show it twice.
func requestPath(r *http.Request) string {
	path := r.Pattern
	if path == "" {
		return r.URL.Path
	}
	if method, rest, ok := strings.Cut(path, " "); ok && method == r.Method {
		return rest
	}
	return path
}

func (m *MetricsCollector) recordRoute(method, path string, status int, duration time.Duration) {
	key := method + " " + path
	route := m.routes[key]
	if route == nil {
		route = m.newRoute(method, path, key)
	}
	route.count++
	route.totalLatency += duration
	route.lastLatency = duration
	route.lastStatus = status
	if status >= 400 {
		route.errors++
	}
}

func (m *MetricsCollector) newRoute(method, path, key string) *routeAccumulator {
	if len(m.routes) >= metricsMaxRoutes {
		path = "(autres)"
		key = method + " " + path
		if existing := m.routes[key]; existing != nil {
			return existing
		}
	}
	route := &routeAccumulator{method: method, path: path}
	m.routes[key] = route
	return route
}

func (m *MetricsCollector) recordError(now time.Time, method, path string, status int, duration time.Duration, panicMessage string) {
	message := panicMessage
	if message == "" {
		message = http.StatusText(status)
		if message == "" {
			message = "error"
		}
	}
	m.errors = append(m.errors, domain.ErrorMetric{
		Time:    now,
		Method:  method,
		Path:    path,
		Status:  status,
		Latency: duration,
		Message: message,
	})
	if len(m.errors) > metricsErrorHistory {
		m.errors = m.errors[len(m.errors)-metricsErrorHistory:]
	}
}

func (m *MetricsCollector) recordTimeline(now time.Time, status int) {
	sec := now.Unix()
	idx := int(sec % metricsTimelineSeconds)
	if m.buckets[idx].sec != sec {
		m.buckets[idx] = timelineSlot{sec: sec}
	}
	m.buckets[idx].total++
	if status >= 400 {
		m.buckets[idx].errors++
	}
}

// BeginLLM marque un appel provider en cours. Il doit toujours etre suivi d'un
// RecordLLM, y compris en cas d'erreur d'ouverture du flux.

type metricsResponseWriter struct {
	http.ResponseWriter
	status int
	bytes  int64
}

func (w *metricsResponseWriter) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
	}
	w.ResponseWriter.WriteHeader(status)
}

func (w *metricsResponseWriter) Write(p []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	n, err := w.ResponseWriter.Write(p)
	w.bytes += int64(n)
	return n, err
}

func (w *metricsResponseWriter) Flush() {
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (w *metricsResponseWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

func (w *metricsResponseWriter) statusOr(fallback int) int {
	if w.status == 0 {
		return fallback
	}
	return w.status
}
