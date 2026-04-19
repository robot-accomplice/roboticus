package memory

import (
	"context"
	"testing"
	"time"

	"roboticus/internal/db"
	"roboticus/testutil"
)

// writeEpisodeSummary persists a reflection summary as episodic_memory for
// the distillation phase to consume.
func writeEpisodeSummary(t *testing.T, store *db.Store, summary *EpisodeSummary) {
	t.Helper()
	if _, err := store.ExecContext(context.Background(),
		`INSERT INTO episodic_memory (id, classification, content, content_json, importance)
		 VALUES (?, 'episode_summary', ?, ?, 8)`,
		db.NewID(), summary.FormatForStorage(), summary.JSON(),
	); err != nil {
		t.Fatalf("seed episode summary: %v", err)
	}
}

func TestParseEpisodeSummary_PullsEnrichedFields(t *testing.T) {
	summary := &EpisodeSummary{
		Goal:          "deploy",
		Outcome:       "success",
		Learnings:     []string{"guard-triggered revision required before final answer"},
		FixPatterns:   []string{"shell: fail→success on retry"},
		EvidenceRefs:  []string{"cache TTL 24h"},
		ResultQuality: 0.9,
		Duration:      time.Second,
	}
	fields := parseEpisodeSummary(summary.FormatForStorage())
	if fields.Outcome != "success" {
		t.Fatalf("expected outcome=success, got %q", fields.Outcome)
	}
	if len(fields.Learnings) != 1 || fields.Learnings[0] != "guard-triggered revision required before final answer" {
		t.Fatalf("expected learning parsed, got %+v", fields.Learnings)
	}
	if len(fields.FixPatterns) != 1 || fields.FixPatterns[0] != "shell: fail→success on retry" {
		t.Fatalf("expected fix pattern parsed, got %+v", fields.FixPatterns)
	}
	if len(fields.EvidenceRefs) != 1 || fields.EvidenceRefs[0] != "cache TTL 24h" {
		t.Fatalf("expected evidence ref parsed, got %+v", fields.EvidenceRefs)
	}
	if !fields.HighQuality {
		t.Fatal("expected success outcome to mark HighQuality")
	}
}

func TestParseEpisodeSummaryStructured_PrefersJSONPayload(t *testing.T) {
	summary := &EpisodeSummary{
		Goal:          "deploy",
		Outcome:       "success",
		Learnings:     []string{"guard-triggered revision required before final answer"},
		FixPatterns:   []string{"shell: fail→success on retry"},
		EvidenceRefs:  []string{"cache TTL 24h"},
		ResultQuality: 0.9,
	}

	fields := parseEpisodeSummaryStructured("Goal: stale | Outcome: failure", summary.JSON())
	if fields.Outcome != "success" {
		t.Fatalf("expected JSON outcome to win, got %q", fields.Outcome)
	}
	if len(fields.Learnings) != 1 || fields.Learnings[0] != "guard-triggered revision required before final answer" {
		t.Fatalf("expected JSON learnings, got %+v", fields.Learnings)
	}
	if !fields.HighQuality {
		t.Fatal("expected JSON quality to mark HighQuality")
	}
}

func TestPhaseEpisodeDistillation_PromotesRepeatedFixPattern(t *testing.T) {
	store := testutil.TempStore(t)
	ctx := context.Background()

	// Three successful episodes, each recording the same fix pattern.
	for i := 0; i < 3; i++ {
		writeEpisodeSummary(t, store, &EpisodeSummary{
			Goal:        "deploy",
			Outcome:     "success",
			FixPatterns: []string{"shell: fail→success on retry"},
		})
	}

	pipeline := &ConsolidationPipeline{}
	promoted := pipeline.phaseEpisodeDistillation(ctx, store)
	if promoted == 0 {
		t.Fatalf("expected at least one distilled fact, got 0")
	}

	var count int
	_ = store.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM semantic_memory WHERE category = 'fix_pattern'`,
	).Scan(&count)
	if count != 1 {
		t.Fatalf("expected one fix_pattern row in semantic_memory, got %d", count)
	}

	// Running the phase again must not create duplicates — the ON CONFLICT
	// clause on (category, key) keeps it idempotent.
	_ = pipeline.phaseEpisodeDistillation(ctx, store)
	_ = store.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM semantic_memory WHERE category = 'fix_pattern'`,
	).Scan(&count)
	if count != 1 {
		t.Fatalf("expected idempotent distillation, got %d rows", count)
	}
}

