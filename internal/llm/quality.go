package llm

import (
	"context"
	"sync"

	"github.com/rs/zerolog/log"

	"roboticus/internal/db"
)

// QualityTracker maintains per-model sliding windows of quality observations
// to inform model routing decisions. Thread-safe via sync.RWMutex.
type QualityTracker struct {
	mu         sync.RWMutex
	models     map[string]*ringBuffer
	windowSize int
}

// ringBuffer is a fixed-size circular buffer of float64 observations.
type ringBuffer struct {
	data  []float64
	head  int // next write position
	count int // number of valid entries (0..cap)
}

func newRingBuffer(size int) *ringBuffer {
	return &ringBuffer{data: make([]float64, size)}
}

func (rb *ringBuffer) push(v float64) {
	rb.data[rb.head] = v
	rb.head = (rb.head + 1) % len(rb.data)
	if rb.count < len(rb.data) {
		rb.count++
	}
}

func (rb *ringBuffer) average() float64 {
	if rb.count == 0 {
		return 0
	}
	var sum float64
	for i := 0; i < rb.count; i++ {
		sum += rb.data[i]
	}
	return sum / float64(rb.count)
}

// NewQualityTracker creates a tracker with the given sliding window size.
// If windowSize <= 0, it defaults to 100.
func NewQualityTracker(windowSize int) *QualityTracker {
	if windowSize <= 0 {
		windowSize = 100
	}
	return &QualityTracker{
		models:     make(map[string]*ringBuffer),
		windowSize: windowSize,
	}
}

// Record adds a quality observation for a model. Quality is clamped to [0.0, 1.0].
func (qt *QualityTracker) Record(model string, quality float64) {
	if quality < 0 {
		quality = 0
	}
	if quality > 1 {
		quality = 1
	}

	qt.mu.Lock()
	defer qt.mu.Unlock()

	rb, ok := qt.models[model]
	if !ok {
		rb = newRingBuffer(qt.windowSize)
		qt.models[model] = rb
	}
	rb.push(quality)
}

// EstimatedQuality returns the windowed average quality for a model.
// Returns 0.5 (neutral) if no observations exist.
func (qt *QualityTracker) EstimatedQuality(model string) float64 {
	qt.mu.RLock()
	defer qt.mu.RUnlock()

	rb, ok := qt.models[model]
	if !ok || rb.count == 0 {
		return 0.5
	}
	return rb.average()
}

// ObservationCount returns the number of recorded observations for a model.
func (qt *QualityTracker) ObservationCount(model string) int {
	qt.mu.RLock()
	defer qt.mu.RUnlock()

	rb, ok := qt.models[model]
	if !ok {
		return 0
	}
	return rb.count
}

// HasObservations returns true if any quality data exists for this model.
func (qt *QualityTracker) HasObservations(model string) bool {
	return qt.ObservationCount(model) > 0
}

// ClearModel removes all observations for a single model and returns the number removed.
func (qt *QualityTracker) ClearModel(model string) int {
	qt.mu.Lock()
	defer qt.mu.Unlock()

	rb, ok := qt.models[model]
	if !ok {
		return 0
	}
	count := rb.count
	delete(qt.models, model)
	return count
}

// ClearAll removes all observations and returns the number removed.
func (qt *QualityTracker) ClearAll() int {
	qt.mu.Lock()
	defer qt.mu.Unlock()

	total := 0
	for _, rb := range qt.models {
		total += rb.count
	}
	qt.models = make(map[string]*ringBuffer)
	return total
}

