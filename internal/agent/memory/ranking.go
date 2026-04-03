package memory

import (
	"math"
	"sort"
	"time"
)

// Ranked is a memory candidate that will be scored and sorted.
type Ranked struct {
	Content   string
	Score     float64
	Timestamp time.Time
	Tier      string
}

// Ranker scores memories using exponential recency decay.
type Ranker struct {
	decayHalfLifeDays float64
}

// NewRanker creates a ranker with the given half-life in days.
// A half-life of 0 or negative disables decay (raw scores are used).
func NewRanker(halfLifeDays float64) *Ranker {
	return &Ranker{decayHalfLifeDays: halfLifeDays}
}

// Rank applies recency decay to every memory and returns them sorted
// highest-score first. The input slice is modified in place.
func (mr *Ranker) Rank(memories []Ranked, now time.Time) []Ranked {
	for i := range memories {
		memories[i].Score = mr.decayScore(memories[i], now)
	}
	sort.Slice(memories, func(i, j int) bool {
		return memories[i].Score > memories[j].Score
	})
	return memories
}

func (mr *Ranker) decayScore(m Ranked, now time.Time) float64 {
	if mr.decayHalfLifeDays <= 0 {
		return m.Score
	}
	age := now.Sub(m.Timestamp).Hours() / 24.0
	decay := math.Pow(0.5, age/mr.decayHalfLifeDays)
	return m.Score * decay
}

// TopN ranks memories and returns up to n results.
func (mr *Ranker) TopN(memories []Ranked, now time.Time, n int) []Ranked {
	ranked := mr.Rank(memories, now)
	if n >= len(ranked) {
		return ranked
	}
	return ranked[:n]
}
