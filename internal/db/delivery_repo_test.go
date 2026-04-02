package db

import (
	"context"
	"testing"
)

func TestDeliveryRepository_EnqueueAndList(t *testing.T) {
	store := testTempStore(t)
	repo := NewDeliveryRepository(store)
	ctx := context.Background()

	row := DeliveryRow{
		ID:          "dq-1",
		Channel:     "telegram",
		RecipientID: "user-123",
		Content:     "Hello!",
		MaxAttempts: 3,
	}
	if err := repo.Enqueue(ctx, row); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	pending, err := repo.ListPending(ctx, 10)
	if err != nil {
		t.Fatalf("ListPending: %v", err)
	}
	if len(pending) != 1 {
		t.Errorf("got %d pending, want 1", len(pending))
	}
	if pending[0].Channel != "telegram" {
		t.Errorf("Channel = %q, want telegram", pending[0].Channel)
	}
}

func TestDeliveryRepository_MarkDelivered(t *testing.T) {
	store := testTempStore(t)
	repo := NewDeliveryRepository(store)
	ctx := context.Background()

	_ = repo.Enqueue(ctx, DeliveryRow{ID: "dq-1", Channel: "slack", RecipientID: "u-1", Content: "hi", MaxAttempts: 3})

	if err := repo.MarkInFlight(ctx, "dq-1"); err != nil {
		t.Fatalf("MarkInFlight: %v", err)
	}
	if err := repo.MarkDelivered(ctx, "dq-1"); err != nil {
		t.Fatalf("MarkDelivered: %v", err)
	}

	got, err := repo.GetByID(ctx, "dq-1")
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.Status != "delivered" {
		t.Errorf("Status = %q, want delivered", got.Status)
	}
	if got.Attempts != 1 {
		t.Errorf("Attempts = %d, want 1", got.Attempts)
	}
}

func TestDeliveryRepository_MarkFailed_DeadLetter(t *testing.T) {
	store := testTempStore(t)
	repo := NewDeliveryRepository(store)
	ctx := context.Background()

	_ = repo.Enqueue(ctx, DeliveryRow{ID: "dq-1", Channel: "email", RecipientID: "u-1", Content: "test", MaxAttempts: 1})
	_ = repo.MarkInFlight(ctx, "dq-1") // attempts becomes 1

	// Fail — attempts(1) >= max_attempts(1), should become dead_letter.
	if err := repo.MarkFailed(ctx, "dq-1", "connection refused", "2099-01-01 00:00:00"); err != nil {
		t.Fatalf("MarkFailed: %v", err)
	}

	got, _ := repo.GetByID(ctx, "dq-1")
	if got.Status != "dead_letter" {
		t.Errorf("Status = %q, want dead_letter", got.Status)
	}
	if got.LastError != "connection refused" {
		t.Errorf("LastError = %q, want 'connection refused'", got.LastError)
	}
}

func TestDeliveryRepository_CountByStatus(t *testing.T) {
	store := testTempStore(t)
	repo := NewDeliveryRepository(store)
	ctx := context.Background()

	_ = repo.Enqueue(ctx, DeliveryRow{ID: "dq-1", Channel: "slack", RecipientID: "u-1", Content: "a", MaxAttempts: 3})
	_ = repo.Enqueue(ctx, DeliveryRow{ID: "dq-2", Channel: "slack", RecipientID: "u-2", Content: "b", MaxAttempts: 3})
	_ = repo.MarkDelivered(ctx, "dq-1")

	counts, err := repo.CountByStatus(ctx)
	if err != nil {
		t.Fatalf("CountByStatus: %v", err)
	}
	if counts["pending"] != 1 {
		t.Errorf("pending count = %d, want 1", counts["pending"])
	}
	if counts["delivered"] != 1 {
		t.Errorf("delivered count = %d, want 1", counts["delivered"])
	}
}
