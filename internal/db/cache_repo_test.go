package db

import (
	"context"
	"testing"
)

func TestCacheRepository_StoreAndLookup(t *testing.T) {
	store := testTempStore(t)
	repo := NewCacheRepository(store)
	ctx := context.Background()

	if err := repo.Store(ctx, "c-1", "hash-abc", "response text", "gpt-4"); err != nil {
		t.Fatalf("Store: %v", err)
	}

	row, err := repo.Lookup(ctx, "hash-abc")
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if row == nil {
		t.Fatal("expected a row, got nil")
	}
	if row.Response != "response text" {
		t.Errorf("Response = %q, want %q", row.Response, "response text")
	}
	if row.Model != "gpt-4" {
		t.Errorf("Model = %q, want %q", row.Model, "gpt-4")
	}
}

func TestCacheRepository_Lookup_NotFound(t *testing.T) {
	store := testTempStore(t)
	repo := NewCacheRepository(store)
	ctx := context.Background()

	row, err := repo.Lookup(ctx, "nonexistent-hash")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if row != nil {
		t.Error("expected nil for missing hash")
	}
}

func TestCacheRepository_IncrementHitsAndStats(t *testing.T) {
	store := testTempStore(t)
	repo := NewCacheRepository(store)
	ctx := context.Background()

	if err := repo.Store(ctx, "c-1", "hash-1", "resp-1", "claude-3"); err != nil {
		t.Fatalf("Store: %v", err)
	}
	if err := repo.Store(ctx, "c-2", "hash-2", "resp-2", "claude-3"); err != nil {
		t.Fatalf("Store second: %v", err)
	}

	_ = repo.IncrementHits(ctx, "hash-1")
	_ = repo.IncrementHits(ctx, "hash-1")
	_ = repo.IncrementHits(ctx, "hash-2")

	total, hits, err := repo.Stats(ctx)
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if total != 2 {
		t.Errorf("total = %d, want 2", total)
	}
	if hits != 3 {
		t.Errorf("totalHits = %d, want 3", hits)
	}
}

func TestCacheRepository_Evict(t *testing.T) {
	store := testTempStore(t)
	repo := NewCacheRepository(store)
	ctx := context.Background()

	if err := repo.Store(ctx, "c-1", "hash-old", "old response", "gpt-4"); err != nil {
		t.Fatalf("Store: %v", err)
	}

	// Evict everything older than the future — should evict our row.
	n, err := repo.Evict(ctx, "9999-12-31 23:59:59")
	if err != nil {
		t.Fatalf("Evict: %v", err)
	}
	if n != 1 {
		t.Errorf("evicted %d rows, want 1", n)
	}

	total, _, _ := repo.Stats(ctx)
	if total != 0 {
		t.Errorf("total after evict = %d, want 0", total)
	}
}
