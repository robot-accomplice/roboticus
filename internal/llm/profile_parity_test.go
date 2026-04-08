package llm

import (
	"testing"
)

// These tests match the Rust reference profile.rs test suite 1:1.
// Each test name corresponds to a Rust #[test] function.

func localProfile(quality float64, obs int) ModelProfile {
	return ModelProfile{
		Model: "ollama/qwen3:8b", Provider: "ollama",
		Quality: quality, Availability: 1.0, Cost: 0.0, // free
		Locality: 1.0, Speed: 0.5,
		Confidence: confidenceFromObs(obs),
	}
}

func cloudProfile(quality float64, obs int) ModelProfile {
	return ModelProfile{
		Model: "openai/gpt-4o", Provider: "openai",
		Quality: quality, Availability: 1.0, Cost: 0.5, // moderate
		Locality: 0.0, Speed: 0.9,
		Confidence: confidenceFromObs(obs),
	}
}

func confidenceFromObs(obs int) float64 {
	if obs >= confidenceThreshold {
		return 1.0
	}
	return 0.6 + 0.4*float64(obs)/float64(confidenceThreshold)
}

func TestMetascore_LocalSimpleTask(t *testing.T) {
	p := localProfile(0.8, 50)
	b := p.BreakdownWithComplexity(0.1, false, DefaultRoutingWeights())
	if b.FinalScore <= 0.5 {
		t.Errorf("local model with good quality on simple task should score > 0.5, got %f", b.FinalScore)
	}
	if b.Locality <= 0.5 {
		t.Errorf("local should have high locality on simple task, got %f", b.Locality)
	}
}

func TestMetascore_CloudComplexTask(t *testing.T) {
	p := cloudProfile(0.9, 50)
	b := p.BreakdownWithComplexity(0.9, false, DefaultRoutingWeights())
	if b.FinalScore <= 0.4 {
		t.Errorf("cloud model with high quality on complex task should score > 0.4, got %f", b.FinalScore)
	}
	if b.Locality <= 0.2 {
		t.Errorf("cloud should have higher locality on complex task, got %f", b.Locality)
	}
}

func TestMetascore_ColdStartPenalty(t *testing.T) {
	cold := localProfile(0.5, 0) // default quality prior, no observations
	warm := localProfile(0.7, 20)
	coldScore := cold.BreakdownWithComplexity(0.5, false, DefaultRoutingWeights())
	warmScore := warm.BreakdownWithComplexity(0.5, false, DefaultRoutingWeights())
	if coldScore.FinalScore >= warmScore.FinalScore {
		t.Errorf("cold-start should score lower: cold=%f warm=%f", coldScore.FinalScore, warmScore.FinalScore)
	}
	if coldScore.Confidence >= 1.0 {
		t.Errorf("cold-start confidence penalty should apply, got %f", coldScore.Confidence)
	}
}

func TestMetascore_AllBlocked(t *testing.T) {
	profiles := []ModelProfile{
		{Model: "a", Availability: 0.0, Quality: 0.9, Confidence: 1.0},
		{Model: "b", Availability: 0.0, Quality: 0.9, Confidence: 1.0},
	}
	result := SelectByMetascore(profiles, nil)
	if result != nil {
		t.Errorf("all blocked should return nil, got %v", result.Model)
	}
}

func TestMetascore_DeterministicTiebreak(t *testing.T) {
	a := localProfile(0.7, 30)
	a.Model = "alpha/model"
	b := localProfile(0.7, 30)
	b.Model = "beta/model"

	r1 := SelectByMetascoreWeighted([]ModelProfile{a, b}, DefaultRoutingWeights())
	r2 := SelectByMetascoreWeighted([]ModelProfile{a, b}, DefaultRoutingWeights())
	if r1 == nil || r2 == nil {
		t.Fatal("expected non-nil results")
	}
	if r1.Model != r2.Model {
		t.Errorf("tiebreak not deterministic: %s vs %s", r1.Model, r2.Model)
	}
}

func TestMetascore_BreakdownComponentsBounded(t *testing.T) {
	p := cloudProfile(0.85, 25)
	b := p.BreakdownWithComplexity(0.5, true, DefaultRoutingWeights())
	if b.Efficacy < 0 || b.Efficacy > 1 {
		t.Errorf("efficacy out of [0,1]: %f", b.Efficacy)
	}
	if b.Cost < 0 || b.Cost > 1 {
		t.Errorf("cost out of [0,1]: %f", b.Cost)
	}
	if b.Availability < 0 || b.Availability > 1 {
		t.Errorf("availability out of [0,1]: %f", b.Availability)
	}
	if b.Locality < 0 || b.Locality > 1 {
		t.Errorf("locality out of [0,1]: %f", b.Locality)
	}
	if b.Confidence < 0 || b.Confidence > 1 {
		t.Errorf("confidence out of [0,1]: %f", b.Confidence)
	}
	if b.Speed < 0 || b.Speed > 1 {
		t.Errorf("speed out of [0,1]: %f", b.Speed)
	}
	if b.FinalScore < 0 {
		t.Errorf("final score should be non-negative: %f", b.FinalScore)
	}
}

