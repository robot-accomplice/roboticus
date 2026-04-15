// graph.go exposes a reusable in-memory graph over the persisted
// knowledge_facts store so callers beyond the retrieval tier (tools, agent
// loops, tests) can reason about dependencies, impact chains, and multi-hop
// paths without rebuilding BFS state every time.
//
// The API is intentionally small:
//
//	g, err := LoadKnowledgeGraph(ctx, store)
//	path := g.ShortestPath("Billing", "Ledger", 4)
//	impacted := g.Impact("Ledger", 3)   // who is affected when Ledger changes
//	dependencies := g.Dependencies("Billing", 3)
//
// The previous retrieval-tier graph walker rebuilt adjacency on every retrieval
// and capped BFS at depth 2. This module is the persisted, reusable surface
// the Milestone 5 roadmap asks for: multi-hop traversals over a named
// structure that consumers can share.

package memory

import (
	"context"
	"strings"

	"roboticus/internal/db"
)

// GraphFactRow is the persisted shape of a knowledge_facts row as consumed by
// the graph API. It mirrors the private graphFactRow type used by retrieval
// but is exported so other packages can construct graphs from their own
// sources.
type GraphFactRow struct {
	ID         string
	Subject    string
	Relation   string
	Object     string
	Confidence float64
	AgeDays    float64
}

// GraphEdge represents a traversed edge in the knowledge graph.
type GraphEdge struct {
	From string
	To   string
	Fact GraphFactRow
}

// GraphPath is a chain of edges from a starting node.
type GraphPath struct {
	Start string
	Edges []GraphEdge
}

// End returns the last node visited on the path. Empty path returns Start.
func (p GraphPath) End() string {
	if len(p.Edges) == 0 {
		return p.Start
	}
	return p.Edges[len(p.Edges)-1].To
}

// Depth returns the number of edges in the path.
func (p GraphPath) Depth() int { return len(p.Edges) }

// KnowledgeGraph is an in-memory adjacency structure built from knowledge_facts.
// It holds both forward (subject -> object) and reverse (object -> subject)
// adjacency so impact and dependency traversals both run in O(edges).
type KnowledgeGraph struct {
	forward   map[string][]GraphEdge
	reverse   map[string][]GraphEdge
	originals map[string]string // lowercase -> original casing
	facts     int
}

// NewKnowledgeGraph builds an in-memory graph from the given facts. Facts
// whose subject or object is empty are skipped. Only traversable relations
// (see IsTraversableRelation) participate in the adjacency — others are kept
// only for their nodes so name lookups still resolve.
func NewKnowledgeGraph(facts []GraphFactRow) *KnowledgeGraph {
	g := &KnowledgeGraph{
		forward:   make(map[string][]GraphEdge),
		reverse:   make(map[string][]GraphEdge),
		originals: make(map[string]string),
	}
	for _, fact := range facts {
		if fact.Subject == "" || fact.Object == "" {
			continue
		}
		subKey := strings.ToLower(fact.Subject)
		objKey := strings.ToLower(fact.Object)
		if _, ok := g.originals[subKey]; !ok {
			g.originals[subKey] = fact.Subject
		}
		if _, ok := g.originals[objKey]; !ok {
			g.originals[objKey] = fact.Object
		}
		if !IsTraversableRelation(fact.Relation) {
			continue
		}
		g.facts++
		forward := GraphEdge{From: fact.Subject, To: fact.Object, Fact: fact}
		reverse := GraphEdge{From: fact.Object, To: fact.Subject, Fact: fact}
		g.forward[subKey] = append(g.forward[subKey], forward)
		g.reverse[objKey] = append(g.reverse[objKey], reverse)
	}
	return g
}

// IsTraversableRelation returns true for relations that represent a directed
// dependency or causal edge in the knowledge graph. Non-traversable relations
// (e.g. "mentions") do not contribute to path or impact traversals.
func IsTraversableRelation(relation string) bool {
	switch relation {
	case "depends_on", "uses", "blocked_by", "blocks",
		"causes", "caused_by", "version_of", "owned_by":
		return true
	default:
		return false
	}
}

// NodeCount returns the number of distinct nodes in the graph (subjects or
// objects of at least one fact).
func (g *KnowledgeGraph) NodeCount() int { return len(g.originals) }

// EdgeCount returns the number of traversable edges loaded into the graph.
func (g *KnowledgeGraph) EdgeCount() int { return g.facts }

// HasNode reports whether the graph contains the given node name
// (case-insensitive).
func (g *KnowledgeGraph) HasNode(name string) bool {
	_, ok := g.originals[strings.ToLower(name)]
	return ok
}

