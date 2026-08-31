package web

import "sync"

// Broadcaster is a fan-out of "something changed" pings to any number of SSE
// subscribers. Non-blocking: a subscriber that is behind simply coalesces the
// pending pings into one.
type Broadcaster struct {
	mu   sync.Mutex
	subs map[chan struct{}]struct{}
}

// NewBroadcaster returns a ready Broadcaster.
func NewBroadcaster() *Broadcaster {
	return &Broadcaster{subs: make(map[chan struct{}]struct{})}
}

// Notify wakes every current subscriber. Safe to call from any goroutine.
func (b *Broadcaster) Notify() {
	if b == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	for ch := range b.subs {
		select {
		case ch <- struct{}{}:
		default: // subscriber hasn't drained the last ping yet
		}
	}
}

func (b *Broadcaster) subscribe() (<-chan struct{}, func()) {
	ch := make(chan struct{}, 1)
	b.mu.Lock()
	b.subs[ch] = struct{}{}
	b.mu.Unlock()
	return ch, func() {
		b.mu.Lock()
		delete(b.subs, ch)
		b.mu.Unlock()
	}
}
