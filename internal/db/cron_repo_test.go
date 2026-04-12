package db

import (
	"context"
	"testing"
)

func TestCronRepository_TryAcquireLease(t *testing.T) {
	store := testTempStore(t)
	repo := NewCronRepository(store)
	ctx := context.Background()

	// Seed a cron job.
	err := repo.CreateJob(ctx, "job-1", "test-job", "desc", "cron", "*/5 * * * *", nil, "agent-1", `{}`, "none", "")
	if err != nil {
		t.Fatalf("CreateJob: %v", err)
	}

	// First acquire should succeed.
	ok, err := repo.TryAcquireLease(ctx, "job-1", "holder-A")
	if err != nil {
		t.Fatalf("TryAcquireLease: %v", err)
	}
	if !ok {
		t.Error("first TryAcquireLease should succeed")
	}

	// Second acquire by a different holder should fail (lease not expired).
	ok, err = repo.TryAcquireLease(ctx, "job-1", "holder-B")
	if err != nil {
		t.Fatalf("TryAcquireLease second: %v", err)
	}
	if ok {
		t.Error("second TryAcquireLease should fail while lease is held")
	}

	// Same holder re-acquiring should also fail (lease_holder is not NULL and not expired).
	ok, err = repo.TryAcquireLease(ctx, "job-1", "holder-A")
	if err != nil {
		t.Fatalf("TryAcquireLease same holder: %v", err)
	}
	if ok {
		t.Error("same holder re-acquire should fail while lease is active")
	}
}

func TestCronRepository_ReleaseLease(t *testing.T) {
	store := testTempStore(t)
	repo := NewCronRepository(store)
	ctx := context.Background()

	err := repo.CreateJob(ctx, "job-2", "test-job-2", "desc", "cron", "*/5 * * * *", nil, "agent-1", `{}`, "none", "")
	if err != nil {
		t.Fatalf("CreateJob: %v", err)
	}

	// Acquire lease.
	ok, err := repo.TryAcquireLease(ctx, "job-2", "holder-A")
	if err != nil || !ok {
		t.Fatalf("TryAcquireLease: ok=%v, err=%v", ok, err)
	}

	// Release by wrong holder should be a no-op.
	if err := repo.ReleaseLease(ctx, "job-2", "holder-B"); err != nil {
		t.Fatalf("ReleaseLease wrong holder: %v", err)
	}
	// Lease should still be held — another holder can't acquire.
	ok, err = repo.TryAcquireLease(ctx, "job-2", "holder-B")
	if err != nil {
		t.Fatalf("TryAcquireLease after wrong release: %v", err)
	}
	if ok {
		t.Error("lease should still be held after wrong-holder release")
	}

	// Release by correct holder.
	if err := repo.ReleaseLease(ctx, "job-2", "holder-A"); err != nil {
		t.Fatalf("ReleaseLease correct holder: %v", err)
	}

	// Now another holder can acquire.
	ok, err = repo.TryAcquireLease(ctx, "job-2", "holder-B")
	if err != nil {
		t.Fatalf("TryAcquireLease after release: %v", err)
	}
	if !ok {
		t.Error("TryAcquireLease should succeed after release")
	}
}

func TestCronRepository_TryAcquireLease_ExpiredLease(t *testing.T) {
	store := testTempStore(t)
	repo := NewCronRepository(store)
	ctx := context.Background()

	err := repo.CreateJob(ctx, "job-3", "test-job-3", "desc", "cron", "*/5 * * * *", nil, "agent-1", `{}`, "none", "")
	if err != nil {
		t.Fatalf("CreateJob: %v", err)
	}

	// Acquire lease then manually expire it.
	ok, err := repo.TryAcquireLease(ctx, "job-3", "holder-A")
	if err != nil || !ok {
		t.Fatalf("TryAcquireLease: ok=%v, err=%v", ok, err)
	}
	// Set lease_expires_at to the past.
	_, err = store.ExecContext(ctx,
		`UPDATE cron_jobs SET lease_expires_at = datetime('now', '-10 seconds') WHERE id = ?`, "job-3")
	if err != nil {
		t.Fatalf("manual expire: %v", err)
	}

	// Now another holder should be able to acquire the expired lease.
	ok, err = repo.TryAcquireLease(ctx, "job-3", "holder-B")
	if err != nil {
		t.Fatalf("TryAcquireLease after expiry: %v", err)
	}
	if !ok {
		t.Error("TryAcquireLease should succeed on expired lease")
	}
}
