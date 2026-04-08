package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"
)

// TicketStore manages single-use, short-lived WebSocket authentication tickets.
type TicketStore struct {
	mu      sync.Mutex
	tickets map[string]time.Time // token → expiry
	ttl     time.Duration
}

// NewTicketStore creates a ticket store with the given TTL.
// The background cleanup goroutine exits when ctx is cancelled.
func NewTicketStore(ctx context.Context, ttl time.Duration) *TicketStore {
	ts := &TicketStore{
		tickets: make(map[string]time.Time),
		ttl:     ttl,
	}
	// Background cleanup — exits when context cancels.
	go func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				ts.cleanup()
			case <-ctx.Done():
				return
			}
		}
	}()
	return ts
}

// Issue generates a new single-use ticket.
func (ts *TicketStore) Issue() string {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	b := make([]byte, 32) // 256-bit entropy for stronger ticket security
	_, _ = rand.Read(b)
	token := "wst_" + hex.EncodeToString(b)
	ts.tickets[token] = time.Now().Add(ts.ttl)
	return token
}

// Validate checks and consumes a ticket. Returns true if valid.
func (ts *TicketStore) Validate(ticket string) bool {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	expiry, ok := ts.tickets[ticket]
	if !ok {
		return false
	}
	delete(ts.tickets, ticket) // one-time use
	return time.Now().Before(expiry)
}

func (ts *TicketStore) cleanup() {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	now := time.Now()
	for token, expiry := range ts.tickets {
		if now.After(expiry) {
			delete(ts.tickets, token)
		}
	}
}
