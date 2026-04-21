package llm

import (
	"context"
	"strings"
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
	model = canonicalModelKey(model)
	if model == "" {
		return
	}
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
	model = canonicalModelKey(model)
	if model == "" {
		return 0.5
	}
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
	model = canonicalModelKey(model)
	if model == "" {
		return 0
	}
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
	model = canonicalModelKey(model)
	if model == "" {
		return 0
	}
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

// SeedFromHistory warms the tracker from recent inference observations in the database.
// Only stored quality scores are used here. The router should not infer
// efficacy from response length during warm start.
func (qt *QualityTracker) SeedFromHistory(ctx context.Context, store *db.Store) {
	if store == nil {
		return
	}

	rows, err := store.QueryContext(ctx,
		`SELECT provider, model, quality_score FROM inference_costs
		 WHERE model != '' AND quality_score IS NOT NULL
		 ORDER BY created_at DESC
		 LIMIT 500`)
	if err != nil {
		log.Warn().Err(err).Msg("quality tracker: failed to seed from history")
		return
	}
	defer func() { _ = rows.Close() }()

	seeded := 0
	for rows.Next() {
		var provider string
		var model string
		var quality float64
		if err := rows.Scan(&provider, &model, &quality); err != nil {
			continue
		}
		qt.Record(historyModelKey(provider, model), quality)
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
	priors     map[IntentClassKey]float64 // model+intent cold-start priors
	baselines  map[string]float64         // intentClass → baseline quality
	windowSize int
}

// NewIntentQualityTracker creates an intent-class-aware quality tracker.
func NewIntentQualityTracker(windowSize int) *IntentQualityTracker {
	if windowSize <= 0 {
		windowSize = 50
	}
	return &IntentQualityTracker{
		intents:    make(map[IntentClassKey]*ringBuffer),
		priors:     make(map[IntentClassKey]float64),
		baselines:  make(map[string]float64),
		windowSize: windowSize,
	}
}

func canonicalIntentClassKey(model, intentClass string) IntentClassKey {
	return IntentClassKey{
		Model:       canonicalModelKey(model),
		IntentClass: strings.TrimSpace(strings.ToUpper(intentClass)),
	}
}

// RecordWithIntent adds a quality observation for a model + intent class pair.
func (iq *IntentQualityTracker) RecordWithIntent(model, intentClass string, score float64) {
	key := canonicalIntentClassKey(model, intentClass)
	if key.Model == "" || key.IntentClass == "" {
		return
	}
	if score < 0 {
		score = 0
	}
	if score > 1 {
		score = 1
	}

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
	key := canonicalIntentClassKey(model, intentClass)
	if key.Model == "" || key.IntentClass == "" {
		return 0.5
	}

	iq.mu.RLock()
	defer iq.mu.RUnlock()

	rb, ok := iq.intents[key]
	if ok && rb.count > 0 {
		return rb.average()
	}

	if prior, exists := iq.priors[key]; exists {
		return prior
	}

	// Fall back to baseline for this intent class.
	if baseline, exists := iq.baselines[key.IntentClass]; exists {
		return baseline
	}
	return 0.5
}

// ObservationCountForIntent returns the number of recorded observations for a
// model+intentClass pair.
func (iq *IntentQualityTracker) ObservationCountForIntent(model, intentClass string) int {
	key := canonicalIntentClassKey(model, intentClass)
	if key.Model == "" || key.IntentClass == "" {
		return 0
	}

	iq.mu.RLock()
	defer iq.mu.RUnlock()

	rb, ok := iq.intents[key]
	if !ok {
		return 0
	}
	return rb.count
}

// HasObservationsForIntent reports whether any quality data exists for the
// model+intentClass pair.
func (iq *IntentQualityTracker) HasObservationsForIntent(model, intentClass string) bool {
	return iq.ObservationCountForIntent(model, intentClass) > 0
}

// SeedFromBaselines sets cold-start priors for intent classes. These are used
// when no observations exist for a model+intentClass pair.
func (iq *IntentQualityTracker) SeedFromBaselines(baselines map[string]float64) {
	iq.mu.Lock()
	defer iq.mu.Unlock()

	for k, v := range baselines {
		k = strings.TrimSpace(strings.ToUpper(k))
		if k == "" {
			continue
		}
		if v < 0 {
			v = 0
		}
		if v > 1 {
			v = 1
		}
		iq.baselines[k] = v
	}
}

// SeedFromExerciseResults warms the tracker from persisted benchmark/exercise
// observations so routing evidence reflects real exercised capability rather
// than only cold-start priors.
func (iq *IntentQualityTracker) SeedFromExerciseResults(ctx context.Context, store *db.Store) {
	if store == nil {
		return
	}

	rows, err := store.QueryContext(ctx,
		`SELECT model, intent_class, quality
		 FROM exercise_results
		 WHERE model != '' AND intent_class != ''
		 ORDER BY created_at DESC
		 LIMIT 1000`)
	if err != nil {
		log.Warn().Err(err).Msg("intent quality tracker: failed to seed from exercise results")
		return
	}
	defer func() { _ = rows.Close() }()

	seeded := 0
	for rows.Next() {
		var model, intentClass string
		var quality float64
		if err := rows.Scan(&model, &intentClass, &quality); err != nil {
			continue
		}
		iq.RecordWithIntent(model, intentClass, quality)
		seeded++
	}

	if seeded > 0 {
		log.Info().Int("seeded", seeded).Msg("intent quality tracker: warm-started from exercise results")
	}
}

// qualityFromResponse computes a weak online efficacy prior. This score must
// stay near-neutral because it is not grounded in user or verifier feedback.
// Structural failures are penalized, but concise valid responses are not.
func qualityFromResponse(resp *Response) float64 {
	if resp == nil {
		return 0
	}
	score := 0.55
	tokens := resp.Usage.OutputTokens
	if tokens <= 0 {
		content := strings.TrimSpace(resp.Content)
		if content == "" {
			return 0.15
		}
		tokens = EstimateTokens(content)
	}

	switch {
	case tokens < 4:
		score -= 0.20
	case tokens < 12:
		score -= 0.08
	case tokens <= 256:
		score += 0.05
	case tokens > 1024:
		score -= 0.05
	}

	if resp.FinishReason == "length" {
		score -= 0.20
	}

	if score < 0 {
		return 0
	}
	if score > 1 {
		return 1
	}
	return score
}