// SeedFromHistory warms the tracker from recent turns stored in the database.
// Quality heuristic: min(1.0, tokens_out / 100.0) for turns with tokens_out > 0.
func (qt *QualityTracker) SeedFromHistory(ctx context.Context, store *db.Store) {
	if store == nil {
		return
	}

	rows, err := store.QueryContext(ctx,
		`SELECT model, tokens_out FROM turns
		 WHERE model != '' AND tokens_out > 0
		 ORDER BY created_at DESC
		 LIMIT 500`)
	if err != nil {
		log.Warn().Err(err).Msg("quality tracker: failed to seed from history")
		return
	}
	defer func() { _ = rows.Close() }()

	seeded := 0
	for rows.Next() {
		var model string
		var tokensOut int
		if err := rows.Scan(&model, &tokensOut); err != nil {
			continue
		}
		quality := float64(tokensOut) / 100.0
		if quality > 1.0 {
			quality = 1.0
		}
		qt.Record(model, quality)
		seeded++
	}

	if seeded > 0 {
		log.Info().Int("seeded", seeded).Msg("quality tracker: warm-started from history")
	}
}

// IntentClassKey identifies a model+intentClass pair for quality tracking.
type IntentClassKey struct {
	Model       string
	IntentClass string
}

// IntentQualityTracker extends QualityTracker with per-intent-class tracking.
// This enables the router to estimate quality for specific intent categories
// (e.g., "code", "creative", "math") rather than just per-model averages.
type IntentQualityTracker struct {
	mu         sync.RWMutex
	intents    map[IntentClassKey]*ringBuffer
	baselines  map[string]float64 // intentClass → baseline quality
	windowSize int
}

// NewIntentQualityTracker creates an intent-class-aware quality tracker.
func NewIntentQualityTracker(windowSize int) *IntentQualityTracker {
	if windowSize <= 0 {
		windowSize = 50
	}
	return &IntentQualityTracker{
		intents:    make(map[IntentClassKey]*ringBuffer),
		baselines:  make(map[string]float64),
		windowSize: windowSize,
	}
}

// RecordWithIntent adds a quality observation for a model + intent class pair.
func (iq *IntentQualityTracker) RecordWithIntent(model, intentClass string, score float64) {
	if score < 0 {
		score = 0
	}
	if score > 1 {
		score = 1
	}

	key := IntentClassKey{Model: model, IntentClass: intentClass}

	iq.mu.Lock()
	defer iq.mu.Unlock()

	rb, ok := iq.intents[key]
	if !ok {
		rb = newRingBuffer(iq.windowSize)
		iq.intents[key] = rb
	}
	rb.push(score)
}

// EstimatedQualityForIntent returns the windowed average quality for a
// model+intentClass pair. Falls back to the baseline for the intent class
// if no observations exist, or 0.5 if no baseline is set either.
func (iq *IntentQualityTracker) EstimatedQualityForIntent(model, intentClass string) float64 {
	key := IntentClassKey{Model: model, IntentClass: intentClass}

	iq.mu.RLock()
	defer iq.mu.RUnlock()

	rb, ok := iq.intents[key]
	if ok && rb.count > 0 {
		return rb.average()
	}

	// Fall back to baseline for this intent class.
	if baseline, exists := iq.baselines[intentClass]; exists {
		return baseline
	}
	return 0.5
}

// SeedFromBaselines sets cold-start priors for intent classes. These are used
// when no observations exist for a model+intentClass pair.
func (iq *IntentQualityTracker) SeedFromBaselines(baselines map[string]float64) {
	iq.mu.Lock()
	defer iq.mu.Unlock()

	for k, v := range baselines {
		if v < 0 {
			v = 0
		}
		if v > 1 {
			v = 1
		}
		iq.baselines[k] = v
	}
}

// qualityFromResponse computes a quality score from a response using
// output length as a crude proxy. Longer, more substantive responses
// score higher.
func qualityFromResponse(resp *Response) float64 {
	if resp == nil {
		return 0
	}
	tokens := resp.Usage.OutputTokens
	if tokens <= 0 {
		// Fall back to content length estimate (rough 4 chars per token).
		tokens = len(resp.Content) / 4
	}
	q := float64(tokens) / 100.0
	if q > 1.0 {
		q = 1.0
	}
	if q < 0 {
		q = 0
	}
	return q
}
