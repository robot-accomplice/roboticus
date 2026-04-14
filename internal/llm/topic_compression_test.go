package llm

import (
	"strings"
	"testing"
)

func TestCompressWithTopicAwareness_FitsInBudget(t *testing.T) {
	msgs := []Message{
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "hi there"},
	}
	result := CompressWithTopicAwareness(msgs, 10000)
	if len(result) != 2 {
		t.Errorf("expected 2 messages when budget is ample, got %d", len(result))
	}
}

func TestCompressWithTopicAwareness_PreservesSystemMessages(t *testing.T) {
	msgs := []Message{
		{Role: "system", Content: "You are a helpful assistant."},
		{Role: "user", Content: "Tell me about databases and indexes and query optimization."},
		{Role: "assistant", Content: "Databases use indexes for query optimization and performance."},
		{Role: "user", Content: "Now tell me about cooking recipes and ingredients."},
		{Role: "assistant", Content: "Cooking involves combining ingredients following recipe instructions."},
	}
	// Use a tight budget that forces compression.
	result := CompressWithTopicAwareness(msgs, 50)
	// System message should be preserved.
	hasSystem := false
	for _, m := range result {
		if m.Role == "system" {
			hasSystem = true
		}
	}
	if !hasSystem {
		t.Error("system message should be preserved")
	}
}

func TestCompressWithTopicAwareness_KeepsRecentTopicExpanded(t *testing.T) {
	// Create two distinct topic groups.
	msgs := []Message{
		{Role: "user", Content: "Let me ask about database indexes and query performance optimization."},
		{Role: "assistant", Content: "Database indexes help with query performance optimization and speed."},
		// Topic shift
		{Role: "user", Content: "Now let us discuss cooking recipes and food ingredients preparation."},
		{Role: "assistant", Content: "Cooking recipes involve combining food ingredients with preparation techniques."},
	}

	// Budget that's tight but allows the recent topic to survive.
	budget := 80
	result := CompressWithTopicAwareness(msgs, budget)

	if len(result) == 0 {
		t.Fatal("expected at least some messages")
	}

	// The last message (most recent topic) should be present and not heavily compressed.
	last := result[len(result)-1]
	if !strings.Contains(last.Content, "cooking") && !strings.Contains(last.Content, "Cooking") &&
		!strings.Contains(strings.ToLower(last.Content), "recipe") {
		t.Errorf("most recent topic should be preserved, got: %q", last.Content)
	}
}

func TestCompressWithTopicAwareness_Empty(t *testing.T) {
	result := CompressWithTopicAwareness(nil, 100)
	if len(result) != 0 {
		t.Errorf("expected nil/empty for nil input, got %d", len(result))
	}
}

func TestCompressWithTopicAwareness_ZeroBudget(t *testing.T) {
	msgs := []Message{{Role: "user", Content: "hello"}}
	result := CompressWithTopicAwareness(msgs, 0)
	if len(result) != 0 {
		t.Errorf("expected nil/empty for zero budget, got %d", len(result))
	}
}

func TestGroupByTopic(t *testing.T) {
	msgs := []Message{
		{Role: "user", Content: "database indexes performance optimization queries"},
		{Role: "assistant", Content: "indexes help database query optimization performance"},
		// Different topic
		{Role: "user", Content: "cooking recipes ingredients kitchen preparation"},
		{Role: "assistant", Content: "kitchen recipes need fresh ingredients for preparation"},
	}

	groups := groupByTopic(msgs, 0.15)

	if len(groups) < 2 {
		t.Errorf("expected at least 2 topic groups, got %d", len(groups))
	}
}

func TestJaccardSimilarity(t *testing.T) {
	a := map[string]bool{"database": true, "index": true, "query": true}
	b := map[string]bool{"database": true, "index": true, "performance": true}

	sim := jaccardSimilarity(a, b)
	// Intersection: 2, Union: 4 → 0.5
	if sim < 0.49 || sim > 0.51 {
		t.Errorf("expected ~0.5, got %f", sim)
	}

	// Empty sets.
	if jaccardSimilarity(nil, nil) != 1.0 {
		t.Error("empty sets should have similarity 1.0")
	}
	if jaccardSimilarity(a, nil) != 0.0 {
		t.Error("one empty set should have similarity 0.0")
	}
}

func TestExtractKeywords(t *testing.T) {
	kw := extractKeywords("The quick brown fox jumps over the lazy dog and database optimization")
	// "the", "and", "over" are stop words or too short; "quick", "brown", "jumps", "lazy" are 5+ letters
	if !kw["quick"] {
		t.Error("expected 'quick' in keywords")
	}
	if !kw["database"] {
		t.Error("expected 'database' in keywords")
	}
	if kw["the"] {
		t.Error("'the' should be filtered as stop word")
	}
	if kw["fox"] {
		t.Error("'fox' should be filtered (3 chars)")
	}
}
