package metrics

import (
	"math"
	"runtime"
	"sort"
	"time"

	"bridge-gateway/domain"
)

func (m *MetricsCollector) Snapshot() domain.MetricsSnapshot {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := m.now()
	snapshot := domain.MetricsSnapshot{
		StartedAt:   m.startedAt,
		GeneratedAt: now,
		Uptime:      now.Sub(m.startedAt),
		Requests: domain.RequestMetrics{
			Total:           m.total,
			InFlight:        m.inFlight,
			Success2xx:      m.success,
			Redirects3xx:    m.redirects,
			ClientErrors4xx: m.clientError,
			ServerErrors5xx: m.serverError,
		},
		Latency: snapshotLatency(m.latencies[:], m.latencyCount),
		Statuses: []domain.StatusMetric{
			{Label: "2xx", Count: m.success},
			{Label: "3xx", Count: m.redirects},
			{Label: "4xx", Count: m.clientError},
			{Label: "5xx", Count: m.serverError},
		},
		Routes:   snapshotRoutes(m.routes),
		Errors:   snapshotErrors(m.errors),
		Timeline: snapshotTimeline(m.buckets[:], now),
		Runtime:  snapshotRuntime(),
		LLM:      snapshotLLM(m.llm),
	}

	if m.total > 0 {
		snapshot.Requests.ErrorRate = float64(m.clientError+m.serverError) / float64(m.total) * 100
	}
	for _, bucket := range snapshot.Timeline {
		snapshot.Requests.RequestsPerMinute += float64(bucket.Total)
	}

	return snapshot
}

func snapshotLatency(samples []time.Duration, total int64) domain.LatencyMetrics {
	count := int(total)
	if count > len(samples) {
		count = len(samples)
	}
	if count == 0 {
		return domain.LatencyMetrics{}
	}

	sorted := make([]time.Duration, count)
	copy(sorted, samples[:count])
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })

	var sum time.Duration
	for _, sample := range sorted {
		sum += sample
	}

	return domain.LatencyMetrics{
		Samples: count,
		Min:     sorted[0],
		Max:     sorted[count-1],
		Avg:     sum / time.Duration(count),
		P50:     percentile(sorted, 0.50),
		P95:     percentile(sorted, 0.95),
		P99:     percentile(sorted, 0.99),
	}
}

// percentile applique le rang le plus proche (nearest-rank) sur un echantillon
// deja trie : aucun echantillon n'est interpole, donc p99 est toujours une
// latence reellement observee.
func percentile(sorted []time.Duration, q float64) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	index := int(math.Ceil(q*float64(len(sorted)))) - 1
	if index < 0 {
		index = 0
	}
	if index >= len(sorted) {
		index = len(sorted) - 1
	}
	return sorted[index]
}

func snapshotRoutes(routes map[string]*routeAccumulator) []domain.RouteMetric {
	out := make([]domain.RouteMetric, 0, len(routes))
	for _, route := range routes {
		average := time.Duration(0)
		if route.count > 0 {
			average = route.totalLatency / time.Duration(route.count)
		}
		out = append(out, domain.RouteMetric{
			Method:      route.method,
			Path:        route.path,
			Count:       route.count,
			Errors:      route.errors,
			AvgLatency:  average,
			LastLatency: route.lastLatency,
			LastStatus:  route.lastStatus,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count == out[j].Count {
			return out[i].Path < out[j].Path
		}
		return out[i].Count > out[j].Count
	})
	return out
}

func snapshotErrors(errors []domain.ErrorMetric) []domain.ErrorMetric {
	if len(errors) == 0 {
		return nil
	}
	out := make([]domain.ErrorMetric, len(errors))
	for i, event := range errors {
		out[len(errors)-1-i] = event // plus recent d'abord
	}
	return out
}

func snapshotTimeline(buckets []timelineSlot, now time.Time) []domain.TimelineBucket {
	out := make([]domain.TimelineBucket, 0, metricsTimelineSeconds)
	nowSec := now.Unix()
	for i := metricsTimelineSeconds - 1; i >= 0; i-- {
		sec := nowSec - int64(i)
		moment := time.Unix(sec, 0)
		bucket := domain.TimelineBucket{
			Start: moment,
			Label: moment.Format("15:04:05"),
		}
		slot := buckets[sec%metricsTimelineSeconds]
		if slot.sec == sec {
			bucket.Total = slot.total
			bucket.Errors = slot.errors
		}
		out = append(out, bucket)
	}
	return out
}

func snapshotRuntime() domain.RuntimeMetrics {
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)

	metrics := domain.RuntimeMetrics{
		Goroutines:  runtime.NumGoroutine(),
		HeapAllocMB: float64(mem.HeapAlloc) / (1 << 20),
		HeapSysMB:   float64(mem.HeapSys) / (1 << 20),
		StackMB:     float64(mem.StackInuse) / (1 << 20),
		NumGC:       mem.NumGC,
		GOMAXPROCS:  runtime.GOMAXPROCS(0),
		NumCPU:      runtime.NumCPU(),
		GoVersion:   runtime.Version(),
		OS:          runtime.GOOS,
		Arch:        runtime.GOARCH,
	}
	if mem.LastGC > 0 {
		metrics.LastGC = time.Unix(0, int64(mem.LastGC))
	}
	return metrics
}

func snapshotLLM(acc llmAccumulator) domain.LLMMetrics {
	metrics := domain.LLMMetrics{
		Calls:       acc.calls,
		Errors:      acc.errors,
		Streams:     acc.streams,
		InFlight:    acc.inFlight,
		LastLatency: acc.lastLatency,
	}
	if acc.calls > 0 {
		metrics.AvgLatency = acc.totalLatency / time.Duration(acc.calls)
	}

	for _, entry := range acc.models {
		model := domain.LLMModelMetric{
			Provider:    entry.provider,
			Model:       entry.model,
			Calls:       entry.calls,
			Errors:      entry.errors,
			LastLatency: entry.lastLatency,
		}
		if entry.calls > 0 {
			model.AvgLatency = entry.totalLatency / time.Duration(entry.calls)
		}
		metrics.Models = append(metrics.Models, model)
	}
	sort.Slice(metrics.Models, func(i, j int) bool {
		if metrics.Models[i].Calls == metrics.Models[j].Calls {
			return metrics.Models[i].Model < metrics.Models[j].Model
		}
		return metrics.Models[i].Calls > metrics.Models[j].Calls
	})
	return metrics
}

// metricsResponseWriter capture le statut et le volume de la reponse sans
// retirer les capacites du writer d'origine : SSE a besoin du Flusher, et
// http.NewResponseController remonte la chaine via Unwrap.
