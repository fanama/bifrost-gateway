package metrics

import (
	"sync"

	"bridge-gateway/domain"
)

// broker is the in-process pub/sub fan-out behind the SSE monitoring feed.
//
// Publishing must never block a request handler, so every subscriber owns a
// one-slot mailbox: a slow client keeps only the freshest snapshot instead of
// building a backlog. The broker knows nothing about HTTP or SSE framing, only
// about snapshots and subscriptions.
type broker struct {
	mu          sync.Mutex
	nextID      uint64
	subscribers map[uint64]chan domain.MetricsSnapshot
}

func newBroker() *broker {
	return &broker{subscribers: make(map[uint64]chan domain.MetricsSnapshot)}
}

// subscribe registers one feed. The returned cancel function is idempotent and
// must be called when the client disconnects, otherwise the subscriber would
// keep receiving frames nobody reads.
func (b *broker) subscribe() (<-chan domain.MetricsSnapshot, func()) {
	b.mu.Lock()
	defer b.mu.Unlock()

	id := b.nextID
	b.nextID++
	feed := make(chan domain.MetricsSnapshot, 1)
	b.subscribers[id] = feed

	var once sync.Once
	cancel := func() {
		once.Do(func() {
			b.mu.Lock()
			delete(b.subscribers, id)
			b.mu.Unlock()
		})
	}
	return feed, cancel
}

func (b *broker) hasSubscribers() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.subscribers) > 0
}

// publish delivers the snapshot to every live subscriber. It is called from the
// request path, so it never waits for a reader.
func (b *broker) publish(snapshot domain.MetricsSnapshot) {
	b.mu.Lock()
	defer b.mu.Unlock()

	for _, feed := range b.subscribers {
		sendLatest(feed, snapshot)
	}
}

// sendLatest writes without blocking. When the mailbox still holds a frame, it
// is discarded first so the reader resumes on the newest data.
func sendLatest(feed chan domain.MetricsSnapshot, snapshot domain.MetricsSnapshot) {
	select {
	case feed <- snapshot:
		return
	default:
	}
	select {
	case <-feed:
	default:
	}
	select {
	case feed <- snapshot:
	default:
	}
}
