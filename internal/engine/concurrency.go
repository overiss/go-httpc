package engine

import "context"

// concurrencyLimiter caps in-flight requests for one client instance.
type concurrencyLimiter struct {
	slots chan struct{}
}

func newConcurrencyLimiter(max int) *concurrencyLimiter {
	if max <= 0 {
		return nil
	}
	return &concurrencyLimiter{slots: make(chan struct{}, max)}
}

func (l *concurrencyLimiter) acquire(ctx context.Context) error {
	if l == nil {
		return nil
	}
	select {
	case l.slots <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (l *concurrencyLimiter) release() {
	if l == nil {
		return
	}
	<-l.slots
}
