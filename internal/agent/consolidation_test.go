package agent

import (
	"testing"
)

func TestConsolidator_SingleEntryPassthrough(t *testing.T) {
	c := NewConsolidator(0.5)
	entries := []ConsolidationEntry{{ID: "1", Content: "hello world", Category: "fact", Score: 1.0}}
	result := c.Consolidate(entries)
	if len(result) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(result))
	}
	if result[0].ID != "1" {
		t.Errorf("expected ID '1', got %q", result[0].ID)
	}
}

func TestConsolidator_TwoSimilarEntriesMerged(t *testing.T) {
	c := NewConsolidator(0.5)
	entries := []ConsolidationEntry{
		{ID: "1", Content: "the cat sat on the mat", Category: "fact", Score: 0.8},
		{ID: "2", Content: "the cat sat on the mat today", Category: "fact", Score: 0.9},
	}
	result := c.Consolidate(entries)
	if len(result) != 1 {
		t.Fatalf("expected 1 merged entry, got %d", len(result))
	}
	// Higher-scored entry content should win
	if result[0].Score != 0.9 {
		t.Errorf("expected merged score 0.9, got %f", result[0].Score)
	}
}

func TestConsolidator_DifferentCategoriesNotMerged(t *testing.T) {
	c := NewConsolidator(0.3)
	entries := []ConsolidationEntry{
		{ID: "1", Content: "the cat sat on the mat", Category: "episodic", Score: 0.8},
		{ID: "2", Content: "the cat sat on the mat", Category: "semantic", Score: 0.9},
	}
	result := c.Consolidate(entries)
	if len(result) != 2 {
		t.Fatalf("expected 2 entries (different categories), got %d", len(result))
	}
}

func TestConsolidator_DissimilarEntriesNotMerged(t *testing.T) {
	c := NewConsolidator(0.8) // high threshold
	entries := []ConsolidationEntry{
		{ID: "1", Content: "apple banana cherry", Category: "fact", Score: 0.5},
		{ID: "2", Content: "dog cat mouse", Category: "fact", Score: 0.6},
	}
	result := c.Consolidate(entries)
	if len(result) != 2 {
		t.Fatalf("expected 2 entries (below similarity threshold), got %d", len(result))
	}
}

func TestConsolidator_EmptyInput(t *testing.T) {
	c := NewConsolidator(0.5)
	result := c.Consolidate(nil)
	if len(result) != 0 {
		t.Errorf("expected empty result for nil input, got %d", len(result))
	}
}

func TestJaccardSimilarity_Boundaries(t *testing.T) {
	// Identical strings → 1.0
	if s := jaccardSimilarity("hello world", "hello world"); s != 1.0 {
		t.Errorf("identical strings should have similarity 1.0, got %f", s)
	}
	// Completely different words → 0.0
	if s := jaccardSimilarity("alpha beta", "gamma delta"); s != 0.0 {
		t.Errorf("disjoint strings should have similarity 0.0, got %f", s)
	}
	// Both empty → 1.0
	if s := jaccardSimilarity("", ""); s != 1.0 {
		t.Errorf("two empty strings should have similarity 1.0, got %f", s)
	}
	// Partial overlap
	s := jaccardSimilarity("a b c", "b c d")
	if s <= 0 || s >= 1 {
		t.Errorf("partial overlap should be between 0 and 1, got %f", s)
	}
}

func TestConsolidator_MultipleGroupsMerged(t *testing.T) {
	c := NewConsolidator(0.5)
	entries := []ConsolidationEntry{
		{ID: "1", Content: "the cat sat on the mat", Category: "fact", Score: 0.5},
		{ID: "2", Content: "the cat sat on the mat", Category: "fact", Score: 0.9},
		{ID: "3", Content: "totally unrelated content here", Category: "fact", Score: 0.7},
	}
	result := c.Consolidate(entries)
	// Entries 1 and 2 should merge, entry 3 stays separate
	if len(result) != 2 {
		t.Fatalf("expected 2 groups, got %d", len(result))
	}
}