// ShortestPath returns the shortest edge chain from `from` to `to` up to
// maxDepth hops. Returns nil if no path exists within the bound.
// The traversal treats edges as undirected so a path exists regardless of
// relation direction. Use TraversePath when direction matters.
func (g *KnowledgeGraph) ShortestPath(from, to string, maxDepth int) []GraphEdge {
	if g == nil || maxDepth <= 0 {
		return nil
	}
	fromKey := strings.ToLower(from)
	toKey := strings.ToLower(to)
	if fromKey == "" || toKey == "" {
		return nil
	}
	if fromKey == toKey {
		return nil
	}

	type state struct {
		Node  string
		Edges []GraphEdge
	}
	queue := []state{{Node: fromKey}}
	visited := map[string]struct{}{fromKey: {}}

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]

		for _, edge := range g.neighborsUndirected(current.Node) {
			nextKey := strings.ToLower(edge.To)
			if _, seen := visited[nextKey]; seen {
				continue
			}
			visited[nextKey] = struct{}{}
			nextEdges := append(append([]GraphEdge(nil), current.Edges...), edge)
			if nextKey == toKey {
				return nextEdges
			}
			if len(nextEdges) >= maxDepth {
				continue
			}
			queue = append(queue, state{Node: nextKey, Edges: nextEdges})
		}
	}
	return nil
}

// Impact returns every path from `seed` to any node that transitively
// depends on it, up to maxDepth hops. It walks reverse adjacency so edges
// like "A depends_on B" surface as "B impacts A" chains.
func (g *KnowledgeGraph) Impact(seed string, maxDepth int) []GraphPath {
	return g.traverse(seed, maxDepth, true)
}

// Dependencies returns every path from `seed` to the nodes it transitively
// depends on, up to maxDepth hops. It walks forward adjacency so edges like
// "A depends_on B" surface as "A -> B -> ..." chains.
func (g *KnowledgeGraph) Dependencies(seed string, maxDepth int) []GraphPath {
	return g.traverse(seed, maxDepth, false)
}

func (g *KnowledgeGraph) traverse(seed string, maxDepth int, reverse bool) []GraphPath {
	if g == nil || maxDepth <= 0 {
		return nil
	}
	seedKey := strings.ToLower(seed)
	if seedKey == "" {
		return nil
	}
	display, ok := g.originals[seedKey]
	if !ok {
		display = seed
	}

	type state struct {
		Node  string
		Depth int
		Edges []GraphEdge
	}
	queue := []state{{Node: seedKey}}
	visitedDepth := map[string]int{seedKey: 0}
	var paths []GraphPath

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		if current.Depth >= maxDepth {
			continue
		}
		var edges []GraphEdge
		if reverse {
			edges = g.reverse[current.Node]
		} else {
			edges = g.forward[current.Node]
		}
		for _, edge := range edges {
			nextKey := strings.ToLower(edge.To)
			depth := current.Depth + 1
			if prior, seen := visitedDepth[nextKey]; seen && prior <= depth {
				continue
			}
			visitedDepth[nextKey] = depth
			nextEdges := append(append([]GraphEdge(nil), current.Edges...), edge)
			paths = append(paths, GraphPath{Start: display, Edges: nextEdges})
			queue = append(queue, state{Node: nextKey, Depth: depth, Edges: nextEdges})
		}
	}
	return paths
}

func (g *KnowledgeGraph) neighborsUndirected(key string) []GraphEdge {
	forward := g.forward[key]
	reverse := g.reverse[key]
	if len(forward) == 0 {
		return reverse
	}
	if len(reverse) == 0 {
		return forward
	}
	combined := make([]GraphEdge, 0, len(forward)+len(reverse))
	combined = append(combined, forward...)
	combined = append(combined, reverse...)
	return combined
}

// LoadKnowledgeGraph reads every knowledge_fact row from the store and returns
// a ready-to-traverse graph. Callers that only need a bounded subset (e.g.,
// the retrieval tier loading LIMIT 200) should use LoadKnowledgeGraphWithLimit.
func LoadKnowledgeGraph(ctx context.Context, store *db.Store) (*KnowledgeGraph, error) {
	return loadGraph(ctx, store, 0)
}

// LoadKnowledgeGraphWithLimit reads up to maxFacts rows, ordered by most
// recent update. Pass 0 to load all facts.
func LoadKnowledgeGraphWithLimit(ctx context.Context, store *db.Store, maxFacts int) (*KnowledgeGraph, error) {
	return loadGraph(ctx, store, maxFacts)
}

func loadGraph(ctx context.Context, store *db.Store, maxFacts int) (*KnowledgeGraph, error) {
	if store == nil {
		return NewKnowledgeGraph(nil), nil
	}
	query := `SELECT id, subject, relation, object, confidence,
		 julianday('now') - julianday(updated_at) AS age_days
		 FROM knowledge_facts
		 ORDER BY updated_at DESC, confidence DESC`
	var (
		rows interface {
			Next() bool
			Scan(...any) error
			Close() error
			Err() error
		}
		err error
	)
	if maxFacts > 0 {
		rows, err = store.QueryContext(ctx, query+" LIMIT ?", maxFacts)
	} else {
		rows, err = store.QueryContext(ctx, query)
	}
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var facts []GraphFactRow
	for rows.Next() {
		var fact GraphFactRow
		if err := rows.Scan(&fact.ID, &fact.Subject, &fact.Relation, &fact.Object, &fact.Confidence, &fact.AgeDays); err != nil {
			continue
		}
		facts = append(facts, fact)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return NewKnowledgeGraph(facts), nil
}
