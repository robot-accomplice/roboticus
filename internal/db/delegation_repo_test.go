package db

import (
	"context"
	"testing"
)

func TestDelegationRepository_SaveAndList(t *testing.T) {
	store := testTempStore(t)
	repo := NewDelegationRepository(store)
	ctx := context.Background()

	if err := repo.Save(ctx, DelegationRow{
		ID: "d1", ParentTaskID: "task1", SubagentID: "agent1",
		Status: "pending", DurationMs: 0,
	}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	list, err := repo.List(ctx, "task1")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("got %d, want 1", len(list))
	}
	if list[0].SubagentID != "agent1" {
		t.Errorf("SubagentID = %q, want %q", list[0].SubagentID, "agent1")
	}
}

func TestDelegationRepository_UpdateOutcome(t *testing.T) {
	store := testTempStore(t)
	repo := NewDelegationRepository(store)
	ctx := context.Background()

	_ = repo.Save(ctx, DelegationRow{ID: "d1", ParentTaskID: "task1", SubagentID: "a1", Status: "pending"})

	if err := repo.UpdateOutcome(ctx, "d1", "complete", "success", "", 1500); err != nil {
		t.Fatalf("UpdateOutcome: %v", err)
	}

	list, _ := repo.List(ctx, "task1")
	if list[0].Status != "complete" {
		t.Errorf("Status = %q, want %q", list[0].Status, "complete")
	}
	if list[0].DurationMs != 1500 {
		t.Errorf("DurationMs = %d, want 1500", list[0].DurationMs)
	}
}
