package llm

import (
	"context"
	"math"
	"testing"
	"time"
)

// ─── 1. MetascoreBreakdown + SelectByMetascoreWeighted ───

func TestMetascoreBreakdown_Components(t *testing.T) {
	p := ModelProfile{
		Quality: 0.8, Availability: 1.0, Cost: 0.2,
		Locality: 1.0, Confidence: 0.9, Speed: 0.7,
	}
	w := DefaultRoutingWeights()
	b := p.Breakdown(w)

	// Each component should be the RAW dimension score [0, 1], not weighted.
	if diff := math.Abs(b.Efficacy - p.Quality); diff > 1e-9 {
		t.Errorf("efficacy: got %f, want %f (raw quality)", b.Efficacy, p.Quality)
	}
	// Cost is inverted from profile's Cost field.
	if diff := math.Abs(b.Cost - (1.0 - p.Cost)); diff > 1e-9 {
		t.Errorf("cost: got %f, want %f (inverted)", b.Cost, 1.0-p.Cost)
	}
	// Speed is raw.
	if diff := math.Abs(b.Speed - p.Speed); diff > 1e-9 {
		t.Errorf("speed: got %f, want %f", b.Speed, p.Speed)
	}
	// Confidence is raw.
	if diff := math.Abs(b.Confidence - p.Confidence); diff > 1e-9 {
		t.Errorf("confidence: got %f, want %f", b.Confidence, p.Confidence)
	}
	// FinalScore should be a weighted combination < 1.0.
	if b.FinalScore <= 0 || b.FinalScore > 1.0 {
		t.Errorf("final_score should be in (0, 1], got %f", b.FinalScore)
	}
	// Total() should return FinalScore.
	if b.Total() != b.FinalScore {
		t.Errorf("Total() %f != FinalScore %f", b.Total(), b.FinalScore)
	}
}

func TestSelectByMetascoreWeighted(t *testing.T) {
	profiles := []ModelProfile{
		{Model: "cheap", Quality: 0.3, Availability: 1.0, Cost: 0.0, Confidence: 1.0, Speed: 1.0},
		{Model: "good", Quality: 0.9, Availability: 1.0, Cost: 0.5, Confidence: 1.0, Speed: 0.5},
	}

	// With high efficacy weight, the good model should win.
	w := RoutingWeights{Efficacy: 0.9, Cost: 0.1}
	winner := SelectByMetascoreWeighted(profiles, w)
	if winner == nil || winner.Model != "good" {
		t.Errorf("expected 'good', got %v", winner)
	}

	// With high cost weight, the cheap model should win.
	w = RoutingWeights{Efficacy: 0.1, Cost: 0.9}
	winner = SelectByMetascoreWeighted(profiles, w)
	if winner == nil || winner.Model != "cheap" {
		t.Errorf("expected 'cheap', got %v", winner)
	}
}

func TestSelectByMetascoreWeighted_Empty(t *testing.T) {
	winner := SelectByMetascoreWeighted(nil, DefaultRoutingWeights())
	if winner != nil {
		t.Errorf("expected nil for empty profiles, got %v", winner)
	}
}

// ─── 2. Semantic cache lookup ───

func TestGetSemantic_HitAboveThreshold(t *testing.T) {
	cfg := DefaultCacheConfig()
	c := NewCache(cfg, nil, nil)

	resp := &Response{Content: "cached answer", Model: "test"}
	embedding := []float64{1.0, 0.0, 0.0}

	c.PutWithEmbedding(context.Background(), &Request{Model: "test", Messages: []Message{{Role: "user", Content: "q"}}}, resp, embedding)

	// Same embedding should hit.
	got, ok := c.GetSemantic(context.Background(), []float64{1.0, 0.0, 0.0}, 0.9)
	if !ok || got == nil {
		t.Fatal("expected semantic hit")
	}
	if got.Content != "cached answer" {
		t.Errorf("got %q, want 'cached answer'", got.Content)
	}
}

