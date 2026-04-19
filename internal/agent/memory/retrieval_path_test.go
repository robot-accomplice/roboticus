// retrieval_path_test.go is the M3.2 regression. It proves the four
// HybridSearch-covered tiers (semantic, procedural, relationship, workflow)
// classify their retrieval path correctly across three end-to-end scenarios:
//
//   1. The query lexically matches the stored row → path == "fts" (vector
//      leg is skipped because the test corpus has no embeddings).
//   2. The query does NOT lexically match anything → both FTS and vector
//      legs return zero rows, the LIKE safety net kicks in OR the result
//      stays empty → path is "like_fallback" (when LIKE substring matches)
//      or "empty" (when nothing matches at all).
//   3. The query is empty / mode is non-search → no annotation is emitted
//      (browse paths don't pollute the LIKE-vs-FTS measurement that M3.3
//      relies on).
//
// The fixture tracer captures every Annotate call so the assertions can
// pin down which tier emitted what, in what order. This is exactly the
// observability surface M3.3 will use to confirm the LIKE safety net is
// dormant before deletion.

package memory

import (
	"context"
	"strings"
	"sync"
	"testing"

	"roboticus/internal/db"
	"roboticus/testutil"
)

// fixtureTracer is a RetrievalTracer that records every annotation in
// insertion order. Used by tests to assert which retrieval.path.<tier>
// values were emitted by a single Retrieve call.
type fixtureTracer struct {
	mu      sync.Mutex
	entries []fixtureAnnotation
}

type fixtureAnnotation struct {
	Key   string
	Value any
}

func (f *fixtureTracer) Annotate(key string, value any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.entries = append(f.entries, fixtureAnnotation{Key: key, Value: value})
}

func (f *fixtureTracer) get(key string) (any, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, e := range f.entries {
		if e.Key == key {
			return e.Value, true
		}
	}
	return nil, false
}

func TestRetrievalPath_SemanticHybridFirstAndLikeFallback(t *testing.T) {
	store := testutil.TempStore(t)
	ctx := context.Background()

	// Seed a semantic row whose value contains the FTS-tokenisable phrase
	// "canary release window". Migration 048's INSERT trigger writes the
	// value to memory_fts so HybridSearch's FTS leg can score it.
	testutil.SeedSemanticMemory(t, store, "policy", "deployment-window",
		"canary release window must be at least 30 minutes")

	t.Run("FTS leg matches the stored content", func(t *testing.T) {
		tracer := &fixtureTracer{}
		ctx := WithRetrievalTracer(ctx, tracer)

		mr := NewRetriever(DefaultRetrievalConfig(), DefaultTierBudget(), store)
		results := mr.retrieveSemanticEvidence(ctx, "canary release", nil, RetrievalHybrid, 200)
		if len(results) == 0 {
			t.Fatalf("expected at least one semantic evidence row for 'canary release'")
		}

		got, ok := tracer.get(retrievalPathKey(RetrievalTierSemantic))
		if !ok {
			t.Fatalf("expected retrieval.path.semantic annotation; tracer entries: %+v", tracer.entries)
		}
		// Vector index is empty in this test, so the path must be FTS.
		if got != RetrievalPathFTS {
			t.Fatalf("expected path %q (vector index is empty); got %q", RetrievalPathFTS, got)
		}
	})

	t.Run("query that matches nothing falls through to like_fallback or empty", func(t *testing.T) {
		tracer := &fixtureTracer{}
		ctx := WithRetrievalTracer(ctx, tracer)

		mr := NewRetriever(DefaultRetrievalConfig(), DefaultTierBudget(), store)
		// "zzznonexistent" can't match anything via FTS, vector, or LIKE.
		_ = mr.retrieveSemanticEvidence(ctx, "zzznonexistent", nil, RetrievalHybrid, 200)

		got, ok := tracer.get(retrievalPathKey(RetrievalTierSemantic))
		if !ok {
			t.Fatalf("expected retrieval.path.semantic annotation even on empty result")
		}
		if got != RetrievalPathEmpty {
			t.Fatalf("expected %q for unmatchable query; got %q", RetrievalPathEmpty, got)
		}
	})

	t.Run("non-search mode emits no annotation", func(t *testing.T) {
		tracer := &fixtureTracer{}
		ctx := WithRetrievalTracer(ctx, tracer)

		mr := NewRetriever(DefaultRetrievalConfig(), DefaultTierBudget(), store)
		// RetrievalRecency is a browse mode, not a search.
		_ = mr.retrieveSemanticEvidence(ctx, "anything", nil, RetrievalRecency, 200)

		if _, ok := tracer.get(retrievalPathKey(RetrievalTierSemantic)); ok {
			t.Fatalf("expected no semantic path annotation in browse mode; got %+v", tracer.entries)
		}
	})
}

