package relay

import (
	"context"
	"sync"
)

type identityGate struct {
	token chan struct{}
	users int
}

// identityGates serializes only identity operations for the same device. Entries
// exist while owned or awaited and are reclaimed even when a waiter is cancelled.
type identityGates struct {
	mu      sync.Mutex
	entries map[string]*identityGate
}

func (g *identityGates) acquire(ctx context.Context, id string) (func(), error) {
	g.mu.Lock()
	if g.entries == nil {
		g.entries = make(map[string]*identityGate)
	}
	entry := g.entries[id]
	if entry == nil {
		entry = &identityGate{token: make(chan struct{}, 1)}
		entry.token <- struct{}{}
		g.entries[id] = entry
	}
	entry.users++
	g.mu.Unlock()
	select {
	case <-ctx.Done():
		g.release(id, entry)
		return nil, ctx.Err()
	case <-entry.token:
		if err := ctx.Err(); err != nil {
			entry.token <- struct{}{}
			g.release(id, entry)
			return nil, err
		}
		var once sync.Once
		return func() { once.Do(func() { entry.token <- struct{}{}; g.release(id, entry) }) }, nil
	}
}

func (g *identityGates) release(id string, entry *identityGate) {
	g.mu.Lock()
	defer g.mu.Unlock()
	entry.users--
	if entry.users == 0 {
		delete(g.entries, id)
	}
}
