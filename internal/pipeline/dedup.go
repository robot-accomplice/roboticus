package pipeline

import (
	"crypto/sha256"
	"encoding/hex"
	"sync"
	"time"
)

// DedupTracker rejects concurrent identical inference requests.
// It maintains a set of in-flight request fingerprints; a second request
// with the same fingerprint while the first is still in-flight is rejected.
//
// This matches the Rust pipeline's dedup guard behavior: fingerprint the
// unified message, check_and_track, and release on scope exit.
type DedupTracker struct {
	mu       sync.Mutex
	inflight map[string]time.Time
	ttl      time.Duration // max time before auto-expiry (safety net)
}

// NewDedupTracker creates a tracker with a safety-net TTL for stale entries.
func NewDedupTracker(ttl time.Duration) *DedupTracker {
	if ttl <= 0 {
		ttl = 60 * time.Second
	}
	return &DedupTracker{
		inflight: make(map[string]time.Time),
		ttl:      ttl,
	}
}

// Fingerprint computes a SHA-256 hash of the content + agent + session
// to identify duplicate requests.
func Fingerprint(content, agentID, sessionID string) string {
	h := sha256.New()
	h.Write([]byte(content))
	h.Write([]byte{0})
	h.Write([]byte(agentID))
	h.Write([]byte{0})
	h.Write([]byte(sessionID))
	return hex.EncodeToString(h.Sum(nil))[:16] // 16 hex chars = 64 bits
}

// CheckAndTrack returns true if the request is allowed (no duplicate in flight).
// Returns false if a concurrent identical request is already being processed.
// On true, the caller MUST call Release(fp) when the request completes.
func (d *DedupTracker) CheckAndTrack(fp string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()

	// Evict any stale entries (safety net for leaked guards).
	now := time.Now()
	for k, t := range d.inflight {
		if now.Sub(t) > d.ttl {
			delete(d.inflight, k)
		}
	}

	if _, exists := d.inflight[fp]; exists {
		return false // duplicate in flight
	}
	d.inflight[fp] = now
	return true
}

// Release removes a fingerprint from the in-flight set.
// Must be called when the request completes (success or failure).
func (d *DedupTracker) Release(fp string) {
	d.mu.Lock()
	delete(d.inflight, fp)
	d.mu.Unlock()
}

// InFlightCount returns the number of currently tracked in-flight requests.
func (d *DedupTracker) InFlightCount() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.inflight)
}
