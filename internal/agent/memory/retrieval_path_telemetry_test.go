// retrieval_path_telemetry_test.go is the M3.3 regression. It pins the
// dormancy aggregator's behavior across the four cases that determine
// whether an operator can safely remove a tier's LIKE safety net:
//
//   1. A tier seen entirely on the FTS path → IsDormant true (above
//      sample minimum).
//   2. A tier seen mostly on FTS but with a few like_fallback hits → not
//      dormant when the fallback share exceeds the threshold.
//   3. A tier whose total observation count is below the sample minimum
//      → not dormant regardless of the fallback share (so a barely-
//      queried tier can't be deleted on weak evidence).
//   4. Multiple tiers in one trace are tallied independently — semantic
//      and procedural classifications don't interfere with each other.
//
// These cases together are the empirical guard the dev spec requires
// before LIKE retirement. If the aggregator regresses, M3.3's deletion
// step would be making decisions on garbage data, so the assertions
// here are deliberately strict about counts and fractions.

package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"roboticus/internal/db"
	"roboticus/testutil"
)

// localTraceSpan mirrors pipeline.TraceSpan's JSON shape for test fixtures.
// We can't import pipeline here because pipeline depends on memory and
// that direction is the cycle-safe one — flipping it would break the
// production build. The struct field tags must match pipeline.TraceSpan
// exactly; if those tags ever change, this fixture will silently produce
// the wrong shape and the assertions will fail loudly.
type localTraceSpan struct {
	Name       string         `json:"name"`
	DurationMs int64          `json:"duration_ms"`
	Outcome    string         `json:"outcome"`
	Metadata   map[string]any `json:"metadata,omitempty"`
}

func TestRetrievalPathTelemetry_DormantTierFlaggedAboveSample(t *testing.T) {
	store := testutil.TempStore(t)
	ctx := context.Background()

	// Seed enough FTS-only retrievals to clear the sample minimum.
	seedTracesWithRetrievalPath(t, store, RetrievalTierSemantic, RetrievalPathFTS, minSampleForDormancy+10)

	dist, err := AggregateRetrievalPaths(ctx, store, 0)
	if err != nil {
		t.Fatalf("AggregateRetrievalPaths: %v", err)
	}
	stats, ok := dist.Tiers[RetrievalTierSemantic]
	if !ok {
		t.Fatalf("expected semantic tier in distribution; got %+v", dist.Tiers)
	}
	if stats.TotalMeasured < minSampleForDormancy {
		t.Fatalf("expected ≥ %d total; got %d", minSampleForDormancy, stats.TotalMeasured)
	}
	if stats.LikeFallbackPct != 0 {
		t.Fatalf("expected 0 like_fallback share; got %.4f", stats.LikeFallbackPct)
	}
	if !stats.IsDormant {
		t.Fatalf("expected IsDormant=true (no fallback above sample minimum); got false")
	}
}

func TestRetrievalPathTelemetry_NotDormantWhenFallbackAboveThreshold(t *testing.T) {
	store := testutil.TempStore(t)
	ctx := context.Background()

	// 90% FTS, 10% like_fallback — well above the 1% retirement threshold.
	const total = minSampleForDormancy + 100
	const fallbackCount = total / 10
	seedTracesWithRetrievalPath(t, store, RetrievalTierProcedural, RetrievalPathFTS, total-fallbackCount)
	seedTracesWithRetrievalPath(t, store, RetrievalTierProcedural, RetrievalPathLikeFallback, fallbackCount)

	dist, err := AggregateRetrievalPaths(ctx, store, 0)
	if err != nil {
		t.Fatalf("AggregateRetrievalPaths: %v", err)
	}
	stats := dist.Tiers[RetrievalTierProcedural]
	if stats == nil {
		t.Fatalf("expected procedural tier; got nil (dist=%+v)", dist)
	}
	if stats.LikeFallbackPct < 0.05 {
		t.Fatalf("expected like_fallback share around 0.10; got %.4f", stats.LikeFallbackPct)
	}
	if stats.IsDormant {
		t.Fatalf("expected IsDormant=false (fallback share above threshold); got true (%.4f)", stats.LikeFallbackPct)
	}
}

func TestRetrievalPathTelemetry_NotDormantBelowSampleMinimum(t *testing.T) {
	store := testutil.TempStore(t)
	ctx := context.Background()

	// Only a handful of observations — below minSampleForDormancy. Even
	// though every observation was FTS, the gate must hold to avoid
	// retiring LIKE based on a tiny sample.
	seedTracesWithRetrievalPath(t, store, RetrievalTierRelationship, RetrievalPathFTS, 10)

	dist, err := AggregateRetrievalPaths(ctx, store, 0)
	if err != nil {
		t.Fatalf("AggregateRetrievalPaths: %v", err)
	}
	stats := dist.Tiers[RetrievalTierRelationship]
	if stats == nil {
		t.Fatalf("expected relationship tier in distribution")
	}
	if stats.LikeFallbackPct != 0 {
		t.Fatalf("like_fallback share must be 0 here; got %.4f", stats.LikeFallbackPct)
	}
	if stats.IsDormant {
		t.Fatalf("IsDormant must be false below sample minimum; total=%d (min=%d)",
			stats.TotalMeasured, minSampleForDormancy)
	}
}

