package llm

import (
	"strings"
	"sync/atomic"
	"time"
)

// InferenceTier represents where an inference was resolved.
type InferenceTier int

const (
	TierCache InferenceTier = iota
	TierLocal
	TierCloud
)

func (t InferenceTier) String() string {
	switch t {
	case TierCache:
		return "cache"
	case TierLocal:
		return "local"
	case TierCloud:
		return "cloud"
	default:
		return "unknown"
	}
}

// ConfidenceEvaluator scores a local model's response to decide if it's good enough
// or should be escalated to a stronger cloud model.
type ConfidenceEvaluator struct {
	ConfidenceFloor float64
}

// NewConfidenceEvaluator creates an evaluator with the given confidence floor.
func NewConfidenceEvaluator(floor float64) *ConfidenceEvaluator {
	if floor <= 0 || floor > 1 {
		floor = 0.7
	}
	return &ConfidenceEvaluator{ConfidenceFloor: floor}
}

// ConfidenceScore evaluates four signals from a response:
// 1. Response length (very short = low confidence)
// 2. Hedging phrases (uncertainty markers)
// 3. Latency (very slow = model struggling)
// 4. Structure (ends with proper sentence = higher confidence)
func (ce *ConfidenceEvaluator) ConfidenceScore(content string, latency time.Duration) float64 {
	scores := [4]float64{
		ce.lengthScore(content),
		ce.hedgingScore(content),
		ce.latencyScore(latency),
		ce.structureScore(content),
	}

	var sum float64
	for _, s := range scores {
		sum += s
	}
	return sum / float64(len(scores))
}

// IsConfident returns true if the response meets the confidence floor.
func (ce *ConfidenceEvaluator) IsConfident(content string, latency time.Duration) bool {
	return ce.ConfidenceScore(content, latency) >= ce.ConfidenceFloor
}

func (ce *ConfidenceEvaluator) lengthScore(content string) float64 {
	n := len(content)
	switch {
	case n < 10:
		return 0.1
	case n < 50:
		return 0.4
	case n < 200:
		return 0.7
	case n <= 1000:
		return 0.85 // 5th length bucket: 201-1000 chars
	default:
		return 1.0
	}
}

func (ce *ConfidenceEvaluator) hedgingScore(content string) float64 {
	hedges := []string{
		"i'm not sure", "i don't know", "i'm uncertain",
		"i cannot determine", "it's unclear", "i'm not confident",
		"possibly", "perhaps", "maybe", "might be",
		"i think", "i believe", "it seems",
	}

	lower := strings.ToLower(content)
	count := 0
	for _, h := range hedges {
		if strings.Contains(lower, h) {
			count++
		}
	}

	switch count {
	case 0:
		return 1.0
	case 1:
		return 0.7
	case 2:
		return 0.4
	default:
		return 0.1
	}
}

func (ce *ConfidenceEvaluator) latencyScore(latency time.Duration) float64 {
	ms := latency.Milliseconds()
	switch {
	case ms < 200:
		return 1.0 // fast threshold tightened from 1000ms
	case ms < 1000:
		return 0.9
	case ms < 3000:
		return 0.8
	case ms < 10000:
		return 0.5
	default:
		return 0.2
	}
}

func (ce *ConfidenceEvaluator) structureScore(content string) float64 {
	trimmed := strings.TrimSpace(content)
	if len(trimmed) == 0 {
		return 0.0
	}
	lastChar := trimmed[len(trimmed)-1]
	// Proper sentence ending.
	if lastChar == '.' || lastChar == '!' || lastChar == '?' || lastChar == '"' || lastChar == ')' || lastChar == '}' {
		return 1.0
	}
	// Code block ending.
	if strings.HasSuffix(trimmed, "```") {
		return 1.0
	}
	return 0.5
}

// EscalationTracker records tier-level inference statistics.
type EscalationTracker struct {
	cacheHits      atomic.Int64
	localAccepted  atomic.Int64
	localEscalated atomic.Int64
	cloudDirect    atomic.Int64
}

// NewEscalationTracker creates a new tracker.
func NewEscalationTracker() *EscalationTracker {
	return &EscalationTracker{}
}

// RecordCacheHit increments the cache hit counter.
func (et *EscalationTracker) RecordCacheHit() { et.cacheHits.Add(1) }

// RecordLocalAccepted increments the local-accepted counter.
func (et *EscalationTracker) RecordLocalAccepted() { et.localAccepted.Add(1) }

// RecordLocalEscalated increments the local-escalated counter.
func (et *EscalationTracker) RecordLocalEscalated() { et.localEscalated.Add(1) }

// RecordCloudDirect increments the cloud-direct counter.
func (et *EscalationTracker) RecordCloudDirect() { et.cloudDirect.Add(1) }

// LocalAcceptanceRate returns the fraction of local inferences accepted without escalation.
func (et *EscalationTracker) LocalAcceptanceRate() float64 {
	accepted := et.localAccepted.Load()
	escalated := et.localEscalated.Load()
	total := accepted + escalated
	if total == 0 {
		return 0
	}
	return float64(accepted) / float64(total)
}

// CacheHitRate returns the fraction of requests served from cache.
func (et *EscalationTracker) CacheHitRate() float64 {
	hits := et.cacheHits.Load()
	total := hits + et.localAccepted.Load() + et.localEscalated.Load() + et.cloudDirect.Load()
	if total == 0 {
		return 0
	}
	return float64(hits) / float64(total)
}

// Stats returns a snapshot of all counters.
func (et *EscalationTracker) Stats() map[string]int64 {
	return map[string]int64{
		"cache_hits":      et.cacheHits.Load(),
		"local_accepted":  et.localAccepted.Load(),
		"local_escalated": et.localEscalated.Load(),
		"cloud_direct":    et.cloudDirect.Load(),
	}
}