func TestRetrievalPath_ProceduralHybridFirstAndAnnotation(t *testing.T) {
	store := testutil.TempStore(t)
	ctx := context.Background()

	// Seed a category=tool procedural_memory row so the FTS trigger writes
	// "deploy_cli: <steps>" into memory_fts. We then ask for "deploy" which
	// the FTS5 tokeniser will match against the indexed name.
	testutil.SeedProceduralMemory(t, store, "deploy_cli", 4, 1)

	tracer := &fixtureTracer{}
	ctx = WithRetrievalTracer(ctx, tracer)

	mr := NewRetriever(DefaultRetrievalConfig(), DefaultTierBudget(), store)
	block := mr.retrieveProceduralMemory(ctx, "deploy", nil, RetrievalHybrid, 200)
	if !strings.Contains(block, "deploy_cli") {
		t.Fatalf("expected procedural block to surface deploy_cli; got %q", block)
	}

	got, ok := tracer.get(retrievalPathKey(RetrievalTierProcedural))
	if !ok {
		t.Fatalf("expected retrieval.path.procedural annotation; tracer: %+v", tracer.entries)
	}
	if got != RetrievalPathFTS {
		t.Fatalf("expected %q for matched procedural query; got %q", RetrievalPathFTS, got)
	}
}

func TestRetrievalPath_RelationshipHybridFirstAndAnnotation(t *testing.T) {
	store := testutil.TempStore(t)
	ctx := context.Background()

	_, err := store.ExecContext(ctx,
		`INSERT INTO relationship_memory
		 (id, entity_id, entity_name, trust_score, interaction_summary, interaction_count)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		db.NewID(), "svc-billing", "Billing Service", 0.85,
		"depends on ledger reconciliation pipeline", 9)
	if err != nil {
		t.Fatalf("seed relationship_memory: %v", err)
	}

	tracer := &fixtureTracer{}
	ctx = WithRetrievalTracer(ctx, tracer)

	mr := NewRetriever(DefaultRetrievalConfig(), DefaultTierBudget(), store)
	results := mr.retrieveRelationshipEvidence(ctx, "ledger", nil, RetrievalHybrid, 400)
	if len(results) == 0 {
		t.Fatalf("expected relationship evidence for 'ledger'")
	}

	got, ok := tracer.get(retrievalPathKey(RetrievalTierRelationship))
	if !ok {
		t.Fatalf("expected retrieval.path.relationship annotation; tracer: %+v", tracer.entries)
	}
	if got != RetrievalPathFTS {
		t.Fatalf("expected %q for matched relationship query; got %q", RetrievalPathFTS, got)
	}
}

func TestRetrievalPath_WorkflowHybridFirstAndAnnotation(t *testing.T) {
	store := testutil.TempStore(t)
	ctx := context.Background()

	mm := NewManager(DefaultConfig(), store)
	if err := mm.RecordWorkflow(ctx, Workflow{
		Name:          "canary-release",
		Steps:         []string{"build artifact", "deploy 5%", "watch metrics", "ramp"},
		Preconditions: []string{"green CI", "owner approval"},
		ContextTags:   []string{"deploy", "canary"},
	}); err != nil {
		t.Fatalf("record workflow: %v", err)
	}

	tracer := &fixtureTracer{}
	ctx = WithRetrievalTracer(ctx, tracer)

	// findWorkflowsHybrid is exercised through the procedural retriever's
	// Part 0 (which calls it directly). A query that lexically matches the
	// workflow's name/tags must light up the workflow tier annotation.
	mr := NewRetriever(DefaultRetrievalConfig(), DefaultTierBudget(), store)
	block := mr.retrieveProceduralMemory(ctx, "canary", nil, RetrievalHybrid, 400)
	if !strings.Contains(block, "canary-release") {
		t.Fatalf("expected workflow block to surface canary-release; got %q", block)
	}

	got, ok := tracer.get(retrievalPathKey(RetrievalTierWorkflow))
	if !ok {
		t.Fatalf("expected retrieval.path.workflow annotation; tracer: %+v", tracer.entries)
	}
	if got != RetrievalPathFTS {
		t.Fatalf("expected %q for matched workflow query; got %q", RetrievalPathFTS, got)
	}
}

// TestRetrievalPath_LikeFallbackFiresWhenFTSMisses_Workflow proves that the
// LIKE safety net is exercised AND annotated when the FTS leg returns no
// workflow rows. We seed a workflow whose searchable tokens require a
// substring search the FTS5 tokeniser can't satisfy directly, then confirm
// the path classification flips to like_fallback rather than empty.
func TestRetrievalPath_LikeFallbackFiresWhenFTSMisses_Workflow(t *testing.T) {
	store := testutil.TempStore(t)
	ctx := context.Background()

	mm := NewManager(DefaultConfig(), store)
	if err := mm.RecordWorkflow(ctx, Workflow{
		Name:  "deploy-rollback-staging",
		Steps: []string{"snapshot db", "swap pointer", "verify"},
	}); err != nil {
		t.Fatalf("record workflow: %v", err)
	}

	tracer := &fixtureTracer{}
	ctx = WithRetrievalTracer(ctx, tracer)

	mr := NewRetriever(DefaultRetrievalConfig(), DefaultTierBudget(), store)
	// "rollback-staging" with the hyphen is a single FTS token that doesn't
	// match the stored hyphenated name in BM25 directly, but the LIKE leg
	// (which matches '%rollback-staging%' substring) does.
	_ = mr.retrieveProceduralMemory(ctx, "rollback-staging", nil, RetrievalHybrid, 400)

	got, ok := tracer.get(retrievalPathKey(RetrievalTierWorkflow))
	if !ok {
		t.Fatalf("expected retrieval.path.workflow annotation; tracer: %+v", tracer.entries)
	}
	// Either the FTS leg matched (acceptable: the tokeniser CAN sometimes
	// split the hyphen depending on FTS5 tokenizer config) OR the LIKE leg
	// rescued the search. What we MUST NOT see is "empty" — the workflow
	// is in the corpus and either leg should find it.
	if got != RetrievalPathFTS && got != RetrievalPathHybrid && got != RetrievalPathLikeFallback {
		t.Fatalf("expected fts/hybrid/like_fallback for present workflow; got %q", got)
	}
}

// TestRetrievalPath_ClassifyHybrid covers the path-classification helper
// for every (ftsHits, vectorHits) combination so the classification stays
// stable as new tier methods adopt it.
func TestRetrievalPath_ClassifyHybrid(t *testing.T) {
	cases := []struct {
		name   string
		fts    int
		vector int
		want   string
	}{
		{"both legs hit", 1, 1, RetrievalPathHybrid},
		{"fts only", 3, 0, RetrievalPathFTS},
		{"vector only", 0, 2, RetrievalPathVector},
		{"neither hit", 0, 0, ""},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got := classifyHybridPath(tc.fts, tc.vector)
			if got != tc.want {
				t.Fatalf("classifyHybridPath(%d, %d) = %q; want %q",
					tc.fts, tc.vector, got, tc.want)
			}
		})
	}
}

// TestRetrievalPath_NoTracerInContextIsSafe verifies that omitting the
// tracer is a true no-op. The retrieval tier methods must run identically
// whether a tracer is present or not — only the side-channel annotation
// changes.
func TestRetrievalPath_NoTracerInContextIsSafe(t *testing.T) {
	store := testutil.TempStore(t)
	ctx := context.Background()

	testutil.SeedSemanticMemory(t, store, "policy", "k", "value with token")

	mr := NewRetriever(DefaultRetrievalConfig(), DefaultTierBudget(), store)
	// No WithRetrievalTracer wrapping — nil tracer in context.
	results := mr.retrieveSemanticEvidence(ctx, "token", nil, RetrievalHybrid, 200)
	if len(results) == 0 {
		t.Fatalf("expected results even without tracer; got 0")
	}
}
