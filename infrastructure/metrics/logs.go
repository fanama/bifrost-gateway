package metrics

import "bridge-gateway/domain"

const (
	// metricsLogHistory borne le journal des appels LLM : les N dernieres
	// entrees, en memoire seulement (le journal disparait au redemarrage,
	// comme les autres compteurs).
	metricsLogHistory = 100
	// metricsLogMaxField borne la taille d'un champ. Un prompt ou une reponse
	// enorme ne doit pas faire croitre la memoire sans limite.
	metricsLogMaxField = 4000
)

// RecordLLMLog ajoute un appel au journal. Les champs texte sont tronques,
// puis l'entree la plus ancienne est oubliee quand la borne est atteinte.
func (m *MetricsCollector) RecordLLMLog(entry domain.CallLog) {
	entry.Input = truncateField(entry.Input)
	entry.Output = truncateField(entry.Output)

	m.mu.Lock()
	defer m.mu.Unlock()

	m.logs = append(m.logs, entry)
	if len(m.logs) > metricsLogHistory {
		m.logs = m.logs[len(m.logs)-metricsLogHistory:]
	}
}

// Logs rend une copie du journal, le plus recent d'abord. La copie est faite
// sous verrou : l'appelant ne partage jamais l'etat interne du collecteur.
func (m *MetricsCollector) Logs() []domain.CallLog {
	m.mu.Lock()
	defer m.mu.Unlock()

	if len(m.logs) == 0 {
		return nil
	}
	out := make([]domain.CallLog, len(m.logs))
	for i, entry := range m.logs {
		out[len(m.logs)-1-i] = entry
	}
	return out
}

// truncateField coupe sur une frontiere de rune : une coupe au milieu d'un
// caractere multi-octets produirait un export invalide.
func truncateField(value string) string {
	if len(value) <= metricsLogMaxField {
		return value
	}
	runes := []rune(value)
	if len(runes) <= metricsLogMaxField {
		return value
	}
	return string(runes[:metricsLogMaxField]) + "\n… (tronqué)"
}
