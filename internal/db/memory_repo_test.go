package db

import (
	"context"
	"testing"
)

func TestMemoryRepository_WorkingMemory(t *testing.T) {
	store := testTempStore(t)
	repo := NewMemoryRepository(store)
	ctx := context.Background()

	if err := repo.StoreWorking(ctx, "w-1", "sess-1", "goal", "finish the task"); err != nil {
		t.Fatalf("StoreWorking: %v", err)
	}
	if err := repo.StoreWorking(ctx, "w-2", "sess-1", "note", "remember to check cache"); err != nil {
		t.Fatalf("StoreWorking second: %v", err)
	}

	rows, err := repo.QueryWorkingBySession(ctx, "sess-1", 10)
	if err != nil {
		t.Fatalf("QueryWorkingBySession: %v", err)
	}
	if len(rows) != 2 {
		t.Errorf("got %d rows, want 2", len(rows))
	}
	if rows[0].Tier != "working" {
		t.Errorf("Tier = %q, want working", rows[0].Tier)
	}

	// Delete one and verify count.
	if err := repo.DeleteWorking(ctx, "w-1"); err != nil {
		t.Fatalf("DeleteWorking: %v", err)
	}
	rows, err = repo.QueryWorkingBySession(ctx, "sess-1", 10)
	if err != nil {
		t.Fatalf("QueryWorkingBySession after delete: %v", err)
	}
	if len(rows) != 1 {
		t.Errorf("got %d rows after delete, want 1", len(rows))
	}
}

func TestMemoryRepository_SemanticMemory(t *testing.T) {
	store := testTempStore(t)
	repo := NewMemoryRepository(store)
	ctx := context.Background()

	if err := repo.StoreSemantic(ctx, "s-1", "user", "name", "Alice", 0.95); err != nil {
		t.Fatalf("StoreSemantic: %v", err)
	}
	if err := repo.StoreSemantic(ctx, "s-2", "user", "role", "admin", 0.90); err != nil {
		t.Fatalf("StoreSemantic second: %v", err)
	}

	rows, err := repo.QuerySemantic(ctx, "user", 10)
	if err != nil {
		t.Fatalf("QuerySemantic: %v", err)
	}
	if len(rows) != 2 {
		t.Errorf("got %d rows, want 2", len(rows))
	}

	// Upsert — confidence should update.
	if err := repo.StoreSemantic(ctx, "s-1b", "user", "name", "Bob", 0.99); err != nil {
		t.Fatalf("StoreSemantic upsert: %v", err)
	}
	got, err := repo.GetSemantic(ctx, "user", "name")
	if err != nil {
		t.Fatalf("GetSemantic: %v", err)
	}
	if got == nil {
		t.Fatal("GetSemantic returned nil")
	}
	if got.Value != "Bob" {
		t.Errorf("Value = %q, want Bob", got.Value)
	}
	if got.Confidence != 0.99 {
		t.Errorf("Confidence = %f, want 0.99", got.Confidence)
	}
}

func TestMemoryRepository_EpisodicMemory(t *testing.T) {
	store := testTempStore(t)
	repo := NewMemoryRepository(store)
	ctx := context.Background()

	if err := repo.StoreEpisodic(ctx, "e-1", "task", "completed login flow", 8); err != nil {
		t.Fatalf("StoreEpisodic: %v", err)
	}
	if err := repo.StoreEpisodic(ctx, "e-2", "error", "rate limit hit", 3); err != nil {
		t.Fatalf("StoreEpisodic second: %v", err)
	}

	rows, err := repo.QueryEpisodic(ctx, 10)
	if err != nil {
		t.Fatalf("QueryEpisodic: %v", err)
	}
	if len(rows) != 2 {
		t.Errorf("got %d rows, want 2", len(rows))
	}
	// First result should be highest importance.
	if rows[0].Importance != 8 {
		t.Errorf("first importance = %f, want 8", rows[0].Importance)
	}
}

func TestMemoryRepository_SemanticUpsertResetsState(t *testing.T) {
	store := testTempStore(t)
	repo := NewMemoryRepository(store)
	ctx := context.Background()

	// Create a semantic entry.
	if err := repo.StoreSemantic(ctx, "s-state-1", "prefs", "theme", "dark", 0.9); err != nil {
		t.Fatalf("StoreSemantic: %v", err)
	}

	// Manually mark it as superseded with a reason.
	_, err := store.ExecContext(ctx,
		`UPDATE semantic_memory SET memory_state = 'superseded', state_reason = 'outdated' WHERE category = 'prefs' AND key = 'theme'`)
	if err != nil {
		t.Fatalf("manual state update: %v", err)
	}

	// Verify state was set.
	var state, reason string
	err = store.QueryRowContext(ctx,
		`SELECT memory_state, COALESCE(state_reason, '') FROM semantic_memory WHERE category = 'prefs' AND key = 'theme'`).Scan(&state, &reason)
	if err != nil {
		t.Fatalf("verify pre-condition: %v", err)
	}
	if state != "superseded" {
		t.Fatalf("pre-condition: state = %q, want superseded", state)
	}

	// Upsert the same key — should reset state to active and clear reason.
	if err := repo.StoreSemantic(ctx, "s-state-2", "prefs", "theme", "light", 0.95); err != nil {
		t.Fatalf("StoreSemantic upsert: %v", err)
	}

	err = store.QueryRowContext(ctx,
		`SELECT memory_state, COALESCE(state_reason, '') FROM semantic_memory WHERE category = 'prefs' AND key = 'theme'`).Scan(&state, &reason)
	if err != nil {
		t.Fatalf("verify post-upsert: %v", err)
	}
	if state != "active" {
		t.Errorf("memory_state = %q after upsert, want 'active'", state)
	}
	if reason != "" {
		t.Errorf("state_reason = %q after upsert, want empty", reason)
	}
}

func TestMemoryRepository_GetSemantic_NotFound(t *testing.T) {
	store := testTempStore(t)
	repo := NewMemoryRepository(store)
	ctx := context.Background()

	got, err := repo.GetSemantic(ctx, "nonexistent", "key")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Error("expected nil for missing semantic key")
	}
}
