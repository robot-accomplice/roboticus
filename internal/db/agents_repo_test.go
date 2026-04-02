package db

import (
	"context"
	"testing"
)

func TestAgentsRepository_CRUD(t *testing.T) {
	store := testTempStore(t)
	repo := NewAgentsRepository(store)
	ctx := context.Background()

	// Save
	row := AgentInstanceRow{ID: "inst-1", AgentID: "agent-1", Name: "worker", Status: "registered"}
	if err := repo.Save(ctx, row); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// List
	list, err := repo.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("got %d agents, want 1", len(list))
	}
	if list[0].Name != "worker" {
		t.Errorf("Name = %q, want %q", list[0].Name, "worker")
	}

	// UpdateStatus
	if err := repo.UpdateStatus(ctx, "inst-1", "running", ""); err != nil {
		t.Fatalf("UpdateStatus: %v", err)
	}
	list, _ = repo.List(ctx)
	if list[0].Status != "running" {
		t.Errorf("Status = %q, want %q", list[0].Status, "running")
	}

	// Delete
	if err := repo.Delete(ctx, "inst-1"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	list, _ = repo.List(ctx)
	if len(list) != 0 {
		t.Errorf("got %d agents after delete, want 0", len(list))
	}
}
