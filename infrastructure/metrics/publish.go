package metrics

import (
	"time"

	"bridge-gateway/domain"
)

const (
	// metricsPublishInterval plafonne la cadence de publication : quel que
	// soit le trafic, un instantane n'est calcule qu'au plus deux fois par
	// seconde. Le hub coalesce ensuite les trames par abonne.
	metricsPublishInterval = 500 * time.Millisecond
)

// Subscribe ouvre un flux d'instantanes pour un client SSE. La fonction rendue
// libere l'abonnement : elle doit etre appelee a la deconnexion du client.
func (m *MetricsCollector) Subscribe() (<-chan domain.MetricsSnapshot, func()) {
	return m.broker.subscribe()
}

// publishSnapshot diffuse l'instantane courant aux abonnes. Le calcul est
// differe quand la cadence est atteinte, et saute quand personne n'ecoute :
// le chemin des requetes ne paie pas le rendu d'un tableau de bord ferme.
func (m *MetricsCollector) publishSnapshot() {
	if !m.broker.hasSubscribers() {
		return
	}

	m.mu.Lock()
	now := m.now()
	due := now.Sub(m.lastPublish) >= metricsPublishInterval
	if due {
		m.lastPublish = now
	}
	m.mu.Unlock()

	if due {
		m.broker.publish(m.Snapshot())
	}
}
