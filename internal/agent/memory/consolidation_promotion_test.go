package memory

import (
	"context"
	"testing"

	"roboticus/internal/db"
	"roboticus/internal/llm"
	"roboticus/testutil"
)

// mockDistiller is a test double for LLM-assisted distillation.
type mockDistiller struct {
	calls   int
	outputs []string
}

func (m *mockDistiller) Distill(_ context.Context, entries []string) (string, error) {
	m.calls++
	if m.calls <= len(m.outputs) {
		return m.outputs[m.calls-1], nil
	}
	return "distilled fact from " + string(rune('0'+len(entries))) + " entries", nil
}

func TestEpisodicPromotion_LLMDistillation(t *testing.T) {
	store := testutil.TempStore(t)
	ctx := context.Background()

	// Seed 3 similar-but-not-identical episodic entries.
	// Must have Jaccard > 0.5 (promotion threshold) but < 0.85 (dedup threshold)
	// so they survive dedup and trigger promotion.
	variants := []string{
		"the server deployment failed because of permission error on nginx config file in production",
		"the server deployment failed because of ownership error on apache config file in staging",
		"the server deployment failed because of access error on haproxy config file in development",
	}
	for _, v := range variants {
		_, _ = store.ExecContext(ctx,
			`INSERT INTO episodic_memory (id, classification, content, importance)
			 VALUES (?, 'tool_event', ?, 5)`,
			db.NewID(), v)
	}

	distilledFact := "Server deployments fail when config file permissions are wrong."
	distiller := &mockDistiller{outputs: []string{distilledFact}}

	pipe := NewConsolidationPipeline()
	pipe.MinInterval = 0
	pipe.Distiller = distiller
	report := pipe.Run(ctx, store)

	if report.Promoted < 1 {
		t.Errorf("expected at least 1 promotion, got %d", report.Promoted)
	}
	if distiller.calls < 1 {
		t.Error("expected distiller to be called at least once")
	}

	// Verify the semantic value is the distilled output, not raw episodic text.
	var value string
	err := store.QueryRowContext(ctx,
		`SELECT value FROM semantic_memory WHERE memory_state = 'active' AND state_reason = 'promoted from episodic'`).Scan(&value)
	if err != nil {
		t.Fatalf("query semantic: %v", err)
	}
	if value != distilledFact {
		t.Errorf("expected distilled value %q, got: %q", distilledFact, value)
	}
}

func TestEpisodicPromotion_FallbackWithoutLLM(t *testing.T) {
	store := testutil.TempStore(t)
	ctx := context.Background()

	// Similar-but-not-identical content (avoids dedup, triggers promotion).
	variants := []string{
		"the database migration failed because of schema error on users table in production",
		"the database migration failed because of constraint error on orders table in staging",
		"the database migration failed because of type error on products table in development",
	}
	for _, v := range variants {
		_, _ = store.ExecContext(ctx,
			`INSERT INTO episodic_memory (id, classification, content, importance)
			 VALUES (?, 'tool_event', ?, 5)`,
			db.NewID(), v)
	}

	// No distiller set — should use longest-entry heuristic.
	pipe := NewConsolidationPipeline()
	pipe.MinInterval = 0
	report := pipe.Run(ctx, store)

	if report.Promoted < 1 {
		t.Errorf("expected promotion even without LLM, got %d", report.Promoted)
	}

	// Value should be one of the raw contents (the longest entry).
	var value string
	err := store.QueryRowContext(ctx,
		`SELECT value FROM semantic_memory WHERE memory_state = 'active'`).Scan(&value)
	if err != nil {
		t.Fatalf("query semantic: %v", err)
	}
	// Without LLM, value should be the longest variant.
	if len(value) == 0 {
		t.Error("expected non-empty semantic value from fallback promotion")
	}
}