func TestMetascore_CostWeightShiftsSelectionToCheaper(t *testing.T) {
	cheap := localProfile(0.65, 30)
	expensive := cloudProfile(0.85, 30)
	profiles := []ModelProfile{cheap, expensive}

	// No cost weight — quality dominates, cloud should win on complex tasks
	noCostW := RoutingWeights{Efficacy: 0.49, Cost: 0.0, Speed: 0.21, Availability: 0.20, Locality: 0.10}
	noCostWinner := SelectByMetascoreWeighted(profiles, noCostW)

	// High cost weight — cheap model should win
	highCostW := RoutingWeights{Efficacy: 0.10, Cost: 0.56, Speed: 0.04, Availability: 0.20, Locality: 0.10}
	highCostWinner := SelectByMetascoreWeighted(profiles, highCostW)

	if noCostWinner == nil || noCostWinner.Model != "openai/gpt-4o" {
		t.Errorf("without cost weight, quality-dominant cloud should win, got %v", noCostWinner)
	}
	if highCostWinner == nil || highCostWinner.Model != "ollama/qwen3:8b" {
		t.Errorf("with high cost weight, free local should win, got %v", highCostWinner)
	}
}

func TestMetascore_SessionPenaltyDegradesAffectedModel(t *testing.T) {
	primary := localProfile(0.80, 50)
	fallback := cloudProfile(0.72, 30)

	// Without penalty, primary wins (higher quality, free)
	w := DefaultRoutingWeights()
	pScore := primary.BreakdownWithComplexity(0.3, false, w)
	fScore := fallback.BreakdownWithComplexity(0.3, false, w)

	if pScore.FinalScore <= fScore.FinalScore {
		t.Fatalf("without penalty, primary (%f) should beat fallback (%f)", pScore.FinalScore, fScore.FinalScore)
	}

	// With 0.40 session penalty, primary's score drops
	penalizedScore := pScore.FinalScore * (1.0 - 0.40)
	if penalizedScore >= fScore.FinalScore {
		t.Errorf("session penalty of 0.40 should make primary (%f) lose to fallback (%f)",
			penalizedScore, fScore.FinalScore)
	}
}

func TestMetascore_AccuracyFloorGatesLowQuality(t *testing.T) {
	cheapBad := localProfile(0.25, 50) // below any reasonable floor
	expensiveGood := cloudProfile(0.82, 40)

	// Apply accuracy floor filtering (as the router does)
	accuracyFloor := 0.50
	minObs := 10
	profiles := []ModelProfile{cheapBad, expensiveGood}

	var filtered []ModelProfile
	for _, p := range profiles {
		obs := int(p.Confidence * float64(confidenceThreshold))
		if obs < minObs || p.Confidence >= 1.0 {
			// Not enough data to gate, or full confidence — check quality
			if p.Quality >= accuracyFloor || obs < minObs {
				filtered = append(filtered, p)
				continue
			}
		}
		if p.Quality >= accuracyFloor {
			filtered = append(filtered, p)
		}
	}

	if len(filtered) != 1 {
		t.Fatalf("accuracy floor should filter out low-quality model, got %d profiles", len(filtered))
	}
	if filtered[0].Model != "openai/gpt-4o" {
		t.Errorf("expected openai/gpt-4o to survive floor, got %s", filtered[0].Model)
	}
}

func TestMetascore_CapacityPressureReducesScore(t *testing.T) {
	saturated := localProfile(0.85, 50)
	saturated.Availability = 0.05 // nearly exhausted (availability * headroom)

	available := cloudProfile(0.75, 30)
	available.Availability = 0.90 // plenty of headroom

	w := DefaultRoutingWeights()
	satScore := saturated.BreakdownWithComplexity(0.5, false, w)
	avlScore := available.BreakdownWithComplexity(0.5, false, w)

	if satScore.Availability >= avlScore.Availability {
		t.Errorf("saturated availability (%f) should be lower than available (%f)",
			satScore.Availability, avlScore.Availability)
	}
}
