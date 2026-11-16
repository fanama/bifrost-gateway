package metrics

import (
	"testing"
	"time"

	"bridge-gateway/domain"
)

func TestBrokerFansOutAndCoalesces(t *testing.T) {
	hub := newBroker()
	first, cancelFirst := hub.subscribe()
	defer cancelFirst()
	second, cancelSecond := hub.subscribe()
	defer cancelSecond()

	hub.publish(domain.MetricsSnapshot{Requests: domain.RequestMetrics{Total: 1}})
	// Le deuxieme envoi remplace la trame encore en boite : un lecteur lent
	// reprend sur l'etat le plus recent, sans rattrapage.
	hub.publish(domain.MetricsSnapshot{Requests: domain.RequestMetrics{Total: 2}})

	feeds := map[string]<-chan domain.MetricsSnapshot{"first": first, "second": second}
	for name, feed := range feeds {
		select {
		case snapshot := <-feed:
			if got := snapshot.Requests.Total; got != 2 {
				t.Fatalf("%s subscriber got total %d, want 2", name, got)
			}
		case <-time.After(time.Second):
			t.Fatalf("%s subscriber received no snapshot", name)
		}
	}
}

func TestBrokerUnsubscribeStopsDelivery(t *testing.T) {
	hub := newBroker()
	feed, cancel := hub.subscribe()

	if !hub.hasSubscribers() {
		t.Fatal("subscription was not registered")
	}
	cancel()
	cancel() // idempotent : un handler peut annuler deux fois.
	if hub.hasSubscribers() {
		t.Fatal("cancelled subscription is still registered")
	}

	hub.publish(domain.MetricsSnapshot{})
	select {
	case snapshot := <-feed:
		t.Fatalf("cancelled subscriber still received %+v", snapshot)
	default:
	}
}

func TestCollectorPublishesToSubscribers(t *testing.T) {
	collector := NewMetricsCollector()
	feed, cancel := collector.Subscribe()
	defer cancel()

	collector.RecordLLMLog(domain.CallLog{Provider: "ollama", Model: "gemma"})

	select {
	case snapshot := <-feed:
		if snapshot.GeneratedAt.IsZero() {
			t.Fatal("published snapshot was not generated")
		}
	case <-time.After(time.Second):
		t.Fatal("no snapshot was published to the subscriber")
	}
}

func TestCollectorDoesNotPublishWithoutSubscribers(t *testing.T) {
	collector := NewMetricsCollector()
	// Aucun abonne : la publication est un no-op, sans calcul d'instantane.
	collector.publishSnapshot()

	if got := collector.Snapshot().Requests.Total; got != 0 {
		t.Fatalf("total = %d, want 0", got)
	}
}
