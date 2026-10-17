package metrics

import (
	"sync"
	"time"

	"bridge-gateway/domain"
)

const (
	// metricsLatencySamples borne la memoire des percentiles : les N dernieres
	// requetes, pas toute la vie du processus.
	metricsLatencySamples = 2048
	// metricsErrorHistory borne le journal des erreurs affiche dans l'UI.
	metricsErrorHistory = 25
	// metricsTimelineSeconds est la largeur du graphique de trafic.
	metricsTimelineSeconds = 60
	// metricsMaxRoutes borne la carte des routes : une URL inconnue aleatoire
	// ne doit pas faire croitre la memoire indefiniment. Au-dela, les routes
	// inedites sont agregees sous "(autres)".
	metricsMaxRoutes = 200
)

// timelineSlot est le compteur d'une seconde. La seconde est stockee avec la
// valeur pour que l'anneau se reinitialise sans balayage : une case dont la
// seconde ne correspond plus est simplement consideree comme vide.
type timelineSlot struct {
	sec    int64
	total  int64
	errors int64
}

type routeAccumulator struct {
	method       string
	path         string
	count        int64
	errors       int64
	totalLatency time.Duration
	lastLatency  time.Duration
	lastStatus   int
}

type llmModelAccumulator struct {
	provider     string
	model        string
	calls        int64
	errors       int64
	totalLatency time.Duration
	lastLatency  time.Duration
}

type llmAccumulator struct {
	calls        int64
	errors       int64
	streams      int64
	inFlight     int64
	totalLatency time.Duration
	lastLatency  time.Duration
	models       map[string]*llmModelAccumulator
}

// MetricsCollector agrege en memoire les metriques de la passerelle : trafic
// HTTP, latences, erreurs recentes, sante du processus et appels LLM.
//
// Aucun service externe : les compteurs vivent dans le processus, proteges par
// un mutex, et disparaissent au redemarrage. C'est le compromis assume d'un
// monitoring "from scratch" — suffisant pour un tableau de bord et un endpoint
// JSON, sans dependance Prometheus/OpenTelemetry.
type MetricsCollector struct {
	startedAt time.Time
	now       func() time.Time

	mu sync.Mutex

	inFlight int64

	total       int64
	success     int64
	redirects   int64
	clientError int64
	serverError int64

	latencies    [metricsLatencySamples]time.Duration
	latencyCount int64

	routes map[string]*routeAccumulator

	errors  []domain.ErrorMetric
	buckets [metricsTimelineSeconds]timelineSlot

	llm llmAccumulator
}

// NewMetricsCollector cree un collecteur dont l'uptime demarre maintenant.
func NewMetricsCollector() *MetricsCollector {
	now := time.Now
	return &MetricsCollector{
		startedAt: now(),
		now:       now,
		routes:    make(map[string]*routeAccumulator),
		llm: llmAccumulator{
			models: make(map[string]*llmModelAccumulator),
		},
	}
}

// Middleware mesure chaque requete HTTP et capture les paniques.
//
// Une panique est journalisee avec sa pile et rendue en 500 : sans cela,
// net/http ferme la connexion sans reponse et l'incident n'apparait nulle part
// dans le tableau de bord. http.ErrAbortHandler est laisse remonter — c'est le
// signal standard d'abandon cote net/http, pas une panne.
