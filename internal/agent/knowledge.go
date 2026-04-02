package agent

import (
	"context"
	"fmt"
	"sync"
)

// Fact represents a single structured knowledge triple.
type Fact struct {
	ID         string
	Subject    string
	Relation   string
	Object     string
	Source     string
	Confidence float64
}

// KnowledgeGraph stores structured facts and supports subject/relation queries.
type KnowledgeGraph struct {
	mu    sync.RWMutex
	facts map[string]*Fact   // keyed by ID
	index map[string][]*Fact // keyed by subject
}

// NewKnowledgeGraph creates an empty KnowledgeGraph.
func NewKnowledgeGraph() *KnowledgeGraph {
	return &KnowledgeGraph{facts: make(map[string]*Fact), index: make(map[string][]*Fact)}
}

// AddFact inserts a fact into the graph.
func (kg *KnowledgeGraph) AddFact(f Fact) {
	kg.mu.Lock()
	defer kg.mu.Unlock()
	kg.facts[f.ID] = &f
	kg.index[f.Subject] = append(kg.index[f.Subject], &f)
}

// QueryBySubject returns all facts whose Subject matches.
func (kg *KnowledgeGraph) QueryBySubject(subject string) []Fact {
	kg.mu.RLock()
	defer kg.mu.RUnlock()
	var result []Fact
	for _, f := range kg.index[subject] {
		result = append(result, *f)
	}
	return result
}

// QueryByRelation returns all facts whose Relation matches.
func (kg *KnowledgeGraph) QueryByRelation(relation string) []Fact {
	kg.mu.RLock()
	defer kg.mu.RUnlock()
	var result []Fact
	for _, f := range kg.facts {
		if f.Relation == relation {
			result = append(result, *f)
		}
	}
	return result
}

// FactCount returns the total number of facts in the graph.
func (kg *KnowledgeGraph) FactCount() int {
	kg.mu.RLock()
	defer kg.mu.RUnlock()
	return len(kg.facts)
}

// FormatForPrompt renders facts about a subject as a string within a token budget.
// budget is approximate: we compare character count against budget*4 (chars per token).
func (kg *KnowledgeGraph) FormatForPrompt(subject string, budget int) string {
	facts := kg.QueryBySubject(subject)
	if len(facts) == 0 {
		return ""
	}
	var result string
	for _, f := range facts {
		line := fmt.Sprintf("- %s %s %s (confidence: %.1f)\n", f.Subject, f.Relation, f.Object, f.Confidence)
		if len(result)+len(line) > budget*4 {
			break
		}
		result += line
	}
	return result
}

// knowledgeContextKey is a private key type for context values.
type knowledgeContextKey struct{}

// WithKnowledgeGraph stores a KnowledgeGraph in a context.
func WithKnowledgeGraph(ctx context.Context, kg *KnowledgeGraph) context.Context {
	return context.WithValue(ctx, knowledgeContextKey{}, kg)
}

// KnowledgeGraphFromContext retrieves a KnowledgeGraph from a context, or nil.
func KnowledgeGraphFromContext(ctx context.Context) *KnowledgeGraph {
	kg, _ := ctx.Value(knowledgeContextKey{}).(*KnowledgeGraph)
	return kg
}
