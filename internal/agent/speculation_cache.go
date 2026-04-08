package agent

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
)

// SpeculationKey identifies a cacheable speculation branch by content hash and
// the set of tool names involved.
type SpeculationKey struct {
	ContentHash string
	ToolNames   []string
}

// cacheKey returns a deterministic string key for map lookups.
func (sk SpeculationKey) cacheKey() string {
	sorted := make([]string, len(sk.ToolNames))
	copy(sorted, sk.ToolNames)
	sort.Strings(sorted)
	raw := sk.ContentHash + "|" + strings.Join(sorted, ",")
	h := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(h[:])
}

// SpeculationCache is an LRU-like bounded cache for speculation branch results.
// Thread-safe via sync.RWMutex.
type SpeculationCache struct {
	mu      sync.RWMutex
	entries map[string]*BranchResult
	order   []string // insertion order for eviction
	maxSize int
}

// NewSpeculationCache creates a cache that holds at most maxSize entries.
// If maxSize <= 0, defaults to 256.
func NewSpeculationCache(maxSize int) *SpeculationCache {
	if maxSize <= 0 {
		maxSize = 256
	}
	return &SpeculationCache{
		entries: make(map[string]*BranchResult, maxSize),
		order:   make([]string, 0, maxSize),
		maxSize: maxSize,
	}
}

// Get retrieves a cached branch result. Returns (nil, false) on miss.
func (sc *SpeculationCache) Get(key SpeculationKey) (*BranchResult, bool) {
	sc.mu.RLock()
	defer sc.mu.RUnlock()
	k := key.cacheKey()
	result, ok := sc.entries[k]
	if !ok {
		return nil, false
	}
	// Return a copy to prevent mutation.
	cp := *result
	return &cp, true
}

// Put stores a branch result. If the cache is full, the oldest entry is evicted.
func (sc *SpeculationCache) Put(key SpeculationKey, result *BranchResult) {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	k := key.cacheKey()

	// If already present, update in-place (no eviction needed).
	if _, exists := sc.entries[k]; exists {
		cp := *result
		sc.entries[k] = &cp
		return
	}

	// Evict oldest if at capacity.
	if len(sc.entries) >= sc.maxSize {
		oldest := sc.order[0]
		sc.order = sc.order[1:]
		delete(sc.entries, oldest)
	}

	cp := *result
	sc.entries[k] = &cp
	sc.order = append(sc.order, k)
}

// Len returns the number of cached entries.
func (sc *SpeculationCache) Len() int {
	sc.mu.RLock()
	defer sc.mu.RUnlock()
	return len(sc.entries)
}

// --- Speculation Slot Guard ---

// ErrNoSlotAvailable is returned when all speculation slots are in use.
var ErrNoSlotAvailable = errors.New("no speculation slot available")

// SpeculationSlotGuard limits concurrent speculation branches. Acquire a slot
// before starting speculative execution and release it when done.
type SpeculationSlotGuard struct {
	executor *SpeculativeExecutor
	released bool
}

// Release returns the slot to the executor's pool. Safe to call multiple times.
func (sg *SpeculationSlotGuard) Release() {
	if sg.released {
		return
	}
	sg.released = true
	sg.executor.releaseSlot()
}

// AcquireSlot attempts to acquire a speculation slot. Returns ErrNoSlotAvailable
// if all slots are in use. The caller must call Release() on the returned guard
// when speculation is complete.
func (se *SpeculativeExecutor) AcquireSlot() (*SpeculationSlotGuard, error) {
	se.slotMu.Lock()
	defer se.slotMu.Unlock()

	if se.activeSlots >= se.maxSlots {
		return nil, fmt.Errorf("%w: %d/%d slots in use", ErrNoSlotAvailable, se.activeSlots, se.maxSlots)
	}
	se.activeSlots++
	return &SpeculationSlotGuard{executor: se, released: false}, nil
}

func (se *SpeculativeExecutor) releaseSlot() {
	se.slotMu.Lock()
	defer se.slotMu.Unlock()
	if se.activeSlots > 0 {
		se.activeSlots--
	}
}

// ActiveSlots returns the number of currently held speculation slots.
func (se *SpeculativeExecutor) ActiveSlots() int {
	se.slotMu.Lock()
	defer se.slotMu.Unlock()
	return se.activeSlots
}
