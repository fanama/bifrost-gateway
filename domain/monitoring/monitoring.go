package monitoring

import "time"

// MetricsSnapshot est une photographie immuable des compteurs de la passerelle.
//
// Le domaine ne fixe que la forme des donnees : aucun type de framework ni
// d'infrastructure n'y apparait. Le collecteur (adaptateur) la produit, l'UI
// HTMX et l'endpoint JSON la consomment, et un autre backend de metriques
// pourrait la produire sans toucher a la presentation.
type MetricsSnapshot struct {
	StartedAt   time.Time        `json:"started_at"`
	GeneratedAt time.Time        `json:"generated_at"`
	Uptime      time.Duration    `json:"uptime_ns"`
	Requests    RequestMetrics   `json:"requests"`
	Latency     LatencyMetrics   `json:"latency"`
	Statuses    []StatusMetric   `json:"statuses"`
	Routes      []RouteMetric    `json:"routes"`
	Errors      []ErrorMetric    `json:"errors"`
	Timeline    []TimelineBucket `json:"timeline"`
	Runtime     RuntimeMetrics   `json:"runtime"`
	LLM         LLMMetrics       `json:"llm"`
}

// RequestMetrics resume le trafic HTTP depuis le demarrage.
type RequestMetrics struct {
	Total             int64   `json:"total"`
	InFlight          int64   `json:"in_flight"`
	Success2xx        int64   `json:"success_2xx"`
	Redirects3xx      int64   `json:"redirects_3xx"`
	ClientErrors4xx   int64   `json:"client_errors_4xx"`
	ServerErrors5xx   int64   `json:"server_errors_5xx"`
	ErrorRate         float64 `json:"error_rate_percent"`
	RequestsPerMinute float64 `json:"requests_per_minute"`
}

// LatencyMetrics est calculee sur un anneau des dernieres requetes : les
// percentiles refletent donc l'activite recente, pas toute la vie du process.
type LatencyMetrics struct {
	Samples int           `json:"samples"`
	Min     time.Duration `json:"min_ns"`
	Avg     time.Duration `json:"avg_ns"`
	P50     time.Duration `json:"p50_ns"`
	P95     time.Duration `json:"p95_ns"`
	P99     time.Duration `json:"p99_ns"`
	Max     time.Duration `json:"max_ns"`
}

// StatusMetric compte les reponses par classe (2xx, 3xx, 4xx, 5xx).
type StatusMetric struct {
	Label string `json:"label"`
	Count int64  `json:"count"`
}

// RouteMetric agrege les requetes par route HTTP. Path est le motif du mux
// (par exemple /models/{id}), pas l'URL brute : les identifiants ne creent donc
// pas une entree par appel.
type RouteMetric struct {
	Method      string        `json:"method"`
	Path        string        `json:"path"`
	Count       int64         `json:"count"`
	Errors      int64         `json:"errors"`
	AvgLatency  time.Duration `json:"avg_latency_ns"`
	LastLatency time.Duration `json:"last_latency_ns"`
	LastStatus  int           `json:"last_status"`
}

// ErrorMetric est une reponse HTTP en erreur (4xx ou 5xx), conservee dans un
// journal circulaire borne.
type ErrorMetric struct {
	Time    time.Time     `json:"time"`
	Method  string        `json:"method"`
	Path    string        `json:"path"`
	Status  int           `json:"status"`
	Latency time.Duration `json:"latency_ns"`
	Message string        `json:"message"`
}

// TimelineBucket est une seconde d'activite, utilisee pour le graphique.
type TimelineBucket struct {
	Start  time.Time `json:"start"`
	Label  string    `json:"label"`
	Total  int64     `json:"total"`
	Errors int64     `json:"errors"`
}

// RuntimeMetrics expose la sante du processus Go (aucune dependance externe).
type RuntimeMetrics struct {
	Goroutines  int       `json:"goroutines"`
	HeapAllocMB float64   `json:"heap_alloc_mb"`
	HeapSysMB   float64   `json:"heap_sys_mb"`
	StackMB     float64   `json:"stack_mb"`
	NumGC       uint32    `json:"num_gc"`
	LastGC      time.Time `json:"last_gc"`
	GOMAXPROCS  int       `json:"gomaxprocs"`
	NumCPU      int       `json:"num_cpu"`
	GoVersion   string    `json:"go_version"`
	OS          string    `json:"os"`
	Arch        string    `json:"arch"`
}

// LLMMetrics resume les appels reellement envoyes a un provider (ou au routeur
// local), cache de reponses exclu.
type LLMMetrics struct {
	Calls       int64            `json:"calls"`
	Errors      int64            `json:"errors"`
	Streams     int64            `json:"streams"`
	InFlight    int64            `json:"in_flight"`
	AvgLatency  time.Duration    `json:"avg_latency_ns"`
	LastLatency time.Duration    `json:"last_latency_ns"`
	Models      []LLMModelMetric `json:"models"`
}

// LLMModelMetric detaille les compteurs par couple provider/modele.
type LLMModelMetric struct {
	Provider    string        `json:"provider"`
	Model       string        `json:"model"`
	Calls       int64         `json:"calls"`
	Errors      int64         `json:"errors"`
	AvgLatency  time.Duration `json:"avg_latency_ns"`
	LastLatency time.Duration `json:"last_latency_ns"`
}
