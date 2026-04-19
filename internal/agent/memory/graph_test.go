package memory

import (
	"context"
	"strings"
	"testing"

	"roboticus/internal/db"
	"roboticus/testutil"
)

func buildTestFacts() []GraphFactRow {
	// A --depends_on--> B --depends_on--> C --depends_on--> D
	// X --depends_on--> B
	// Y --blocks--> D
	return []GraphFactRow{
		{ID: "1", Subject: "A", Relation: "depends_on", Object: "B", Confidence: 0.9},
		{ID: "2", Subject: "B", Relation: "depends_on", Object: "C", Confidence: 0.9},
		{ID: "3", Subject: "C", Relation: "depends_on", Object: "D", Confidence: 0.8},
		{ID: "4", Subject: "X", Relation: "depends_on", Object: "B", Confidence: 0.7},
		{ID: "5", Subject: "Y", Relation: "blocks", Object: "D", Confidence: 0.6},
		{ID: "6", Subject: "Z", Relation: "mentions", Object: "D", Confidence: 0.3}, // non-traversable
	}
}

func TestKnowledgeGraph_NodeAndEdgeCounts(t *testing.T) {
	g := NewKnowledgeGraph(buildTestFacts())
	if g.NodeCount() != 7 {
		t.Fatalf("expected 7 distinct nodes, got %d", g.NodeCount())
	}
	// Only 5 of 6 facts are traversable (the mention relation is skipped).
	if g.EdgeCount() != 5 {
		t.Fatalf("expected 5 traversable edges, got %d", g.EdgeCount())
	}
	if !g.HasNode("a") || !g.HasNode("Z") {
		t.Fatalf("expected case-insensitive node lookup")
	}
}

func TestKnowledgeGraph_ShortestPath_MultiHop(t *testing.T) {
	g := NewKnowledgeGraph(buildTestFacts())

	// A -> B -> C -> D is three hops; depth cap 3 should find it.
	path := g.ShortestPath("A", "D", 3)
	if len(path) != 3 {
		t.Fatalf("expected 3-hop path A->D, got %+v", path)
	}

	// Depth cap below 3 should not find it.
	shorter := g.ShortestPath("A", "D", 2)
	if shorter != nil {
		t.Fatalf("expected no path under depth 2, got %+v", shorter)
	}

	// No path case.
	none := g.ShortestPath("A", "Nonexistent", 5)
	if none != nil {
		t.Fatalf("expected nil path for missing node, got %+v", none)
	}

	// Self path returns nil.
	self := g.ShortestPath("A", "A", 3)
	if self != nil {
		t.Fatalf("expected nil for self path, got %+v", self)
	}
}

func TestKnowledgeGraph_Impact_MultiHop(t *testing.T) {
	g := NewKnowledgeGraph(buildTestFacts())

	// Who depends on C? B transitively does (B depends_on C), and everything
	// that depends on B (A, X) transitively depends on C too.
	paths := g.Impact("C", 3)
	endpoints := pathEndpoints(paths)
	if !endpoints["B"] {
		t.Fatalf("expected B in impacted-by-C set, got %+v", endpoints)
	}
	if !endpoints["A"] {
		t.Fatalf("expected A in impacted-by-C set, got %+v", endpoints)
	}
	if !endpoints["X"] {
		t.Fatalf("expected X in impacted-by-C set, got %+v", endpoints)
	}

	// Depth limit should cut off A and X (they are 2 hops away from C).
	shallow := g.Impact("C", 1)
	endpoints = pathEndpoints(shallow)
	if endpoints["A"] || endpoints["X"] {
		t.Fatalf("expected A and X cut off at depth 1, got %+v", endpoints)
	}
	if !endpoints["B"] {
		t.Fatalf("expected B at depth 1, got %+v", endpoints)
	}
}

func TestKnowledgeGraph_Dependencies_MultiHop(t *testing.T) {
	g := NewKnowledgeGraph(buildTestFacts())

	paths := g.Dependencies("A", 3)
	endpoints := pathEndpoints(paths)
	for _, node := range []string{"B", "C", "D"} {
		if !endpoints[node] {
			t.Fatalf("expected A dependency chain to include %s, got %+v", node, endpoints)
		}
	}
}

func TestKnowledgeGraph_IgnoresNonTraversableRelations(t *testing.T) {
	g := NewKnowledgeGraph(buildTestFacts())

	// Z "mentions" D — mentions is not traversable.
	paths := g.Impact("D", 3)
	endpoints := pathEndpoints(paths)
	if endpoints["Z"] {
		t.Fatalf("did not expect Z (mentions-only edge) in impacted-by-D set, got %+v", endpoints)
	}
	// Y "blocks" D is traversable.
	if !endpoints["Y"] {
		t.Fatalf("expected Y in impacted-by-D set via blocks edge, got %+v", endpoints)
	}
}

func TestLoadKnowledgeGraph_ReadsPersistedFacts(t *testing.T) {
	store := testutil.TempStore(t)
	ctx := context.Background()

	seed := []struct {
		id, subj, rel, obj string
	}{
		{"f1", "Billing", "depends_on", "Ledger"},
		{"f2", "Ledger", "depends_on", "Postgres"},
		{"f3", "Postgres", "version_of", "Postgres 15"},
	}
	for _, s := range seed {
		if _, err := store.ExecContext(ctx,
			`INSERT INTO knowledge_facts (id, subject, relation, object, source_table, source_id, confidence)
			 VALUES (?, ?, ?, ?, 'test', ?, 0.9)`,
			s.id, s.subj, s.rel, s.obj, s.id,
		); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	g, err := LoadKnowledgeGraph(ctx, store)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if g.EdgeCount() != 3 {
		t.Fatalf("expected 3 edges loaded, got %d", g.EdgeCount())
	}
	path := g.ShortestPath("Billing", "Postgres", 3)
	if len(path) != 2 {
		t.Fatalf("expected Billing->Ledger->Postgres, got %+v", path)
	}
	if !strings.EqualFold(path[len(path)-1].To, "Postgres") {
		t.Fatalf("expected path to end at Postgres, got %+v", path)
	}
}

func TestLoadKnowledgeGraph_HonorsLimit(t *testing.T) {
	store := testutil.TempStore(t)
	ctx := context.Background()

	for i := 0; i < 10; i++ {
		id := db.NewID()
		if _, err := store.ExecContext(ctx,
			`INSERT INTO knowledge_facts (id, subject, relation, object, source_table, source_id, confidence)
			 VALUES (?, ?, 'depends_on', 'Z', 'test', ?, 0.5)`,
			id, string(rune('A'+i)), id,
		); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	g, err := LoadKnowledgeGraphWithLimit(ctx, store, 3)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if g.EdgeCount() != 3 {
		t.Fatalf("expected 3 edges with limit, got %d", g.EdgeCount())
	}
}

func pathEndpoints(paths []GraphPath) map[string]bool {
	ends := make(map[string]bool)
	for _, p := range paths {
		ends[p.End()] = true
	}
	return ends
}
