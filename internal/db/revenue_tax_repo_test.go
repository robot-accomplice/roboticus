package db

import (
	"context"
	"testing"
)

func seedTaxTask(t *testing.T, store *Store, id, title, sourceJSON string) {
	t.Helper()
	ctx := context.Background()
	_, err := store.ExecContext(ctx,
		`INSERT INTO tasks (id, title, status, source) VALUES (?, ?, 'pending', ?)`,
		id, title, sourceJSON)
	if err != nil {
		t.Fatalf("seedTaxTask: %v", err)
	}
}

func TestRevenueTaxTaskLifecycle(t *testing.T) {
	store := testTempStore(t)
	repo := NewRevenueTaxRepository(store)
	ctx := context.Background()

	id := "rev_tax:opp_001"
	seedTaxTask(t, store, id, "Test Tax Payout", `{"type":"revenue_tax_payout","amount":25}`)

	// List.
	tasks, err := repo.ListRevenueTaxTasks(ctx, 10)
	if err != nil {
		t.Fatalf("ListRevenueTaxTasks: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(tasks))
	}

	// Full lifecycle: pending → in_progress → submitting → submitted → confirmed.
	ok, _ := repo.MarkRevenueTaxInProgress(ctx, id)
	if !ok {
		t.Fatal("expected in_progress transition")
	}

	ok, _ = repo.ClaimRevenueTaxSubmission(ctx, id)
	if !ok {
		t.Fatal("expected claim")
	}

	ok, err = repo.MarkRevenueTaxSubmitted(ctx, id, "tax_tx_001")
	if err != nil {
		t.Fatalf("MarkRevenueTaxSubmitted: %v", err)
	}
	if !ok {
		t.Fatal("expected submit")
	}

	ok, err = repo.MarkRevenueTaxConfirmed(ctx, id, "tax_tx_001_conf")
	if err != nil {
		t.Fatalf("MarkRevenueTaxConfirmed: %v", err)
	}
	if !ok {
		t.Fatal("expected confirm")
	}

	task, _ := repo.GetRevenueTaxTask(ctx, id)
	if task.Status != "confirmed" {
		t.Errorf("status = %q, want confirmed", task.Status)
	}
}

func TestRevenueTaxClaimRelease(t *testing.T) {
	store := testTempStore(t)
	repo := NewRevenueTaxRepository(store)
	ctx := context.Background()

	id := "rev_tax:claim_001"
	seedTaxTask(t, store, id, "Claim Test", `{"type":"revenue_tax_payout"}`)

	repo.MarkRevenueTaxInProgress(ctx, id)

	// Claim.
	ok, _ := repo.ClaimRevenueTaxSubmission(ctx, id)
	if !ok {
		t.Fatal("expected claim")
	}

	// Double claim fails.
	ok, _ = repo.ClaimRevenueTaxSubmission(ctx, id)
	if ok {
		t.Error("expected double claim to fail")
	}

	// Release.
	ok, _ = repo.ReleaseRevenueTaxClaim(ctx, id)
	if !ok {
		t.Fatal("expected release")
	}

	// Can reclaim.
	ok, _ = repo.ClaimRevenueTaxSubmission(ctx, id)
	if !ok {
		t.Fatal("expected re-claim")
	}
}
