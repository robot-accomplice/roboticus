package db

import (
	"context"
	"testing"
)

func seedSwapTask(t *testing.T, store *Store, id, title, sourceJSON string) {
	t.Helper()
	ctx := context.Background()
	_, err := store.ExecContext(ctx,
		`INSERT INTO tasks (id, title, status, source) VALUES (?, ?, 'pending', ?)`,
		id, title, sourceJSON)
	if err != nil {
		t.Fatalf("seedSwapTask: %v", err)
	}
}

func TestRevenueSwapTaskLifecycle(t *testing.T) {
	store := testTempStore(t)
	repo := NewRevenueSwapRepository(store)
	ctx := context.Background()

	id := "rev_swap:opp_001"
	seedSwapTask(t, store, id, "Test Swap", `{"type":"revenue_swap","amount":100}`)

	// List should find it.
	tasks, err := repo.ListRevenueSwapTasks(ctx, 10)
	if err != nil {
		t.Fatalf("ListRevenueSwapTasks: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(tasks))
	}
	if tasks[0].Status != "pending" {
		t.Errorf("status = %q, want pending", tasks[0].Status)
	}

	// Get by ID.
	task, err := repo.GetRevenueSwapTask(ctx, id)
	if err != nil {
		t.Fatalf("GetRevenueSwapTask: %v", err)
	}
	if task == nil {
		t.Fatal("expected task, got nil")
	}
	if task.Title != "Test Swap" {
		t.Errorf("title = %q, want Test Swap", task.Title)
	}

	// pending → in_progress.
	ok, err := repo.MarkRevenueSwapInProgress(ctx, id)
	if err != nil {
		t.Fatalf("MarkRevenueSwapInProgress: %v", err)
	}
	if !ok {
		t.Error("expected transition to succeed")
	}

	// Double-mark should fail (already in_progress).
	ok, _ = repo.MarkRevenueSwapInProgress(ctx, id)
	if ok {
		t.Error("expected double in_progress to fail")
	}

	// in_progress → submitting (claim).
	ok, err = repo.ClaimRevenueSwapSubmission(ctx, id)
	if err != nil {
		t.Fatalf("ClaimRevenueSwapSubmission: %v", err)
	}
	if !ok {
		t.Error("expected claim to succeed")
	}

	// Double-claim should fail.
	ok, _ = repo.ClaimRevenueSwapSubmission(ctx, id)
	if ok {
		t.Error("expected double claim to fail")
	}

	// Release claim → back to in_progress.
	ok, err = repo.ReleaseRevenueSwapClaim(ctx, id)
	if err != nil {
		t.Fatalf("ReleaseRevenueSwapClaim: %v", err)
	}
	if !ok {
		t.Error("expected release to succeed")
	}

	// Re-claim and submit.
	ok, _ = repo.ClaimRevenueSwapSubmission(ctx, id)
	if !ok {
		t.Fatal("expected re-claim to succeed")
	}

	ok, err = repo.MarkRevenueSwapSubmitted(ctx, id, "tx_abc123")
	if err != nil {
		t.Fatalf("MarkRevenueSwapSubmitted: %v", err)
	}
	if !ok {
		t.Error("expected submit to succeed")
	}

	// Verify tx hash in source JSON.
	task, _ = repo.GetRevenueSwapTask(ctx, id)
	if task == nil || task.Status != "submitted" {
		t.Fatalf("expected submitted, got %v", task)
	}
	if task.SourceJSON == "" {
		t.Error("expected source JSON with tx hash")
	}

	// submitted → confirmed.
	ok, err = repo.MarkRevenueSwapConfirmed(ctx, id, "tx_abc123_confirmed")
	if err != nil {
		t.Fatalf("MarkRevenueSwapConfirmed: %v", err)
	}
	if !ok {
		t.Error("expected confirm to succeed")
	}
}

func TestRevenueSwapTaskFailed(t *testing.T) {
	store := testTempStore(t)
	repo := NewRevenueSwapRepository(store)
	ctx := context.Background()

	id := "rev_swap:fail_001"
	seedSwapTask(t, store, id, "Fail Swap", `{"type":"revenue_swap"}`)

	// Move to in_progress, then fail.
	repo.MarkRevenueSwapInProgress(ctx, id)
	ok, err := repo.MarkRevenueSwapFailed(ctx, id, "insufficient liquidity")
	if err != nil {
		t.Fatalf("MarkRevenueSwapFailed: %v", err)
	}
	if !ok {
		t.Error("expected fail to succeed")
	}

	task, _ := repo.GetRevenueSwapTask(ctx, id)
	if task.Status != "failed" {
		t.Errorf("status = %q, want failed", task.Status)
	}
}

func TestRevenueSwapGetNonexistent(t *testing.T) {
	store := testTempStore(t)
	repo := NewRevenueSwapRepository(store)

	task, err := repo.GetRevenueSwapTask(context.Background(), "nonexistent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if task != nil {
		t.Error("expected nil for nonexistent task")
	}
}
