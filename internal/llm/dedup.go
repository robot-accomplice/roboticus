package llm

import (
	"context"
	"sync"
	"time"
)

// Dedup prevents duplicate in-flight requests. If two identical requests
// arrive within a short window, the second one waits for the first's result
// instead of hitting the provider again. This is cheaper and faster than
// cache because it handles the exact moment of concurrent duplicate calls.
type Dedup struct {
	mu       sync.Mutex
	inflight map[string]*inflightEntry
	ttl      time.Duration
}

type inflightEntry struct {
	done     chan struct{}
	response *Response
	err      error
}

// NewDedup creates a dedup tracker.
func NewDedup(ttl time.Duration) *Dedup {
	return &Dedup{
		inflight: make(map[string]*inflightEntry),
		ttl:      ttl,
	}
}

// Do executes fn only if no identical request is already in-flight.
// Concurrent callers with the same key share the result.
func (d *Dedup) Do(ctx context.Context, key string, fn func() (*Response, error)) (*Response, error) {
	d.mu.Lock()
	if entry, ok := d.inflight[key]; ok {
		d.mu.Unlock()
		// Wait for the in-flight request to complete.
		select {
		case <-entry.done:
			return entry.response, entry.err
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	entry := &inflightEntry{done: make(chan struct{})}
	d.inflight[key] = entry
	d.mu.Unlock()

	// Execute the actual request.
	entry.response, entry.err = fn()
	close(entry.done)

	// Clean up after TTL (allow brief reuse of result for near-simultaneous calls).
	go func() {
		select {
		case <-time.After(d.ttl):
		case <-ctx.Done():
		}
		d.mu.Lock()
		delete(d.inflight, key)
		d.mu.Unlock()
	}()

	return entry.response, entry.err
}
