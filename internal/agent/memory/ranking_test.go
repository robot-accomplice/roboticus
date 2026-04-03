package memory

import (
	"testing"
	"time"
)

var testNow = time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC)

func TestRanker_RankingOrderByDecay(t *testing.T) {
	ranker := NewRanker(7) // 7-day half-life

	memories := []Ranked{
		{Content: "old", Score: 1.0, Timestamp: testNow.Add(-14 * 24 * time.Hour)}, // 2 half-lives ago
		{Content: "recent", Score: 1.0, Timestamp: testNow.Add(-1 * 24 * time.Hour)},
		{Content: "medium", Score: 1.0, Timestamp: testNow.Add(-7 * 24 * time.Hour)}, // 1 half-life ago
	}

	ranked := ranker.Rank(memories, testNow)

	if ranked[0].Content != "recent" {
		t.Errorf("expected 'recent' to rank first, got %q", ranked[0].Content)
	}
	if ranked[1].Content != "medium" {
		t.Errorf("expected 'medium' to rank second, got %q", ranked[1].Content)
	}
	if ranked[2].Content != "old" {
		t.Errorf("expected 'old' to rank last, got %q", ranked[2].Content)
	}
}

func TestRanker_RecentScoresHigher(t *testing.T) {
	ranker := NewRanker(7)

	recent := Ranked{Content: "recent", Score: 1.0, Timestamp: testNow.Add(-1 * 24 * time.Hour)}
	old := Ranked{Content: "old", Score: 1.0, Timestamp: testNow.Add(-30 * 24 * time.Hour)}

	memories := []Ranked{recent, old}
	ranked := ranker.Rank(memories, testNow)

	if ranked[0].Score <= ranked[1].Score {
		t.Errorf("recent memory (%f) should score higher than old (%f)", ranked[0].Score, ranked[1].Score)
	}
}

func TestRanker_TopNLimits(t *testing.T) {
	ranker := NewRanker(7)

	memories := []Ranked{
		{Content: "a", Score: 1.0, Timestamp: testNow},
		{Content: "b", Score: 0.9, Timestamp: testNow},
		{Content: "c", Score: 0.8, Timestamp: testNow},
		{Content: "d", Score: 0.7, Timestamp: testNow},
		{Content: "e", Score: 0.6, Timestamp: testNow},
	}

	top3 := ranker.TopN(memories, testNow, 3)
	if len(top3) != 3 {
		t.Fatalf("expected 3 results from TopN(3), got %d", len(top3))
	}

	// n >= len should return all
	all := ranker.TopN(memories, testNow, 10)
	if len(all) != 5 {
		t.Fatalf("expected 5 results when n > len, got %d", len(all))
	}
}

func TestRanker_EmptyList(t *testing.T) {
	ranker := NewRanker(7)

	ranked := ranker.Rank(nil, testNow)
	if len(ranked) != 0 {
		t.Errorf("expected empty result for nil input, got %d", len(ranked))
	}

	topN := ranker.TopN(nil, testNow, 5)
	if len(topN) != 0 {
		t.Errorf("expected empty result for nil TopN input, got %d", len(topN))
	}
}

func TestRanker_ZeroHalfLifeDisablesDecay(t *testing.T) {
	ranker := NewRanker(0)

	memories := []Ranked{
		{Content: "high_score_old", Score: 0.9, Timestamp: testNow.Add(-365 * 24 * time.Hour)},
		{Content: "low_score_recent", Score: 0.1, Timestamp: testNow},
	}

	ranked := ranker.Rank(memories, testNow)

	// Without decay, raw scores should determine order
	if ranked[0].Content != "high_score_old" {
		t.Errorf("with zero half-life, higher raw score should rank first, got %q", ranked[0].Content)
	}
}

func TestRanker_NegativeHalfLifeDisablesDecay(t *testing.T) {
	ranker := NewRanker(-1)

	memories := []Ranked{
		{Content: "high", Score: 1.0, Timestamp: testNow.Add(-100 * 24 * time.Hour)},
		{Content: "low", Score: 0.1, Timestamp: testNow},
	}

	ranked := ranker.Rank(memories, testNow)

	if ranked[0].Content != "high" {
		t.Errorf("negative half-life should use raw scores, got %q first", ranked[0].Content)
	}
}

func TestRanker_HalfLifeDecayMath(t *testing.T) {
	ranker := NewRanker(7)

	// A memory exactly 7 days old should have ~half its original score
	mem := Ranked{Score: 1.0, Timestamp: testNow.Add(-7 * 24 * time.Hour)}
	memories := []Ranked{mem}
	ranked := ranker.Rank(memories, testNow)

	if ranked[0].Score < 0.49 || ranked[0].Score > 0.51 {
		t.Errorf("score after one half-life should be ~0.5, got %f", ranked[0].Score)
	}
}