func TestEpisodicPromotion_CostCap(t *testing.T) {
	store := testutil.TempStore(t)
	ctx := context.Background()

	// Create 6 groups of 3 similar entries (6 promotions needed).
	for g := 0; g < 6; g++ {
		baseContent := "group " + string(rune('A'+g)) + " the repeated event content that is long enough to be similar"
		for i := 0; i < 3; i++ {
			_, _ = store.ExecContext(ctx,
				`INSERT INTO episodic_memory (id, classification, content, importance)
				 VALUES (?, 'tool_event', ?, 5)`,
				db.NewID(), baseContent)
		}
	}

	distiller := &mockDistiller{}
	pipe := NewConsolidationPipeline()
	pipe.MinInterval = 0
	pipe.Distiller = distiller
	pipe.MaxDistillPerRun = 3
	pipe.Run(ctx, store)

	if distiller.calls > 3 {
		t.Errorf("expected max 3 distill calls (cost cap), got %d", distiller.calls)
	}
}

func TestContradictionDetection_SupersedesOlder(t *testing.T) {
	store := testutil.TempStore(t)
	ctx := context.Background()
	ec := llm.NewEmbeddingClient(nil) // n-gram fallback

	// Insert "old" entry with old created_at AND updated_at.
	oldID := db.NewID()
	_, _ = store.ExecContext(ctx,
		`INSERT INTO semantic_memory (id, category, key, value, created_at, updated_at)
		 VALUES (?, 'knowledge', 'deployment tool', 'deployment uses Docker containers', datetime('now', '-1 hour'), datetime('now', '-1 hour'))`,
		oldID)

	// Insert consolidation log marking the boundary.
	_, _ = store.ExecContext(ctx,
		`INSERT INTO consolidation_log (id, indexed, deduped, promoted, confidence_decayed, importance_decayed, pruned, orphaned, created_at)
		 VALUES (?, 0, 0, 0, 0, 0, 0, 0, datetime('now', '-30 minutes'))`,
		db.NewID())

	// Insert contradicting "new" entry after the boundary.
	newID := db.NewID()
	_, _ = store.ExecContext(ctx,
		`INSERT INTO semantic_memory (id, category, key, value, created_at)
		 VALUES (?, 'knowledge', 'deployment system', 'deployment uses Podman containers', datetime('now'))`,
		newID)

	pipe := NewConsolidationPipeline()
	pipe.MinInterval = 0
	pipe.EmbedClient = ec
	report := pipe.Run(ctx, store)

	if report.Superseded < 1 {
		t.Errorf("expected at least 1 superseded entry, got %d", report.Superseded)
	}

	// Verify old entry is stale.
	var state string
	err := store.QueryRowContext(ctx,
		`SELECT memory_state FROM semantic_memory WHERE id = ?`, oldID).Scan(&state)
	if err != nil {
		t.Fatalf("query old entry: %v", err)
	}
	if state != "stale" {
		t.Errorf("old contradicting entry should be stale, got: %s", state)
	}

	// Verify new entry is still active.
	err = store.QueryRowContext(ctx,
		`SELECT memory_state FROM semantic_memory WHERE id = ?`, newID).Scan(&state)
	if err != nil {
		t.Fatalf("query new entry: %v", err)
	}
	if state != "active" {
		t.Errorf("new entry should still be active, got: %s", state)
	}
}

func TestContradictionDetection_BothPreExisting(t *testing.T) {
	// Two contradicting entries that BOTH predate the last consolidation.
	// The broader scope should still detect and supersede the older one.
	store := testutil.TempStore(t)
	ctx := context.Background()
	ec := llm.NewEmbeddingClient(nil)

	// Both entries created "before" consolidation cutoff.
	_, _ = store.ExecContext(ctx,
		`INSERT INTO semantic_memory (id, category, key, value, created_at)
		 VALUES (?, 'knowledge', 'db_old', 'the system uses PostgreSQL database for storage', datetime('now', '-2 hours'))`,
		db.NewID())
	_, _ = store.ExecContext(ctx,
		`INSERT INTO semantic_memory (id, category, key, value, created_at)
		 VALUES (?, 'knowledge', 'db_new', 'the system uses SQLite database for storage', datetime('now', '-1 hour'))`,
		db.NewID())

	// Consolidation log entry that predates both entries (old approach would miss both).
	_, _ = store.ExecContext(ctx,
		`INSERT INTO consolidation_log (id, indexed, deduped, promoted, confidence_decayed, importance_decayed, pruned, orphaned, created_at)
		 VALUES (?, 0, 0, 0, 0, 0, 0, 0, datetime('now', '-3 hours'))`,
		db.NewID())

	pipe := NewConsolidationPipeline()
	pipe.MinInterval = 0
	pipe.EmbedClient = ec
	report := pipe.Run(ctx, store)

	if report.Superseded < 1 {
		t.Errorf("expected at least 1 superseded even for pre-existing entries, got %d", report.Superseded)
	}
}