func TestGetSemantic_MissBelowThreshold(t *testing.T) {
	cfg := DefaultCacheConfig()
	c := NewCache(cfg, nil, nil)

	resp := &Response{Content: "cached", Model: "test"}
	c.PutWithEmbedding(context.Background(), &Request{Model: "test", Messages: []Message{{Role: "user", Content: "q"}}}, resp, []float64{1.0, 0.0, 0.0})

	// Orthogonal embedding should miss.
	_, ok := c.GetSemantic(context.Background(), []float64{0.0, 1.0, 0.0}, 0.9)
	if ok {
		t.Error("expected semantic miss for orthogonal vector")
	}
}

func TestCosineSimilarity(t *testing.T) {
	// Identical vectors.
	sim := cosineSimilarity([]float64{1, 0}, []float64{1, 0})
	if math.Abs(sim-1.0) > 1e-9 {
		t.Errorf("identical vectors: got %f, want 1.0", sim)
	}

	// Orthogonal vectors.
	sim = cosineSimilarity([]float64{1, 0}, []float64{0, 1})
	if math.Abs(sim) > 1e-9 {
		t.Errorf("orthogonal vectors: got %f, want 0.0", sim)
	}

	// Different lengths.
	sim = cosineSimilarity([]float64{1, 0}, []float64{1, 0, 0})
	if sim != 0 {
		t.Errorf("different dimensions: got %f, want 0.0", sim)
	}
}

// ─── 3. Intent-class quality tracking ───

func TestIntentQualityTracker_RecordAndEstimate(t *testing.T) {
	iq := NewIntentQualityTracker(10)

	iq.RecordWithIntent("gpt-4", "code", 0.9)
	iq.RecordWithIntent("gpt-4", "code", 0.8)
	iq.RecordWithIntent("gpt-4", "creative", 0.6)

	codeQ := iq.EstimatedQualityForIntent("gpt-4", "code")
	if math.Abs(codeQ-0.85) > 1e-9 {
		t.Errorf("code quality: got %f, want 0.85", codeQ)
	}

	creativeQ := iq.EstimatedQualityForIntent("gpt-4", "creative")
	if math.Abs(creativeQ-0.6) > 1e-9 {
		t.Errorf("creative quality: got %f, want 0.6", creativeQ)
	}
}

func TestIntentQualityTracker_FallsBackToBaseline(t *testing.T) {
	iq := NewIntentQualityTracker(10)
	iq.SeedFromBaselines(map[string]float64{"math": 0.7})

	q := iq.EstimatedQualityForIntent("gpt-4", "math")
	if math.Abs(q-0.7) > 1e-9 {
		t.Errorf("baseline fallback: got %f, want 0.7", q)
	}

	// Unknown intent with no baseline returns 0.5.
	q = iq.EstimatedQualityForIntent("gpt-4", "unknown")
	if math.Abs(q-0.5) > 1e-9 {
		t.Errorf("no-baseline fallback: got %f, want 0.5", q)
	}
}

func TestIntentQualityTracker_Clamping(t *testing.T) {
	iq := NewIntentQualityTracker(10)
	iq.RecordWithIntent("m", "c", -5.0)
	iq.RecordWithIntent("m", "c", 10.0)

	q := iq.EstimatedQualityForIntent("m", "c")
	if q < 0 || q > 1 {
		t.Errorf("clamping failed: got %f", q)
	}
}

// ─── 4. Cascade utility formula ───

func TestCascadeOptimizer_HighSuccessRatePrefersCascade(t *testing.T) {
	co := NewCascadeOptimizer(50)
	for i := 0; i < 20; i++ {
		co.Record(CascadeOutcome{
			QueryClass:    "simple",
			WeakModelUsed: true,
			WeakSucceeded: true,
			WeakLatency:   50 * time.Millisecond,
			StrongLatency: 500 * time.Millisecond,
		})
	}

	strategy := co.ShouldCascade("simple")
	if strategy != StrategyCascade {
		t.Error("high success rate should prefer cascade")
	}
}