func TestRetrievalPathTelemetry_MultipleTiersTalliedIndependently(t *testing.T) {
	store := testutil.TempStore(t)
	ctx := context.Background()

	// Each trace carries TWO retrieval-path annotations (semantic + workflow)
	// so we can confirm the aggregator counts them separately.
	for i := 0; i < 5; i++ {
		stagesJSON := buildStagesJSONWithMultipleTiers(map[string]string{
			"retrieval.path.semantic": RetrievalPathFTS,
			"retrieval.path.workflow": RetrievalPathLikeFallback,
		})
		insertTraceRow(t, store, stagesJSON)
	}

	dist, err := AggregateRetrievalPaths(ctx, store, 0)
	if err != nil {
		t.Fatalf("AggregateRetrievalPaths: %v", err)
	}
	if dist.TracesScanned != 5 {
		t.Fatalf("expected to scan 5 traces; scanned %d", dist.TracesScanned)
	}
	semantic := dist.Tiers[RetrievalTierSemantic]
	workflow := dist.Tiers[RetrievalTierWorkflow]
	if semantic == nil || workflow == nil {
		t.Fatalf("expected both semantic and workflow tiers; got %+v", dist.Tiers)
	}
	if semantic.CountsByPath[RetrievalPathFTS] != 5 {
		t.Fatalf("expected 5 semantic FTS observations; got %d", semantic.CountsByPath[RetrievalPathFTS])
	}
	if workflow.CountsByPath[RetrievalPathLikeFallback] != 5 {
		t.Fatalf("expected 5 workflow like_fallback observations; got %d", workflow.CountsByPath[RetrievalPathLikeFallback])
	}
}

func TestRetrievalPathTelemetry_SortedTiers_DeterministicOrder(t *testing.T) {
	dist := &RetrievalPathDistribution{
		Tiers: map[string]*RetrievalPathTierStats{
			RetrievalTierWorkflow:     {Tier: RetrievalTierWorkflow},
			RetrievalTierSemantic:     {Tier: RetrievalTierSemantic},
			RetrievalTierProcedural:   {Tier: RetrievalTierProcedural},
			RetrievalTierRelationship: {Tier: RetrievalTierRelationship},
		},
	}
	got := dist.SortedTiers()
	if len(got) != 4 {
		t.Fatalf("expected 4 tiers; got %d", len(got))
	}
	want := []string{RetrievalTierProcedural, RetrievalTierRelationship, RetrievalTierSemantic, RetrievalTierWorkflow}
	for i, ts := range got {
		if ts.Tier != want[i] {
			t.Fatalf("position %d: want %q got %q", i, want[i], ts.Tier)
		}
	}
}

// ── helpers ───────────────────────────────────────────────────────────────

// seedTracesWithRetrievalPath inserts `count` pipeline_traces rows whose
// stages_json contains a single span carrying the given path annotation.
// Used by every aggregator subtest to prime the corpus deterministically.
func seedTracesWithRetrievalPath(t *testing.T, store *db.Store, tier, path string, count int) {
	t.Helper()
	stagesJSON := buildStagesJSONWithMultipleTiers(map[string]string{
		"retrieval.path." + tier: path,
	})
	for i := 0; i < count; i++ {
		insertTraceRow(t, store, stagesJSON)
	}
}

func buildStagesJSONWithMultipleTiers(annotations map[string]string) string {
	span := localTraceSpan{
		Name:       "memory_retrieval",
		DurationMs: 5,
		Outcome:    "ok",
		Metadata:   make(map[string]any, len(annotations)),
	}
	for k, v := range annotations {
		span.Metadata[k] = v
	}
	buf, _ := json.Marshal([]localTraceSpan{span})
	return string(buf)
}

func insertTraceRow(t *testing.T, store *db.Store, stagesJSON string) {
	t.Helper()
	id := db.NewID()
	turnID := fmt.Sprintf("turn-%s", id)
	_, err := store.ExecContext(context.Background(),
		`INSERT INTO pipeline_traces (id, turn_id, session_id, channel, total_ms, stages_json)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		id, turnID, "session-x", "test", 100, stagesJSON)
	if err != nil {
		t.Fatalf("insert pipeline_traces: %v", err)
	}
}
