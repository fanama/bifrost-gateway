package metrics

import "time"

func (m *MetricsCollector) BeginLLM() {
	m.mu.Lock()
	m.llm.inFlight++
	m.mu.Unlock()
}

// RecordLLM clot un appel provider et l'attribue au couple provider/modele. Le
// cache de reponses est place au-dessus : un hit n'arrive jamais ici.
func (m *MetricsCollector) RecordLLM(provider, model string, duration time.Duration, err error, stream bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.llm.inFlight > 0 {
		m.llm.inFlight--
	}
	m.llm.calls++
	if stream {
		m.llm.streams++
	}
	if err != nil {
		m.llm.errors++
	}
	m.llm.totalLatency += duration
	m.llm.lastLatency = duration

	key := provider + "\x00" + model
	entry := m.llm.models[key]
	if entry == nil {
		entry = &llmModelAccumulator{provider: provider, model: model}
		m.llm.models[key] = entry
	}
	entry.calls++
	entry.totalLatency += duration
	entry.lastLatency = duration
	if err != nil {
		entry.errors++
	}
}

// Snapshot rend une copie coherente des compteurs. La copie est faite sous
// verrou : l'appelant ne partage jamais l'etat interne du collecteur.