func TestCascadeOptimizer_LowSuccessRatePrefersDirect(t *testing.T) {
	co := NewCascadeOptimizer(50)
	for i := 0; i < 20; i++ {
		co.Record(CascadeOutcome{
			QueryClass:    "hard",
			WeakModelUsed: true,
			WeakSucceeded: false,
			WeakLatency:   200 * time.Millisecond,
			StrongLatency: 300 * time.Millisecond,
		})
	}

	strategy := co.ShouldCascade("hard")
	if strategy != StrategyDirect {
		t.Error("low success rate should prefer direct")
	}
}

// ─── 5. Confidence scoring ───

func TestConfidenceEvaluator_FastLatencyBoost(t *testing.T) {
	ce := NewConfidenceEvaluator(0.7)
	fast := ce.latencyScore(100 * time.Millisecond)   // < 200ms
	medium := ce.latencyScore(500 * time.Millisecond) // 200-1000ms

	if fast <= medium {
		t.Errorf("fast (%f) should score higher than medium (%f)", fast, medium)
	}
	if fast != 1.0 {
		t.Errorf("sub-200ms should score 1.0, got %f", fast)
	}
}

func TestConfidenceEvaluator_5thLengthBucket(t *testing.T) {
	ce := NewConfidenceEvaluator(0.7)

	// 201-1000 chars should score 0.85.
	score := ce.lengthScore(string(make([]byte, 500)))
	if score != 0.85 {
		t.Errorf("500 chars should score 0.85, got %f", score)
	}

	// >1000 chars should score 1.0.
	score = ce.lengthScore(string(make([]byte, 1500)))
	if score != 1.0 {
		t.Errorf("1500 chars should score 1.0, got %f", score)
	}
}

// ─── 6. Preemptive HalfOpen ───

func TestTryPreemptiveHalfOpen_Success(t *testing.T) {
	cb := NewCircuitBreaker(DefaultCircuitBreakerConfig())
	cb.SetCapacityPressure(true)

	if !cb.TryPreemptiveHalfOpen() {
		t.Error("should transition closed+pressure to half-open")
	}
	if cb.State() != CircuitHalfOpen {
		t.Errorf("state should be half-open, got %v", cb.State())
	}
}

func TestTryPreemptiveHalfOpen_NoPressure(t *testing.T) {
	cb := NewCircuitBreaker(DefaultCircuitBreakerConfig())
	// No capacity pressure set.
	if cb.TryPreemptiveHalfOpen() {
		t.Error("should not transition without capacity pressure")
	}
}

func TestTryPreemptiveHalfOpen_NotClosed(t *testing.T) {
	cb := NewCircuitBreaker(DefaultCircuitBreakerConfig())
	cb.ForceOpen()
	cb.SetCapacityPressure(true)

	if cb.TryPreemptiveHalfOpen() {
		t.Error("should not transition from forced-open")
	}
}

// ─── 7. Metascore eval harness ───

func TestRunMetascoreEval(t *testing.T) {
	corpus := []MetascoreEvalCase{
		{
			Label: "efficacy-wins",
			Profiles: []ModelProfile{
				{Model: "good", Quality: 0.9, Availability: 1.0, Cost: 0.5, Confidence: 1.0},
				{Model: "cheap", Quality: 0.3, Availability: 1.0, Cost: 0.0, Confidence: 1.0},
			},
			Weights:  RoutingWeights{Efficacy: 0.9, Cost: 0.1},
			Expected: "good",
		},
		{
			Label: "cost-wins",
			Profiles: []ModelProfile{
				{Model: "good", Quality: 0.9, Availability: 1.0, Cost: 0.9, Confidence: 1.0},
				{Model: "cheap", Quality: 0.3, Availability: 1.0, Cost: 0.0, Confidence: 1.0},
			},
			Weights:  RoutingWeights{Efficacy: 0.1, Cost: 0.9},
			Expected: "cheap",
		},
	}

	result := RunMetascoreEval(corpus)
	if result.Total != 2 {
		t.Fatalf("total: got %d, want 2", result.Total)
	}
	if result.Correct != 2 {
		t.Errorf("correct: got %d, want 2; errors: %v", result.Correct, result.Errors)
	}
	if len(result.Details) != 2 {
		t.Errorf("details: got %d, want 2", len(result.Details))
	}
}

