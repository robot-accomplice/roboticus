package llm

import (
	"sync"
	"time"
)

// CapacityTracker tracks tokens-per-minute and requests-per-minute for a provider.
type CapacityTracker struct {
	mu       sync.Mutex
	windows  map[string]*slidingWindow
	tpmLimit int
	rpmLimit int
}

type slidingWindow struct {
	entries []windowEntry
}

type windowEntry struct {
	timestamp time.Time
	tokens    int
}

// NewCapacityTracker creates a tracker with the given TPM and RPM limits.
// A limit of 0 means unlimited.
func NewCapacityTracker(tpmLimit, rpmLimit int) *CapacityTracker {
	return &CapacityTracker{
		windows:  make(map[string]*slidingWindow),
		tpmLimit: tpmLimit,
		rpmLimit: rpmLimit,
	}
}

// Record adds a request to the window.
func (ct *CapacityTracker) Record(provider string, tokens int) {
	ct.mu.Lock()
	defer ct.mu.Unlock()
	w, ok := ct.windows[provider]
	if !ok {
		w = &slidingWindow{}
		ct.windows[provider] = w
	}
	w.entries = append(w.entries, windowEntry{timestamp: time.Now(), tokens: tokens})
}

// Available returns true if the provider has capacity.
func (ct *CapacityTracker) Available(provider string) bool {
	ct.mu.Lock()
	defer ct.mu.Unlock()
	w, ok := ct.windows[provider]
	if !ok {
		return true
	}
	cutoff := time.Now().Add(-1 * time.Minute)
	w.prune(cutoff)

	if ct.rpmLimit > 0 && len(w.entries) >= ct.rpmLimit {
		return false
	}
	if ct.tpmLimit > 0 {
		total := 0
		for _, e := range w.entries {
			total += e.tokens
		}
		if total >= ct.tpmLimit {
			return false
		}
	}
	return true
}

// Usage returns current RPM and TPM for a provider.
func (ct *CapacityTracker) Usage(provider string) (rpm int, tpm int) {
	ct.mu.Lock()
	defer ct.mu.Unlock()
	w, ok := ct.windows[provider]
	if !ok {
		return 0, 0
	}
	cutoff := time.Now().Add(-1 * time.Minute)
	w.prune(cutoff)
	rpm = len(w.entries)
	for _, e := range w.entries {
		tpm += e.tokens
	}
	return
}

func (w *slidingWindow) prune(cutoff time.Time) {
	i := 0
	for i < len(w.entries) && w.entries[i].timestamp.Before(cutoff) {
		i++
	}
	w.entries = w.entries[i:]
}
