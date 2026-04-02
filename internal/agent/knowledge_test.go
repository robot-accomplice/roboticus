package agent

import (
	"strings"
	"testing"
)

func TestKnowledgeGraph_AddAndQueryBySubject(t *testing.T) {
	kg := NewKnowledgeGraph()

	kg.AddFact(Fact{ID: "1", Subject: "Go", Relation: "is", Object: "a language", Source: "wiki", Confidence: 0.99})
	kg.AddFact(Fact{ID: "2", Subject: "Go", Relation: "has", Object: "goroutines", Source: "docs", Confidence: 0.95})
	kg.AddFact(Fact{ID: "3", Subject: "Python", Relation: "is", Object: "a language", Source: "wiki", Confidence: 0.99})

	goFacts := kg.QueryBySubject("Go")
	if len(goFacts) != 2 {
		t.Fatalf("expected 2 facts for 'Go', got %d", len(goFacts))
	}

	pyFacts := kg.QueryBySubject("Python")
	if len(pyFacts) != 1 {
		t.Fatalf("expected 1 fact for 'Python', got %d", len(pyFacts))
	}

	if kg.FactCount() != 3 {
		t.Fatalf("expected FactCount 3, got %d", kg.FactCount())
	}
}

func TestKnowledgeGraph_QueryByRelation(t *testing.T) {
	kg := NewKnowledgeGraph()

	kg.AddFact(Fact{ID: "1", Subject: "Go", Relation: "is", Object: "a language", Confidence: 0.9})
	kg.AddFact(Fact{ID: "2", Subject: "Python", Relation: "is", Object: "a language", Confidence: 0.9})
	kg.AddFact(Fact{ID: "3", Subject: "Go", Relation: "has", Object: "goroutines", Confidence: 0.95})

	isFacts := kg.QueryByRelation("is")
	if len(isFacts) != 2 {
		t.Fatalf("expected 2 'is' facts, got %d", len(isFacts))
	}

	hasFacts := kg.QueryByRelation("has")
	if len(hasFacts) != 1 {
		t.Fatalf("expected 1 'has' fact, got %d", len(hasFacts))
	}

	noneFacts := kg.QueryByRelation("unknown_relation")
	if len(noneFacts) != 0 {
		t.Fatalf("expected 0 facts for unknown relation, got %d", len(noneFacts))
	}
}

func TestKnowledgeGraph_FormatForPrompt(t *testing.T) {
	kg := NewKnowledgeGraph()

	for i := 0; i < 50; i++ {
		kg.AddFact(Fact{
			ID:         string(rune('a' + i)),
			Subject:    "subject",
			Relation:   "has",
			Object:     "a very long object value that takes up many characters in the output",
			Confidence: 0.8,
		})
	}

	// Small budget should truncate output
	small := kg.FormatForPrompt("subject", 5)
	large := kg.FormatForPrompt("subject", 500)

	if len(small) >= len(large) {
		t.Errorf("small budget (%d chars) should produce less output than large budget (%d chars)", len(small), len(large))
	}
}

func TestKnowledgeGraph_FormatForPromptEmpty(t *testing.T) {
	kg := NewKnowledgeGraph()

	result := kg.FormatForPrompt("nonexistent", 100)
	if result != "" {
		t.Errorf("expected empty string for unknown subject, got %q", result)
	}
}

func TestKnowledgeGraph_FormatForPromptContent(t *testing.T) {
	kg := NewKnowledgeGraph()
	kg.AddFact(Fact{ID: "1", Subject: "Alice", Relation: "knows", Object: "Bob", Confidence: 1.0})

	result := kg.FormatForPrompt("Alice", 100)
	if !strings.Contains(result, "Alice") || !strings.Contains(result, "Bob") {
		t.Errorf("expected fact content in output, got %q", result)
	}
	if !strings.Contains(result, "1.0") {
		t.Errorf("expected confidence in output, got %q", result)
	}
}

func TestKnowledgeGraph_EmptyGraph(t *testing.T) {
	kg := NewKnowledgeGraph()

	if kg.FactCount() != 0 {
		t.Errorf("new graph should have 0 facts")
	}
	if facts := kg.QueryBySubject("anything"); len(facts) != 0 {
		t.Errorf("empty graph QueryBySubject should return nil/empty")
	}
	if facts := kg.QueryByRelation("anything"); len(facts) != 0 {
		t.Errorf("empty graph QueryByRelation should return nil/empty")
	}
}
