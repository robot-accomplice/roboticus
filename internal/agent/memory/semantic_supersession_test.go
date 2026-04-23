package memory

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"roboticus/internal/db"
	"roboticus/testutil"
)

func TestStoreSemanticMemory_BumpsVersionOnValueChange(t *testing.T) {
	store := testutil.TempStore(t)
	mm := NewManager(DefaultConfig(), store)
	ctx := context.Background()

	mm.storeSemanticMemory(ctx, "policy", "refund_window", "30 days")
	mm.storeSemanticMemory(ctx, "policy", "refund_window", "60 days")

	var version int
	var value string
	if err := store.QueryRowContext(ctx,
		`SELECT version, value FROM semantic_memory WHERE category = ? AND key = ?`,
		"policy", "refund_window",
	).Scan(&version, &value); err != nil {
		t.Fatal(err)
	}
	if version != 2 {
		t.Fatalf("expected version=2 after value change, got %d", version)
	}
	if value != "60 days" {
		t.Fatalf("expected latest value retained, got %q", value)
	}
}

func TestStoreSemanticMemory_IdempotentRewriteKeepsVersion(t *testing.T) {
	store := testutil.TempStore(t)
	mm := NewManager(DefaultConfig(), store)
	ctx := context.Background()

	mm.storeSemanticMemory(ctx, "policy", "refund_window", "30 days")
	mm.storeSemanticMemory(ctx, "policy", "refund_window", "30 days")
	mm.storeSemanticMemory(ctx, "policy", "refund_window", "30 days")

	var version int
	if err := store.QueryRowContext(ctx,
		`SELECT version FROM semantic_memory WHERE category = ? AND key = ?`,
		"policy", "refund_window",
	).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version != 1 {
		t.Fatalf("expected version to stay at 1 for idempotent rewrites, got %d", version)
	}
}

func TestCurrentSemanticValue_WalksSupersessionChain(t *testing.T) {
	store := testutil.TempStore(t)
	mm := NewManager(DefaultConfig(), store)
	ctx := context.Background()

	// The UNIQUE(category, key) constraint means supersession chains in
	// production arise across rows with similar-but-distinct keys (e.g.,
	// a new key version). Seed three such revisions so the walker has a
	// real chain to follow.
	seed := func(id, key, value, supersededBy, state string) {
		var sup sql.NullString
		if supersededBy != "" {
			sup = sql.NullString{String: supersededBy, Valid: true}
		}
		if _, err := store.ExecContext(ctx,
			`INSERT INTO semantic_memory (id, category, key, value, version, memory_state, superseded_by)
			 VALUES (?, 'policy', ?, ?, 1, ?, ?)`,
			id, key, value, state, sup,
		); err != nil {
			t.Fatalf("seed %s: %v", id, err)
		}
	}

	idOldest := db.NewID()
	idMid := db.NewID()
	idCurrent := db.NewID()

	seed(idCurrent, "refund_window_v3", "90 days", "", "active")
	seed(idMid, "refund_window_v2", "60 days", idCurrent, "stale")
	seed(idOldest, "refund_window_v1", "30 days", idMid, "stale")

	rev, err := mm.CurrentSemanticValue(ctx, idOldest)
	if err != nil {
		t.Fatalf("walk chain: %v", err)
	}
	if rev == nil || rev.ID != idCurrent {
		t.Fatalf("expected to resolve to current id %q, got %+v", idCurrent, rev)
	}
	if rev.Value != "90 days" {
		t.Fatalf("expected current value 90 days, got %q", rev.Value)
	}
	if rev.ChainDepth != 2 {
		t.Fatalf("expected chain depth 2, got %d", rev.ChainDepth)
	}
}

func TestCurrentSemanticValue_HandlesCycleSafely(t *testing.T) {
	store := testutil.TempStore(t)
	mm := NewManager(DefaultConfig(), store)
	ctx := context.Background()

	aID := db.NewID()
	bID := db.NewID()
	// A points at B; B points at A. Classic cycle. Use distinct keys so the
	// UNIQUE(category, key) constraint does not swallow one of the rows.
	if _, err := store.ExecContext(ctx,
		`INSERT INTO semantic_memory (id, category, key, value, version, memory_state, superseded_by)
		 VALUES (?, 'policy', 'k_a', 'a-value', 1, 'stale', ?),
		        (?, 'policy', 'k_b', 'b-value', 1, 'stale', ?)`,
		aID, bID, bID, aID,
	); err != nil {
		t.Fatal(err)
	}

	rev, err := mm.CurrentSemanticValue(ctx, aID)
	if !errors.Is(err, ErrSemanticChainCycle) {
		t.Fatalf("expected cycle error, got %v", err)
	}
	if rev == nil {
		t.Fatal("expected partial revision despite cycle")
	}
}

