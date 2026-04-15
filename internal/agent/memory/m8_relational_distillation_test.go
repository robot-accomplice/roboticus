// m8_relational_distillation_test.go is the regression suite for the M8
// relational distillation phase. Each subtest pins one acceptance criterion
// of the shipped M8 behavior:
//
//   - Relations round-trip through FormatForStorage ↔ parseEpisodeSummary
//   - Recurring high-quality episodes promote a (subject, relation, object)
//     triple into knowledge_facts via the canonical write gate
//   - Below-threshold support does NOT promote
//   - Failed / low-quality episodes don't drive promotion even at high count
//   - Promotion is idempotent across repeated consolidation runs (no
//     duplicate rows; later runs upsert in place)
//   - Non-canonical relations never reach knowledge_facts (defense-in-depth
//     against a corrupted episode_summary)
//   - Promoted relations are graph-traversable (KnowledgeGraph reads them
//     under the same vocabulary as per-document extractions)
//
// Together these guard the headline M8 contract: episode-level pattern
// observation flows into the typed graph layer without anecdote hijacking
// and without creating a parallel relation vocabulary.

package memory

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"roboticus/internal/db"
	"roboticus/testutil"
)

// TestM8_RelationsRoundTripThroughEpisodeSummary verifies that every
// canonical-relation triple emitted by AnalyzeEpisode survives the
// FormatForStorage → episodic_memory.content → parseEpisodeSummary path.
// If this regresses, distillation will see zero relations even though the
// extractor ran correctly at AnalyzeEpisode time.
func TestM8_RelationsRoundTripThroughEpisodeSummary(t *testing.T) {
	summary := AnalyzeEpisode(EpisodeInput{
		UserContent:     "what owns the billing service?",
		AssistantAnswer: "Billing Service depends on Ledger Service for invoice settlement.",
		EvidenceItems: []string{
			"Auth Service owned by Identity Team for user verification.",
		},
	})
	if summary == nil {
		t.Fatalf("expected episode summary; got nil")
	}
	if len(summary.Relations) == 0 {
		t.Fatalf("expected extracted relations; got 0")
	}

	formatted := summary.FormatForStorage()
	if !strings.Contains(formatted, "Relations: ") {
		t.Fatalf("expected Relations segment in formatted output; got %q", formatted)
	}

	parsed := parseEpisodeSummary(formatted)
	if len(parsed.Relations) != len(summary.Relations) {
		t.Fatalf("relation count mismatch: extracted=%d parsed=%d (formatted=%q)",
			len(summary.Relations), len(parsed.Relations), formatted)
	}
	for i, original := range summary.Relations {
		got := parsed.Relations[i]
		if got.Subject != original.Subject || got.Relation != original.Relation || got.Object != original.Object {
			t.Fatalf("round-trip mismatch at index %d: got %+v want %+v", i, got, original)
		}
	}
}