func TestContradictionDetection_DifferentSubjectNotSuperseded(t *testing.T) {
	store := testutil.TempStore(t)
	ctx := context.Background()
	ec := llm.NewEmbeddingClient(nil)

	// Two entries about different subjects — should NOT supersede.
	_, _ = store.ExecContext(ctx,
		`INSERT INTO semantic_memory (id, category, key, value)
		 VALUES (?, 'knowledge', 'deploy_docker', 'the deployment system uses Docker containers for isolation')`,
		db.NewID())
	_, _ = store.ExecContext(ctx,
		`INSERT INTO semantic_memory (id, category, key, value)
		 VALUES (?, 'knowledge', 'test_docker', 'the testing framework uses Docker containers for isolation')`,
		db.NewID())

	pipe := NewConsolidationPipeline()
	pipe.MinInterval = 0
	pipe.EmbedClient = ec
	report := pipe.Run(ctx, store)

	// Both should remain active (different subjects: deployment vs testing).
	var activeCount int
	_ = store.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM semantic_memory WHERE memory_state = 'active'`).Scan(&activeCount)
	if activeCount < 2 {
		t.Errorf("different-subject entries should both remain active, got %d active", activeCount)
	}
	if report.Superseded > 0 {
		t.Logf("⚠ %d entries superseded — n-gram similarity may not distinguish subjects well", report.Superseded)
	}
}

func TestContradictionDetection_IgnoresIdentical(t *testing.T) {
	store := testutil.TempStore(t)
	ctx := context.Background()
	ec := llm.NewEmbeddingClient(nil)

	// Two identical entries should not trigger contradiction.
	for i := 0; i < 2; i++ {
		_, _ = store.ExecContext(ctx,
			`INSERT INTO semantic_memory (id, category, key, value)
			 VALUES (?, 'knowledge', ?, 'deployment uses Docker containers')`,
			db.NewID(), "key"+string(rune('A'+i)))
	}

	pipe := NewConsolidationPipeline()
	pipe.MinInterval = 0
	pipe.EmbedClient = ec
	report := pipe.Run(ctx, store)

	if report.Superseded != 0 {
		t.Errorf("identical entries should not be superseded, got %d", report.Superseded)
	}
}

func TestFTSUpdateTrigger_SemanticValueChange(t *testing.T) {
	store := testutil.TempStore(t)
	ctx := context.Background()

	entryID := db.NewID()
	_, _ = store.ExecContext(ctx,
		`INSERT INTO semantic_memory (id, category, key, value)
		 VALUES (?, 'knowledge', 'tool', 'uses Docker')`, entryID)

	// Check FTS finds the original value.
	var count int
	_ = store.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM memory_fts WHERE memory_fts MATCH '"Docker"'`).Scan(&count)
	// Note: FTS entry may not exist if no INSERT trigger covers semantic_memory directly.
	// The UPDATE trigger we added fires only on UPDATE, not INSERT.
	// This test verifies the UPDATE path works.

	// Update the value.
	_, _ = store.ExecContext(ctx,
		`UPDATE semantic_memory SET value = 'uses Podman' WHERE id = ?`, entryID)

	// After update, FTS should find Podman (via the new UPDATE trigger).
	var countPodman int
	err := store.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM memory_fts WHERE memory_fts MATCH '"Podman"'`).Scan(&countPodman)
	if err != nil {
		t.Fatalf("FTS query error: %v", err)
	}
	if countPodman < 1 {
		t.Error("after UPDATE, FTS should find the new value 'Podman'")
	}
}
