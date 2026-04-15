package memory

import (
	"context"
	"strings"
	"testing"

	"roboticus/internal/db"
	"roboticus/testutil"
)

// TestBuildGraphPathEvidence_IgnoresNonCanonicalRelations is the M5 follow-on
// regression: if someone re-introduces the permissive fallback that used to
// let "mentions"-style relations count as dependency edges, this test
// catches it. The `mentions` edge should NOT produce a path.
func TestBuildGraphPathEvidence_IgnoresNonCanonicalRelations(t *testing.T) {
	facts := []graphFactRow{
		// Only non-canonical relation — no dependency edge connects A and B.
		{ID: "f1", Subject: "A", Relation: "mentions", Object: "B", Confidence: 0.9},
	}
	got := buildGraphPathEvidence(facts, "A", "B")
	if len(got) != 0 {
		t.Fatalf("expected no path via non-canonical relation, got %+v", got)
	}
}

func TestBuildGraphPathEvidence_TraversesCanonicalRelations(t *testing.T) {
	// Control: a canonical relation MUST produce a path so the previous
	// test proves the filter, not a blanket-off path search.
	facts := []graphFactRow{
		{ID: "f1", Subject: "A", Relation: "depends_on", Object: "B", Confidence: 0.9},
	}
	got := buildGraphPathEvidence(facts, "A", "B")
	if len(got) != 1 {
		t.Fatalf("expected one path via canonical edge, got %+v", got)
	}
	if !strings.Contains(got[0].Content, "A") || !strings.Contains(got[0].Content, "B") {
		t.Fatalf("expected path content to name both endpoints, got %q", got[0].Content)
	}
}

func TestCanonicalGraphRelations_InSyncWithExtractor(t *testing.T) {
	// Every relation the production extractor writes must be in the
	// canonical set — otherwise the write gate in StoreKnowledgeFact would
	// reject real ingestion output, and retrieval would silently ignore it.
	extractorRelations := make(map[string]struct{})
	for _, pat := range graphRelationPatterns {
		extractorRelations[pat.relation] = struct{}{}
	}
	for rel := range extractorRelations {
		if !db.IsCanonicalGraphRelation(rel) {
			t.Fatalf("extractor produces relation %q but it is not in db.CanonicalGraphRelations; add it there or remove from graphRelationPatterns", rel)
		}
	}
	for _, rel := range db.CanonicalGraphRelations {
		if _, ok := extractorRelations[rel]; !ok {
			t.Fatalf("canonical relation %q has no ingestion pattern in graphRelationPatterns; add one or remove from the canonical list", rel)
		}
	}
}

func TestIsTraversableRelation_DelegatesToCanonicalList(t *testing.T) {
	for _, rel := range db.CanonicalGraphRelations {
		if !IsTraversableRelation(rel) {
			t.Fatalf("canonical relation %q must be traversable", rel)
		}
	}
	for _, rel := range []string{"mentions", "", "see_also", "random_string"} {
		if IsTraversableRelation(rel) {
			t.Fatalf("non-canonical relation %q must not be traversable", rel)
		}
	}
}

func TestStoreKnowledgeFact_RejectsNonCanonicalRelation(t *testing.T) {
	store := testutil.TempStore(t)
	repo := db.NewMemoryRepository(store)

	err := repo.StoreKnowledgeFact(context.Background(),
		"f1", "A", "mentions", "B", "semantic_memory", "src-1", 0.9,
	)
	if err == nil {
		t.Fatal("expected StoreKnowledgeFact to reject non-canonical relation 'mentions'")
	}
	if !strings.Contains(err.Error(), "not canonical") {
		t.Fatalf("expected canonical-violation error, got %v", err)
	}

	// Control: a canonical relation must still succeed.
	err = repo.StoreKnowledgeFact(context.Background(),
		"f2", "A", "depends_on", "B", "semantic_memory", "src-1", 0.9,
	)
	if err != nil {
		t.Fatalf("expected canonical relation to be accepted, got %v", err)
	}
}
