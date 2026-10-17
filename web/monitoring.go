package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"bridge-gateway/domain"
)

// monitoringController expose le tableau de bord de supervision : page HTMX
// auto-rafraichie, fragment de mise a jour et snapshot JSON pour un outil
// externe. Il ne connait que le port metricsService (voir ports.go).
type monitoringController struct {
	uiShared
	metrics metricsService
}

// monitoringView est le modele de la page et du fragment. Il embarque le
// snapshot pour que les templates accedent directement a .Requests, .Latency,
// etc., et ajoute les valeurs de presentation (hauteur des barres).
type monitoringView struct {
	Title  string
	Active string
	domain.MetricsSnapshot
	TimelineMax int64
}

func (s *monitoringController) pageMonitoring(w http.ResponseWriter, r *http.Request) {
	s.render(w, "page_monitoring", s.view())
}

func (s *monitoringController) stats(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	s.render(w, "monitoring_dashboard", s.view())
}

func (s *monitoringController) metricsJSON(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	// Un snapshot doit toujours etre frais : aucun cache intermediaire.
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(s.snapshot())
}

func (s *monitoringController) view() monitoringView {
	snapshot := s.snapshot()
	view := monitoringView{
		Title:           "Monitoring",
		Active:          "monitoring",
		MetricsSnapshot: snapshot,
	}
	for _, bucket := range snapshot.Timeline {
		if bucket.Total > view.TimelineMax {
			view.TimelineMax = bucket.Total
		}
	}
	return view
}

func (s *monitoringController) snapshot() domain.MetricsSnapshot {
	if s.metrics == nil {
		return domain.MetricsSnapshot{}
	}
	return s.metrics.Snapshot()
}

// ---- Helpers de template (enregistres dans NewServer) ----

// formatDuration rend une duree lisible en gardant une precision utile a
// l'echelle de la requete (ns, µs, ms) puis a celle de l'uptime.
func formatDuration(d time.Duration) string {
	if d <= 0 {
		return "0s"
	}
	switch {
	case d < time.Microsecond:
		return fmt.Sprintf("%dns", d.Nanoseconds())
	case d < time.Millisecond:
		return fmt.Sprintf("%.1fµs", float64(d)/float64(time.Microsecond))
	case d < time.Second:
		return fmt.Sprintf("%.1fms", float64(d)/float64(time.Millisecond))
	case d < time.Minute:
		return fmt.Sprintf("%.2fs", d.Seconds())
	case d < time.Hour:
		return fmt.Sprintf("%dm%02ds", int(d.Minutes()), int(d.Seconds())%60)
	default:
		return fmt.Sprintf("%dh%02dm", int(d.Hours()), int(d.Minutes())%60)
	}
}

// formatAgo rend l'age d'un evenement du journal d'erreurs.
func formatAgo(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	d := time.Since(t)
	if d < 0 {
		d = 0
	}
	switch {
	case d < time.Second:
		return "à l'instant"
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	default:
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
}

// barHeight convertit un compteur en pourcentage de hauteur. Une valeur non
// nulle garde une barre visible (4 %), sinon un pic isole ecraserait tout le
// graphique a une hauteur nulle.
func barHeight(value, max int64) int {
	if value <= 0 || max <= 0 {
		return 0
	}
	height := int(float64(value) / float64(max) * 100)
	if height < 4 {
		height = 4
	}
	if height > 100 {
		height = 100
	}
	return height
}
