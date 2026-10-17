package infrastructure

import (
	"context"
	"fmt"

	"bridge-gateway/domain"
)

// ChatStream replays the routing ladder for streaming responses. Failover is
// only allowed before the first chunk is emitted to the caller.
func (p *RoutingLLMProvider) ChatStream(ctx context.Context, cfg *domain.ChatConfig, messages []domain.ChatMessage) (<-chan domain.StreamEvent, error) {
	ladder := p.ladder(ctx, cfg, messages)
	if len(ladder) == 0 {
		return domain.StreamFrom(ctx, p.inner, cfg, messages)
	}

	var (
		attempts []string
		lastErr  error
	)
	for i, target := range ladder {
		if p.maxAttempts > 0 && i >= p.maxAttempts {
			break
		}
		stream, done, err := p.tryStreamTarget(ctx, cfg, target, messages, &attempts, &lastErr)
		if done {
			return stream, err
		}
	}

	if lastErr == nil {
		lastErr = fmt.Errorf("no routing candidate was callable")
	}
	return nil, p.aggregateError(cfg, attempts, lastErr)
}

func (p *RoutingLLMProvider) tryStreamTarget(
	ctx context.Context,
	cfg *domain.ChatConfig,
	target *domain.ChatConfig,
	messages []domain.ChatMessage,
	attempts *[]string,
	lastErr *error,
) (<-chan domain.StreamEvent, bool, error) {
	merged := p.applyTier(ctx, cfg, target)
	if merged == nil {
		*attempts = append(*attempts, fmt.Sprintf("%s/%s (identifiants manquants)", target.Provider, target.Model))
		return nil, false, nil
	}

	source, err := domain.StreamFrom(ctx, p.inner, merged, messages)
	if err == nil {
		first, ok, waitErr := firstEvent(ctx, source)
		if ok {
			if len(*attempts) > 0 {
				logRoutingFailover(cfg.Model, first.Model, *attempts)
			}
			return p.pump(ctx, source, first), true, nil
		}
		if waitErr != nil {
			err = waitErr
		} else {
			err = fmt.Errorf("stream closed before any content")
		}
	}

	if p.recordAttemptFailure(ctx, target, err, attempts, lastErr) {
		if ctx.Err() != nil {
			return nil, true, p.aggregateError(cfg, *attempts, *lastErr)
		}
		return nil, true, p.aggregateError(cfg, *attempts, err)
	}
	return nil, false, nil
}

func firstEvent(ctx context.Context, source <-chan domain.StreamEvent) (domain.StreamEvent, bool, error) {
	select {
	case <-ctx.Done():
		return domain.StreamEvent{}, false, ctx.Err()
	case event, ok := <-source:
		if !ok {
			return domain.StreamEvent{}, false, nil
		}
		return event, true, nil
	}
}

func (p *RoutingLLMProvider) pump(ctx context.Context, source <-chan domain.StreamEvent, first domain.StreamEvent) <-chan domain.StreamEvent {
	out := make(chan domain.StreamEvent)
	go func() {
		defer close(out)
		relayStreamEvents(ctx, source, out, first)
	}()
	return out
}

func relayStreamEvents(ctx context.Context, source <-chan domain.StreamEvent, out chan<- domain.StreamEvent, first domain.StreamEvent) {
	pending := first
	for {
		select {
		case <-ctx.Done():
			return
		case out <- pending:
		}
		if pending.Done || pending.Err != nil {
			return
		}
		select {
		case <-ctx.Done():
			return
		case event, ok := <-source:
			if !ok {
				return
			}
			pending = event
		}
	}
}

var _ domain.StreamingLLMProvider = (*RoutingLLMProvider)(nil)
