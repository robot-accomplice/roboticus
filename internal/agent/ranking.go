package agent

import (
	"math"
	"sort"
	"time"
)

// RankedMemory is a memory candidate that will be scored and sorted.
type RankedMemory struct {
	Content   string
	Score     float64
	Timestamp time.Time
	Tier      string
}

// MemoryRanker scores memories using exponential recency decay.
type MemoryRanker struct {
	decayHalfLifeDays float64
}

// NewMemoryRanker creates a ranker with the given half-life in days.
// A half-life of 0 or negative disables decay (raw scores are used).
func NewMemoryRanker(halfLifeDays float64) *MemoryRanker {
	return &MemoryRanker{decayHalfLifeDays: halfLifeDays}
}

// Rank applies recency decay to every memory and returns them sorted
// highest-score first. The input slice is modified in place.
func (mr *MemoryRanker) Rank(memories []RankedMemory, now time.Time) []RankedMemory {
	for i := range memories {
		memories[i].Score = mr.decayScore(memories[i], now)
	}
	sort.Slice(memories, func(i, j int) bool {
		return memories[i].Score > memories[j].Score
	})
	return memories
}

func (mr *MemoryRanker) decayScore(m RankedMemory, now time.Time) float64 {
	if mr.decayHalfLifeDays <= 0 {
		return m.Score
	}
	age := now.Sub(m.Timestamp).Hours() / 24.0
	decay := math.Pow(0.5, age/mr.decayHalfLifeDays)
	return m.Score * decay
}

// TopN ranks memories and returns up to n results.
func (mr *MemoryRanker) TopN(memories []RankedMemory, now time.Time, n int) []RankedMemory {
	ranked := mr.Rank(memories, now)
	if n >= len(ranked) {
		return ranked
	}
	return ranked[:n]
}