// TestM8_RecurringHighQualityRelationsArePromoted is the headline M8
// acceptance test. We seed three high-quality episode summaries that each
// contain the same canonical triple and verify the distillation phase
// promotes it into knowledge_facts.
func TestM8_RecurringHighQualityRelationsArePromoted(t *testing.T) {
	store := testutil.TempStore(t)
	ctx := context.Background()

	for i := 0; i < MinRelationDistillSupport; i++ {
		seedEpisodeSummaryWithRelations(t, store, "success", "high",
			[]EpisodeRelation{{Subject: "Billing", Relation: "depends_on", Object: "Ledger"}},
			fmt.Sprintf("episode-%d", i))
	}

	pipeline := newTestConsolidationPipeline()
	promoted := pipeline.phaseEpisodeDistillation(ctx, store)
	if promoted == 0 {
		t.Fatalf("expected at least one promotion (the recurring relation); got 0")
	}

	rows, err := store.QueryContext(ctx,
		`SELECT subject, relation, object, source_table, confidence
		   FROM knowledge_facts
		  WHERE subject = ? AND relation = ? AND object = ?`,
		"Billing", "depends_on", "Ledger")
	if err != nil {
		t.Fatalf("query knowledge_facts: %v", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		t.Fatalf("expected promoted triple Billing depends_on Ledger in knowledge_facts; found none")
	}
	var subject, relation, object, sourceTable string
	var confidence float64
	if err := rows.Scan(&subject, &relation, &object, &sourceTable, &confidence); err != nil {
		t.Fatalf("scan knowledge_facts: %v", err)
	}
	if sourceTable != distilledRelationSourceTable {
		t.Fatalf("expected source_table=%q for distilled row; got %q", distilledRelationSourceTable, sourceTable)
	}
	if confidence > distilledRelationConfidenceCap {
		t.Fatalf("distilled confidence must be ≤ %.2f; got %.2f", distilledRelationConfidenceCap, confidence)
	}
}

// TestM8_BelowThresholdRelationsAreNotPromoted confirms the anecdote-
// hijacking guard: a single episode (or two) mentioning a relation must
// not promote it. Only triples observed in MinRelationDistillSupport or
// more episodes survive the bar.
func TestM8_BelowThresholdRelationsAreNotPromoted(t *testing.T) {
	store := testutil.TempStore(t)
	ctx := context.Background()

	// Seed exactly MinRelationDistillSupport-1 episodes with the relation —
	// just one short of the bar. The distillation phase must NOT promote.
	for i := 0; i < MinRelationDistillSupport-1; i++ {
		seedEpisodeSummaryWithRelations(t, store, "success", "high",
			[]EpisodeRelation{{Subject: "PaymentApi", Relation: "uses", Object: "Stripe"}},
			fmt.Sprintf("below-threshold-%d", i))
	}

	pipeline := newTestConsolidationPipeline()
	_ = pipeline.phaseEpisodeDistillation(ctx, store)

	count := countKnowledgeFacts(t, store, "PaymentApi", "uses", "Stripe")
	if count != 0 {
		t.Fatalf("expected zero promoted rows below threshold; got %d", count)
	}
}

// TestM8_LowQualityEpisodesDoNotDrivePromotion proves that even if a
// relation appears in many episodes, those episodes must individually pass
// the high-quality bar (success outcome OR Quality high/medium) for the
// occurrence to count toward distillation. Low-quality / failed episodes
// are ignored entirely.
func TestM8_LowQualityEpisodesDoNotDrivePromotion(t *testing.T) {
	store := testutil.TempStore(t)
	ctx := context.Background()

	// 5 episodes mentioning the relation, but all are failures.
	for i := 0; i < 5; i++ {
		seedEpisodeSummaryWithRelations(t, store, "failure", "low",
			[]EpisodeRelation{{Subject: "OldAuth", Relation: "depends_on", Object: "FlakyDB"}},
			fmt.Sprintf("low-quality-%d", i))
	}

	pipeline := newTestConsolidationPipeline()
	_ = pipeline.phaseEpisodeDistillation(ctx, store)

	count := countKnowledgeFacts(t, store, "OldAuth", "depends_on", "FlakyDB")
	if count != 0 {
		t.Fatalf("expected failed episodes to be ignored; promoted %d rows", count)
	}
}

// TestM8_RelationPromotionIsIdempotent runs the distillation phase twice
// over the same corpus and asserts that the second run produces zero new
// rows for the promoted relation (the StoreKnowledgeFact UPSERT path
// updates in place via the stable distilled fact id).
func TestM8_RelationPromotionIsIdempotent(t *testing.T) {
	store := testutil.TempStore(t)
	ctx := context.Background()

	for i := 0; i < MinRelationDistillSupport; i++ {
		seedEpisodeSummaryWithRelations(t, store, "success", "high",
			[]EpisodeRelation{{Subject: "Worker", Relation: "uses", Object: "RedisQueue"}},
			fmt.Sprintf("idempotent-%d", i))
	}

	pipeline := newTestConsolidationPipeline()
	_ = pipeline.phaseEpisodeDistillation(ctx, store)
	firstCount := countKnowledgeFacts(t, store, "Worker", "uses", "RedisQueue")
	if firstCount != 1 {
		t.Fatalf("expected exactly 1 promoted row after first run; got %d", firstCount)
	}

	_ = pipeline.phaseEpisodeDistillation(ctx, store)
	secondCount := countKnowledgeFacts(t, store, "Worker", "uses", "RedisQueue")
	if secondCount != 1 {
		t.Fatalf("expected idempotent UPSERT — still 1 row after second run; got %d", secondCount)
	}
}

// TestM8_PromotedRelationsAreGraphTraversable closes the loop on the
// "graph retrieval can surface a distilled relation after promotion"
// acceptance criterion. After promotion, the KnowledgeGraph API must read
// the distilled triple as a normal traversable edge — distillation source
// must be invisible to the read path.
func TestM8_PromotedRelationsAreGraphTraversable(t *testing.T) {
	store := testutil.TempStore(t)
	ctx := context.Background()

	for i := 0; i < MinRelationDistillSupport; i++ {
		seedEpisodeSummaryWithRelations(t, store, "success", "high",
			[]EpisodeRelation{{Subject: "Frontend", Relation: "depends_on", Object: "ApiGateway"}},
			fmt.Sprintf("traverse-%d", i))
	}

	pipeline := newTestConsolidationPipeline()
	_ = pipeline.phaseEpisodeDistillation(ctx, store)

	// Load all knowledge facts and run a dependency walk from Frontend.
	rows, err := store.QueryContext(ctx,
		`SELECT id, subject, relation, object, confidence,
		        julianday('now') - julianday(updated_at)
		   FROM knowledge_facts`)
	if err != nil {
		t.Fatalf("query knowledge_facts: %v", err)
	}
	defer func() { _ = rows.Close() }()

	var facts []GraphFactRow
	for rows.Next() {
		var f GraphFactRow
		if err := rows.Scan(&f.ID, &f.Subject, &f.Relation, &f.Object, &f.Confidence, &f.AgeDays); err != nil {
			t.Fatalf("scan: %v", err)
		}
		facts = append(facts, f)
	}

	graph := NewKnowledgeGraph(facts)
	deps := graph.Dependencies("Frontend", 2)
	if len(deps) == 0 {
		t.Fatalf("expected at least one dependency edge from Frontend after distillation; got 0")
	}
	found := false
	for _, path := range deps {
		for _, edge := range path.Edges {
			if edge.To == "ApiGateway" && edge.Fact.Relation == "depends_on" {
				found = true
				break
			}
		}
	}
	if !found {
		t.Fatalf("expected to traverse Frontend -depends_on-> ApiGateway via distilled edge; deps=%+v", deps)
	}
}

// TestM8_ParseEpisodeSummary_DropsMalformedRelationSegments verifies the
// parser's defense-in-depth behavior: a corrupted episode_summary (missing
// fields, wrong separator count, empty parts) must not produce phantom
// relation entries that would skew the distillation tally.
func TestM8_ParseEpisodeSummary_DropsMalformedRelationSegments(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want int
	}{
		{"two valid", "A||depends_on||B; C||uses||D", 2},
		{"missing object", "A||depends_on||; C||uses||D", 1},
		{"wrong separator", "A|depends_on|B; C||uses||D", 1},
		{"empty subject", "||depends_on||B; C||uses||D", 1},
		{"all malformed", "A|B; C|D", 0},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got := parseRelationsList(tc.raw)
			if len(got) != tc.want {
				t.Fatalf("parseRelationsList(%q) → %d entries; want %d (%+v)",
					tc.raw, len(got), tc.want, got)
			}
		})
	}
}