// ─── 8. Capacity utilization stats ───

func TestCapacityUtilizationStats(t *testing.T) {
	ct := NewCapacityTracker(1000, 60)
	ct.Register("openai", 100000, 100)

	// Record some usage.
	ct.Record("openai", 500)
	ct.Record("openai", 300)

	stats := ct.UtilizationStats()
	if len(stats) != 1 {
		t.Fatalf("expected 1 provider, got %d", len(stats))
	}

	s := stats[0]
	if s.Provider != "openai" {
		t.Errorf("provider: got %q, want 'openai'", s.Provider)
	}
	if s.RPMUsed != 2 {
		t.Errorf("rpm used: got %d, want 2", s.RPMUsed)
	}
	if s.TPMUsed != 800 {
		t.Errorf("tpm used: got %d, want 800", s.TPMUsed)
	}
	if s.RPMLimit != 100 {
		t.Errorf("rpm limit: got %d, want 100", s.RPMLimit)
	}
	if s.Utilization < 0 || s.Utilization > 1 {
		t.Errorf("utilization out of range: %f", s.Utilization)
	}
}

func TestCapacityUtilizationStats_Empty(t *testing.T) {
	ct := NewCapacityTracker(1000, 60)
	stats := ct.UtilizationStats()
	if len(stats) != 0 {
		t.Errorf("expected empty stats, got %d", len(stats))
	}
}

// ─── 9. TransformOutput ───

func TestTransformPipeline_ApplyWithOutput(t *testing.T) {
	pipeline := DefaultTransformPipeline()

	input := "<think>internal reasoning</think>Hello, world!"
	output := pipeline.ApplyWithOutput(input)

	if output.Content != "Hello, world!" {
		t.Errorf("content: got %q, want 'Hello, world!'", output.Content)
	}
	if output.ReasoningExtracted != "internal reasoning" {
		t.Errorf("reasoning: got %q, want 'internal reasoning'", output.ReasoningExtracted)
	}
	if !output.Modified {
		t.Error("expected modified=true")
	}
}

func TestTransformPipeline_ApplyWithOutput_NoReasoning(t *testing.T) {
	pipeline := DefaultTransformPipeline()

	input := "Just a normal response."
	output := pipeline.ApplyWithOutput(input)

	if output.Content != "Just a normal response." {
		t.Errorf("content: got %q", output.Content)
	}
	if output.ReasoningExtracted != "" {
		t.Errorf("reasoning should be empty, got %q", output.ReasoningExtracted)
	}
	if output.Modified {
		t.Error("expected modified=false for unchanged content")
	}
}

func TestReasoningExtractor_ExtractReasoning(t *testing.T) {
	re := &ReasoningExtractor{}

	content := "<think>first block</think>middle<think>second block</think>"
	reasoning := re.ExtractReasoning(content)

	if reasoning != "first block\n\nsecond block" {
		t.Errorf("got %q", reasoning)
	}
}

// ─── 10. MaxAutoPayUSDC ───

func TestMaxAutoPayUSDC_Value(t *testing.T) {
	if MaxAutoPayUSDC != 1.0 {
		t.Errorf("MaxAutoPayUSDC: got %f, want 1.0", MaxAutoPayUSDC)
	}
}
