package web

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"bridge-gateway/domain"
)

const (
	// sseHeartbeatInterval garde la connexion vivante derriere un proxy qui
	// fermerait un flux muet.
	sseHeartbeatInterval = 15 * time.Second
	// sseRefreshInterval re-rend le tableau de bord quand aucune requete n'a
	// eu lieu : la timeline et l'uptime continuent d'avancer sur une
	// passerelle inactive.
	sseRefreshInterval = 3 * time.Second
)

// events diffuse le tableau de bord en SSE : une trame initiale, puis chaque
// instantane publie par le collecteur (coalesce par le hub) et un rafraichissement
// periodique. Le rendu reste cote serveur ; le client remplace le fragment.
func (s *monitoringController) events(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	prepareSSE(w)
	if !s.writeDashboardEvent(w, flusher, s.snapshot()) {
		return
	}

	feed, cancel := s.subscribe()
	defer cancel()

	heartbeat := time.NewTicker(sseHeartbeatInterval)
	defer heartbeat.Stop()
	refresh := time.NewTicker(sseRefreshInterval)
	defer refresh.Stop()

	s.streamLoop(w, flusher, r, feed, refresh.C, heartbeat.C)
}

// streamLoop sert la connexion jusqu'a la deconnexion du client ou une erreur
// d'ecriture. Les trames de donnees et les commentaires de keep-alive
// partagent le meme flux.
func (s *monitoringController) streamLoop(
	w http.ResponseWriter,
	flusher http.Flusher,
	r *http.Request,
	feed <-chan domain.MetricsSnapshot,
	refresh <-chan time.Time,
	heartbeat <-chan time.Time,
) {
	for {
		select {
		case <-r.Context().Done():
			return
		case snapshot, ok := <-feed:
			if !ok || !s.writeDashboardEvent(w, flusher, snapshot) {
				return
			}
		case <-refresh:
			if !s.writeDashboardEvent(w, flusher, s.snapshot()) {
				return
			}
		case <-heartbeat:
			if !writeSSEComment(w, flusher, "keep-alive") {
				return
			}
		}
	}
}

// prepareSSE pose les en-tetes du flux et retire l'echeance d'ecriture du
// serveur pour cette connexion : un flux de supervision vit plus longtemps que
// le WriteTimeout global. ResponseController traverse l'enveloppe de metriques.
func prepareSSE(w http.ResponseWriter) {
	header := w.Header()
	header.Set("Content-Type", "text/event-stream")
	header.Set("Cache-Control", "no-store")
	header.Set("Connection", "keep-alive")
	header.Set("X-Accel-Buffering", "no")
	_ = http.NewResponseController(w).SetWriteDeadline(time.Time{})
	w.WriteHeader(http.StatusOK)
}

// writeDashboardEvent rend le fragment et l'encadre en un evenement SSE. Le
// HTML multi-lignes est decoupe ligne par ligne (chaque ligne garde son prefixe
// "data: ") ; le navigateur les recompose avec des sauts de ligne.
func (s *monitoringController) writeDashboardEvent(w io.Writer, flusher http.Flusher, snapshot domain.MetricsSnapshot) bool {
	var fragment bytes.Buffer
	if err := s.templates.ExecuteTemplate(&fragment, "monitoring_dashboard", newMonitoringView(snapshot)); err != nil {
		log.Printf("monitoring sse: render: %v", err)
		return false
	}
	if _, err := io.WriteString(w, "event: dashboard\n"); err != nil {
		return false
	}
	for _, line := range strings.Split(fragment.String(), "\n") {
		if _, err := fmt.Fprintf(w, "data: %s\n", line); err != nil {
			return false
		}
	}
	if _, err := io.WriteString(w, "\n"); err != nil {
		return false
	}
	flusher.Flush()
	return true
}

func writeSSEComment(w io.Writer, flusher http.Flusher, text string) bool {
	if _, err := fmt.Fprintf(w, ": %s\n\n", text); err != nil {
		return false
	}
	flusher.Flush()
	return true
}
