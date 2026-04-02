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