// TestM8_NonCanonicalRelationsBlockedAtWriteGate uses a forged episode
// summary containing a non-canonical relation and confirms the write gate
// in StoreKnowledgeFact rejects it (and the canonical filter inside
// phaseEpisodeDistillation pre-empts the write entirely). The triple must
// not appear in knowledge_facts no matter how many episodes mention it.
func TestM8_NonCanonicalRelationsBlockedAtWriteGate(t *testing.T) {
	store := testutil.TempStore(t)
	ctx := context.Background()

	// "foo_bars" is intentionally not in db.CanonicalGraphRelations.
	for i := 0; i < MinRelationDistillSupport+2; i++ {
		seedRawEpisodeSummary(t, store,
			fmt.Sprintf("Goal: bogus | Outcome: success | Quality: high | Relations: A||foo_bars||B"))
	}

	pipeline := newTestConsolidationPipeline()
	_ = pipeline.phaseEpisodeDistillation(ctx, store)

	count := countKnowledgeFacts(t, store, "A", "foo_bars", "B")
	if count != 0 {
		t.Fatalf("non-canonical relation must be rejected; found %d rows", count)
	}
}

// ── helpers ───────────────────────────────────────────────────────────────

// newTestConsolidationPipeline returns a minimally-configured pipeline for
// invoking phaseEpisodeDistillation in tests. The full ConsolidationPipeline
// has many dependencies the distillation phase doesn't need.
func newTestConsolidationPipeline() *ConsolidationPipeline {
	return &ConsolidationPipeline{}
}

// seedEpisodeSummaryWithRelations writes an episodic_memory row whose
// content is the FormatForStorage shape with explicit Relations and the
// outcome/quality the test wants. Used by the M8 promotion tests.
func seedEpisodeSummaryWithRelations(t *testing.T, store *db.Store,
	outcome, quality string, relations []EpisodeRelation, suffix string) {
	t.Helper()
	var triples []string
	for _, rel := range relations {
		triples = append(triples, rel.Subject+"||"+rel.Relation+"||"+rel.Object)
	}
	content := fmt.Sprintf("Goal: test goal %s | Outcome: %s | Quality: %s | Relations: %s",
		suffix, outcome, quality, strings.Join(triples, "; "))
	seedRawEpisodeSummary(t, store, content)
}

// seedRawEpisodeSummary writes an episodic_memory row directly. The caller
// is responsible for the wire-format shape — used by the malformed /
// non-canonical guardrail tests where the test wants to bypass the
// FormatForStorage helper to construct adversarial input.
func seedRawEpisodeSummary(t *testing.T, store *db.Store, content string) {
	t.Helper()
	_, err := store.ExecContext(context.Background(),
		`INSERT INTO episodic_memory (id, classification, content, importance, memory_state)
		 VALUES (?, ?, ?, ?, ?)`,
		db.NewID(), "episode_summary", content, 0.6, "active")
	if err != nil {
		t.Fatalf("seed episode_summary: %v", err)
	}
}

func countKnowledgeFacts(t *testing.T, store *db.Store, subject, relation, object string) int {
	t.Helper()
	var n int
	if err := store.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM knowledge_facts
		  WHERE subject = ? AND relation = ? AND object = ?`,
		subject, relation, object).Scan(&n); err != nil {
		t.Fatalf("count knowledge_facts: %v", err)
	}
	return n
}
