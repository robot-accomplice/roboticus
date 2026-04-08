package llm

import (
	"context"
	"sort"
	"sync"

	"github.com/rs/zerolog/log"

	"roboticus/internal/db"
)

// LatencyTracker maintains per-model latency observations for the Speed
// metascore axis. Mirrors QualityTracker's ring-buffer pattern but tracks
// milliseconds instead of quality scores.
type LatencyTracker struct {
	mu         sync.RWMutex
	models     map[string]*latencyBuffer
	windowSize int
}

type latencyBuffer struct {
	data  []int64
	idx   int
	count int
}

func (b *latencyBuffer) push(v int64) {
	if b.count < len(b.data) {
		b.data[b.count] = v
		b.count++
	} else {
		b.data[b.idx] = v
		b.idx = (b.idx + 1) % len(b.data)
	}
}

func (b *latencyBuffer) values() []int64 {
	if b.count == 0 {
		return nil
	}
	result := make([]int64, b.count)
	copy(result, b.data[:b.count])
	return result
}

func (b *latencyBuffer) median() int64 {
	vals := b.values()
	if len(vals) == 0 {
		return 0
	}
	sort.Slice(vals, func(i, j int) bool { return vals[i] < vals[j] })
	mid := len(vals) / 2
	if len(vals)%2 == 0 {
		return (vals[mid-1] + vals[mid]) / 2
	}
	return vals[mid]
}

// NewLatencyTracker creates a tracker with the given window size per model.
func NewLatencyTracker(windowSize int) *LatencyTracker {
	if windowSize <= 0 {
		windowSize = 100
	}
	return &LatencyTracker{
		models:     make(map[string]*latencyBuffer),
		windowSize: windowSize,
	}
}

// Record adds a latency observation (milliseconds) for a model.
func (lt *LatencyTracker) Record(model string, latencyMs int64) {
	if model == "" || latencyMs < 0 {
		return
	}
	lt.mu.Lock()
	defer lt.mu.Unlock()
	buf, ok := lt.models[model]
	if !ok {
		buf = &latencyBuffer{data: make([]int64, lt.windowSize)}
		lt.models[model] = buf
	}
	buf.push(latencyMs)
}

// MedianLatency returns the windowed median latency for a model.
// Returns 0 if no observations exist.
func (lt *LatencyTracker) MedianLatency(model string) int64 {
	lt.mu.RLock()
	defer lt.mu.RUnlock()
	buf, ok := lt.models[model]
	if !ok {
		return 0
	}
	return buf.median()
}

// SpeedScore converts median latency to a 0-1 speed score.
// fastMs=1.0 (instant), slowMs=0.0 (unacceptable). Linear interpolation.
// Default thresholds: fast=500ms, slow=5000ms.
func (lt *LatencyTracker) SpeedScore(model string, fastMs, slowMs int64) float64 {
	if fastMs <= 0 {
		fastMs = 500
	}
	if slowMs <= 0 {
		slowMs = 5000
	}
	median := lt.MedianLatency(model)
	if median == 0 {
		return 0.5 // no data — neutral
	}
	if median <= fastMs {
		return 1.0
	}
	if median >= slowMs {
		return 0.0
	}
	// Linear interpolation.
	return 1.0 - float64(median-fastMs)/float64(slowMs-fastMs)
}

// HasObservations returns true if any latency data exists for this model.
func (lt *LatencyTracker) HasObservations(model string) bool {
	lt.mu.RLock()
	defer lt.mu.RUnlock()
	buf, ok := lt.models[model]
	return ok && buf.count > 0
}

// ObservationCount returns the number of latency observations for a model.
func (lt *LatencyTracker) ObservationCount(model string) int {
	lt.mu.RLock()
	defer lt.mu.RUnlock()
	buf, ok := lt.models[model]
	if !ok {
		return 0
	}
	return buf.count
}

// SeedFromHistory warms the tracker from recent inference cost records in the DB.
func (lt *LatencyTracker) SeedFromHistory(ctx context.Context, store *db.Store) {
	if store == nil {
		return
	}

	rows, err := store.QueryContext(ctx,
		`SELECT model, latency_ms FROM inference_costs
		 WHERE model != '' AND latency_ms > 0
		 ORDER BY created_at DESC
		 LIMIT 500`)
	if err != nil {
		log.Warn().Err(err).Msg("latency tracker: failed to seed from history")
		return
	}
	defer func() { _ = rows.Close() }()

	seeded := 0
	for rows.Next() {
		var model string
		var latencyMs int64
		if err := rows.Scan(&model, &latencyMs); err != nil {
			continue
		}
		lt.Record(model, latencyMs)
		seeded++
	}

	if seeded > 0 {
		log.Info().Int("seeded", seeded).Msg("latency tracker: warm-started from history")
	}
}