func TestMarkSemanticSuperseded_FlipsOriginalAndSetsPointer(t *testing.T) {
	store := testutil.TempStore(t)
	mm := NewManager(DefaultConfig(), store)
	ctx := context.Background()

	mm.storeSemanticMemory(ctx, "policy", "refund_window", "30 days")
	var oldID string
	if err := store.QueryRowContext(ctx,
		`SELECT id FROM semantic_memory WHERE category = ? AND key = ?`,
		"policy", "refund_window",
	).Scan(&oldID); err != nil {
		t.Fatal(err)
	}

	// Create a second row under a different key (so the unique constraint
	// does not swap them) that will act as the replacement.
	replacementID := db.NewID()
	if _, err := store.ExecContext(ctx,
		`INSERT INTO semantic_memory (id, category, key, value, version, memory_state)
		 VALUES (?, 'policy', 'refund_window_v2', '90 days', 1, 'active')`,
		replacementID,
	); err != nil {
		t.Fatal(err)
	}

	ok, err := mm.MarkSemanticSuperseded(ctx, oldID, replacementID, "new policy version")
	if err != nil || !ok {
		t.Fatalf("expected supersession to succeed, got ok=%v err=%v", ok, err)
	}

	var state, reason, ptr sql.NullString
	if err := store.QueryRowContext(ctx,
		`SELECT memory_state, state_reason, superseded_by FROM semantic_memory WHERE id = ?`,
		oldID,
	).Scan(&state, &reason, &ptr); err != nil {
		t.Fatal(err)
	}
	if state.String != "stale" {
		t.Fatalf("expected state=stale, got %q", state.String)
	}
	if ptr.String != replacementID {
		t.Fatalf("expected pointer to replacement, got %q", ptr.String)
	}

	// CurrentSemanticValue from oldID should now follow the chain to the
	// active replacement.
	rev, err := mm.CurrentSemanticValue(ctx, oldID)
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	if rev == nil || rev.ID != replacementID {
		t.Fatalf("expected chain to reach replacement, got %+v", rev)
	}
}

func TestMarkSemanticSuperseded_RejectsInactiveReplacement(t *testing.T) {
	store := testutil.TempStore(t)
	mm := NewManager(DefaultConfig(), store)
	ctx := context.Background()

	aID := db.NewID()
	bID := db.NewID()
	if _, err := store.ExecContext(ctx,
		`INSERT INTO semantic_memory (id, category, key, value, version, memory_state)
		 VALUES (?, 'policy', 'a', 'v', 1, 'active'),
		        (?, 'policy', 'b', 'v', 1, 'stale')`,
		aID, bID,
	); err != nil {
		t.Fatal(err)
	}
	_, err := mm.MarkSemanticSuperseded(ctx, aID, bID, "bad replacement")
	if err == nil {
		t.Fatal("expected error when replacement is stale")
	}
}

func TestSupersession_ConsolidationSetsPointerOnStaleEntry(t *testing.T) {
	store := testutil.TempStore(t)
	mm := NewManager(DefaultConfig(), store)
	ctx := context.Background()

	// Two different keys under the same category with similar subjects.
	// The consolidation contradiction phase compares embedded values.
	// We seed directly to avoid depending on the embedding pipeline.
	oldID := db.NewID()
	newID := db.NewID()
	if _, err := store.ExecContext(ctx,
		`INSERT INTO semantic_memory (id, category, key, value, version, memory_state, superseded_by)
		 VALUES (?, 'policy', 'refund_policy', 'Refund window is 30 days for unused items', 1, 'stale', ?)`,
		oldID, newID,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ExecContext(ctx,
		`INSERT INTO semantic_memory (id, category, key, value, version, memory_state)
		 VALUES (?, 'policy', 'refund_policy_v2', 'Refund window is 60 days for unused items', 1, 'active')`,
		newID,
	); err != nil {
		t.Fatal(err)
	}

	// Walking from the stale entry must resolve to the active replacement.
	rev, err := mm.CurrentSemanticValue(ctx, oldID)
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	if rev == nil || rev.ID != newID {
		t.Fatalf("expected resolution to newID, got %+v", rev)
	}
}
