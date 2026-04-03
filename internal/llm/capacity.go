package llm

import (
	"sync"
	"time"
)

const (
	// maxRingSize caps the per-provider ring buffer.
	maxRingSize = 10_000

	// windowDuration is the sliding window length for rate calculations.
	windowDuration = time.Minute

	// sustainedHotThreshold is the utilization fraction (0-1) above which a
	// provider is considered "hot".
	sustainedHotThreshold = 0.90

	// sustainedHotMinRequests is the minimum request count in the window for
	// sustained-hot detection.
	sustainedHotMinRequests = 3

	// sustainedHotMinTokens is the minimum token count in the window for
	// sustained-hot detection.
	sustainedHotMinTokens = 1024
)

// windowEntry records a single request event.
type windowEntry struct {
	timestamp time.Time
	tokens    int
}

// providerWindow holds a capped ring buffer of events and per-provider limits.
type providerWindow struct {
	entries  []windowEntry
	head     int // next write index in the ring
	count    int // number of valid entries
	tpmLimit int
	rpmLimit int
}

// CapacityTracker tracks tokens-per-minute and requests-per-minute per provider.
type CapacityTracker struct {
	mu             sync.RWMutex
	providers      map[string]*providerWindow
	defaultTPM     int
	defaultRPM     int
	nowFunc        func() time.Time // for testing
}

// NewCapacityTracker creates a tracker with fallback TPM and RPM limits for
// unregistered providers. A limit of 0 means unlimited.
func NewCapacityTracker(tpmLimit, rpmLimit int) *CapacityTracker {
	return &CapacityTracker{
		providers:  make(map[string]*providerWindow),
		defaultTPM: tpmLimit,
		defaultRPM: rpmLimit,
		nowFunc:    time.Now,
	}
}

// Register sets per-provider TPM and RPM limits. Must be called before
// recording events for providers that need custom limits.
func (ct *CapacityTracker) Register(provider string, tpmLimit, rpmLimit int) {
	ct.mu.Lock()
	defer ct.mu.Unlock()

	pw := ct.getOrCreate(provider)
	pw.tpmLimit = tpmLimit
	pw.rpmLimit = rpmLimit
}

// Record adds a request event for the given provider.
func (ct *CapacityTracker) Record(provider string, tokens int) {
	ct.mu.Lock()
	defer ct.mu.Unlock()

	pw := ct.getOrCreate(provider)
	pw.append(windowEntry{timestamp: ct.nowFunc(), tokens: tokens})
}

// Available returns true if the provider has capacity remaining in the current window.
func (ct *CapacityTracker) Available(provider string) bool {
	ct.mu.RLock()
	defer ct.mu.RUnlock()

	pw, ok := ct.providers[provider]
	if !ok {
		return true
	}

	cutoff := ct.nowFunc().Add(-windowDuration)
	rpm, tpm := pw.usageSince(cutoff)

	if pw.rpmLimit > 0 && rpm >= pw.rpmLimit {
		return false
	}
	if pw.tpmLimit > 0 && tpm >= pw.tpmLimit {
		return false
	}
	return true
}

// Usage returns current RPM and TPM for a provider within the sliding window.
func (ct *CapacityTracker) Usage(provider string) (rpm int, tpm int) {
	ct.mu.RLock()
	defer ct.mu.RUnlock()

	pw, ok := ct.providers[provider]
	if !ok {
		return 0, 0
	}

	cutoff := ct.nowFunc().Add(-windowDuration)
	return pw.usageSince(cutoff)
}

// Headroom returns a value from 0.0 (saturated) to 1.0 (idle) representing
// the minimum of TPM headroom and RPM headroom.
// For unknown or unregistered providers it returns 1.0.
func (ct *CapacityTracker) Headroom(provider string) float64 {
	ct.mu.RLock()
	defer ct.mu.RUnlock()

	pw, ok := ct.providers[provider]
	if !ok {
		return 1.0
	}

	cutoff := ct.nowFunc().Add(-windowDuration)
	rpm, tpm := pw.usageSince(cutoff)

	tpmH := headroomFraction(tpm, pw.tpmLimit)
	rpmH := headroomFraction(rpm, pw.rpmLimit)

	if tpmH < rpmH {
		return tpmH
	}
	return rpmH
}

// IsSustainedHot returns true when the provider's utilization is >= 90% AND
// at least sustainedHotMinRequests requests or sustainedHotMinTokens tokens
// have been recorded in the current window.
func (ct *CapacityTracker) IsSustainedHot(provider string) bool {
	ct.mu.RLock()
	defer ct.mu.RUnlock()

	pw, ok := ct.providers[provider]
	if !ok {
		return false
	}

	cutoff := ct.nowFunc().Add(-windowDuration)
	rpm, tpm := pw.usageSince(cutoff)

	// Must meet minimum activity thresholds.
	if rpm < sustainedHotMinRequests && tpm < sustainedHotMinTokens {
		return false
	}

	// Check if utilization >= 90% on either dimension.
	tpmUtil := utilization(tpm, pw.tpmLimit)
	rpmUtil := utilization(rpm, pw.rpmLimit)

	// Use the higher utilization: if either dimension is hot, the provider is hot.
	maxUtil := tpmUtil
	if rpmUtil > maxUtil {
		maxUtil = rpmUtil
	}
	return maxUtil >= sustainedHotThreshold
}

// ---------- internal helpers ----------

// getOrCreate returns the providerWindow for the given provider, creating one
// with default limits if it does not exist. Caller must hold ct.mu (write lock).
func (ct *CapacityTracker) getOrCreate(provider string) *providerWindow {
	pw, ok := ct.providers[provider]
	if !ok {
		pw = &providerWindow{
			entries:  make([]windowEntry, maxRingSize),
			tpmLimit: ct.defaultTPM,
			rpmLimit: ct.defaultRPM,
		}
		ct.providers[provider] = pw
	}
	return pw
}

// append adds an entry to the ring buffer, overwriting the oldest entry when full.
func (pw *providerWindow) append(e windowEntry) {
	pw.entries[pw.head] = e
	pw.head = (pw.head + 1) % maxRingSize
	if pw.count < maxRingSize {
		pw.count++
	}
}

// usageSince returns (requests, tokens) for entries at or after cutoff.
func (pw *providerWindow) usageSince(cutoff time.Time) (rpm int, tpm int) {
	start := pw.head - pw.count
	if start < 0 {
		start += maxRingSize
	}
	for i := 0; i < pw.count; i++ {
		idx := (start + i) % maxRingSize
		e := pw.entries[idx]
		if !e.timestamp.Before(cutoff) {
			rpm++
			tpm += e.tokens
		}
	}
	return
}

// headroomFraction returns 1.0 - (used/limit), clamped to [0, 1].
// A limit of 0 (unlimited) returns 1.0.
func headroomFraction(used, limit int) float64 {
	if limit <= 0 {
		return 1.0
	}
	h := 1.0 - float64(used)/float64(limit)
	if h < 0 {
		return 0
	}
	if h > 1 {
		return 1
	}
	return h
}

// utilization returns used/limit as a fraction in [0, 1].
// A limit of 0 (unlimited) returns 0.0.
func utilization(used, limit int) float64 {
	if limit <= 0 {
		return 0.0
	}
	u := float64(used) / float64(limit)
	if u > 1 {
		return 1.0
	}
	return u
}
