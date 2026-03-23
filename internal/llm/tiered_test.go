package llm

import (
	"testing"
	"time"
)

func TestConfidenceEvaluator_HighConfidence(t *testing.T) {
	ce := NewConfidenceEvaluator(0.7)
	// Long, non-hedging, fast, proper ending.
	content := "The capital of France is Paris. It has been the capital since the 10th century and is known for its rich history and culture."
	score := ce.ConfidenceScore(content, 500*time.Millisecond)
	if score < 0.7 {
		t.Errorf("high confidence score = %f, want >= 0.7", score)
	}
	if !ce.IsConfident(content, 500*time.Millisecond) {
		t.Error("should be confident")
	}
}

func TestConfidenceEvaluator_LowConfidenceHedging(t *testing.T) {
	ce := NewConfidenceEvaluator(0.7)
	content := "I'm not sure, but I think maybe it's possibly Paris? I'm uncertain about this."
	score := ce.ConfidenceScore(content, 8*time.Second)
	if score >= 0.7 {
		t.Errorf("hedging score = %f, want < 0.7", score)
	}
	if ce.IsConfident(content, 8*time.Second) {
		t.Error("should not be confident with hedging + slow latency")
	}
}

func TestConfidenceEvaluator_EmptyResponse(t *testing.T) {
	ce := NewConfidenceEvaluator(0.7)
	// Empty response: lengthScore=0.1, hedgingScore=1.0, latencyScore varies, structureScore=0.0.
	// With fast latency the score is ~0.525 — below the 0.7 confidence floor.
	if ce.IsConfident("", 100*time.Millisecond) {
		t.Error("empty response should not be confident at 0.7 floor")
	}
}

func TestConfidenceEvaluator_ShortResponse(t *testing.T) {
	ce := NewConfidenceEvaluator(0.7)
	score := ce.ConfidenceScore("Yes.", 100*time.Millisecond)
	// Short but fast and ends properly — mixed signals.
	if score <= 0 || score >= 1 {
		t.Errorf("short response score = %f, want in (0, 1)", score)
	}
}

func TestConfidenceEvaluator_CodeBlock(t *testing.T) {
	ce := NewConfidenceEvaluator(0.5)
	content := "Here's the solution:\n```go\nfmt.Println(\"hello\")\n```"
	score := ce.ConfidenceScore(content, 1*time.Second)
	if score < 0.5 {
		t.Errorf("code block score = %f, want >= 0.5", score)
	}
}

func TestConfidenceEvaluator_CustomFloor(t *testing.T) {
	ce := NewConfidenceEvaluator(0)
	if ce.ConfidenceFloor != 0.7 {
		t.Errorf("invalid floor should default to 0.7, got %f", ce.ConfidenceFloor)
	}
	ce2 := NewConfidenceEvaluator(2.0)
	if ce2.ConfidenceFloor != 0.7 {
		t.Errorf("invalid floor should default to 0.7, got %f", ce2.ConfidenceFloor)
	}
}

func TestEscalationTracker(t *testing.T) {
	et := NewEscalationTracker()
	et.RecordCacheHit()
	et.RecordCacheHit()
	et.RecordLocalAccepted()
	et.RecordLocalEscalated()
	et.RecordCloudDirect()

	stats := et.Stats()
	if stats["cache_hits"] != 2 {
		t.Errorf("cache_hits = %d, want 2", stats["cache_hits"])
	}
	if stats["local_accepted"] != 1 {
		t.Errorf("local_accepted = %d, want 1", stats["local_accepted"])
	}

	// Acceptance rate: 1 accepted / (1 accepted + 1 escalated) = 0.5.
	if rate := et.LocalAcceptanceRate(); rate != 0.5 {
		t.Errorf("acceptance rate = %f, want 0.5", rate)
	}

	// Cache hit rate: 2 / (2 + 1 + 1 + 1) = 0.4.
	if rate := et.CacheHitRate(); rate != 0.4 {
		t.Errorf("cache hit rate = %f, want 0.4", rate)
	}
}

func TestEscalationTracker_Empty(t *testing.T) {
	et := NewEscalationTracker()
	if rate := et.LocalAcceptanceRate(); rate != 0 {
		t.Errorf("empty acceptance rate = %f, want 0", rate)
	}
	if rate := et.CacheHitRate(); rate != 0 {
		t.Errorf("empty cache hit rate = %f, want 0", rate)
	}
}