func TestPhaseEpisodeDistillation_PromotesRepeatedEvidence(t *testing.T) {
	store := testutil.TempStore(t)
	ctx := context.Background()

	// Three successful episodes, each with the same evidence preview.
	for i := 0; i < 3; i++ {
		writeEpisodeSummary(t, store, &EpisodeSummary{
			Goal:         "investigate outage",
			Outcome:      "success",
			EvidenceRefs: []string{"Billing cache TTL was set to 24h in release v2"},
		})
	}

	pipeline := &ConsolidationPipeline{}
	promoted := pipeline.phaseEpisodeDistillation(ctx, store)
	if promoted == 0 {
		t.Fatalf("expected at least one distilled fact, got 0")
	}

	var count int
	_ = store.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM semantic_memory WHERE category = 'learned_fact'`,
	).Scan(&count)
	if count != 1 {
		t.Fatalf("expected one learned_fact row, got %d", count)
	}
}

func TestPhaseEpisodeDistillation_PromotesRepeatedLearnings(t *testing.T) {
	store := testutil.TempStore(t)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		writeEpisodeSummary(t, store, &EpisodeSummary{
			Goal:      "ship release",
			Outcome:   "success",
			Learnings: []string{"guard-triggered revision required before final answer"},
		})
	}

	pipeline := &ConsolidationPipeline{}
	promoted := pipeline.phaseEpisodeDistillation(ctx, store)
	if promoted == 0 {
		t.Fatalf("expected at least one distilled fact, got 0")
	}

	var count int
	var value string
	_ = store.QueryRowContext(ctx,
		`SELECT COUNT(*), COALESCE(MAX(value), '') FROM semantic_memory WHERE category = 'episode_learning'`,
	).Scan(&count, &value)
	if count != 1 {
		t.Fatalf("expected one episode_learning row, got %d", count)
	}
	if value != "guard-triggered revision required before final answer" {
		t.Fatalf("unexpected episode_learning value %q", value)
	}
}

func TestPhaseEpisodeDistillation_IgnoresLowSupport(t *testing.T) {
	store := testutil.TempStore(t)
	ctx := context.Background()

	// Only two episodes — below MinDistillSupport for evidence.
	for i := 0; i < 2; i++ {
		writeEpisodeSummary(t, store, &EpisodeSummary{
			Goal:         "investigate outage",
			Outcome:      "success",
			EvidenceRefs: []string{"anecdote-only evidence"},
		})
	}

	pipeline := &ConsolidationPipeline{}
	pipeline.phaseEpisodeDistillation(ctx, store)

	var count int
	_ = store.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM semantic_memory WHERE category = 'learned_fact'`,
	).Scan(&count)
	if count != 0 {
		t.Fatalf("expected no learned_fact rows below support threshold, got %d", count)
	}
}

func TestPhaseEpisodeDistillation_IgnoresLowQualityEpisodes(t *testing.T) {
	store := testutil.TempStore(t)
	ctx := context.Background()

	// Three episodes that all hit "failure" outcome — distillation must skip.
	for i := 0; i < 3; i++ {
		writeEpisodeSummary(t, store, &EpisodeSummary{
			Goal:         "bad run",
			Outcome:      "failure",
			EvidenceRefs: []string{"bad evidence"},
		})
	}

	pipeline := &ConsolidationPipeline{}
	pipeline.phaseEpisodeDistillation(ctx, store)

	var count int
	_ = store.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM semantic_memory WHERE category IN ('fix_pattern', 'learned_fact')`,
	).Scan(&count)
	if count != 0 {
		t.Fatalf("expected failure-outcome episodes to be skipped, got %d rows", count)
	}
}
