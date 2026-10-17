package infrastructure

import (
	"bridge-gateway/domain"
	"bridge-gateway/infrastructure/metrics"
)

type MetricsCollector = metrics.MetricsCollector

func NewMetricsCollector() *MetricsCollector { return metrics.NewMetricsCollector() }

func NewMeasuredLLMProvider(inner domain.LLMProvider, m *MetricsCollector) *metrics.MeasuredLLMProvider {
	return metrics.NewMeasuredLLMProvider(inner, m)
}
