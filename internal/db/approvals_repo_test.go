package db

import (
	"context"
	"testing"
)

func TestApprovalsRepository_CreateAndList(t *testing.T) {
	store := testTempStore(t)
	repo := NewApprovalsRepository(store)
	ctx := context.Background()

	row := ApprovalRow{
		ID:        "apr-1",
		ToolName:  "bash",
		ToolInput: `{"cmd":"rm -rf /"}`,
		SessionID: "sess-1",
		TimeoutAt: "2099-01-01 00:00:00",
	}
	if err := repo.Create(ctx, row); err != nil {
		t.Fatalf("Create: %v", err)
	}

	pending, err := repo.ListPending(ctx)
	if err != nil {
		t.Fatalf("ListPending: %v", err)
	}
	if len(pending) != 1 {
		t.Errorf("got %d pending, want 1", len(pending))
	}
	if pending[0].ToolName != "bash" {
		t.Errorf("ToolName = %q, want bash", pending[0].ToolName)
	}
}

func TestApprovalsRepository_ApproveAndDeny(t *testing.T) {
	store := testTempStore(t)
	repo := NewApprovalsRepository(store)
	ctx := context.Background()

	if err := repo.Create(ctx, ApprovalRow{
		ID: "apr-1", ToolName: "web_search", ToolInput: `{}`,
		SessionID: "sess-1", TimeoutAt: "2099-01-01 00:00:00",
	}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := repo.Create(ctx, ApprovalRow{
		ID: "apr-2", ToolName: "file_write", ToolInput: `{}`,
		SessionID: "sess-1", TimeoutAt: "2099-01-01 00:00:00",
	}); err != nil {
		t.Fatalf("Create second: %v", err)
	}

	if err := repo.Approve(ctx, "apr-1", "alice"); err != nil {
		t.Fatalf("Approve: %v", err)
	}
	if err := repo.Deny(ctx, "apr-2", "bob"); err != nil {
		t.Fatalf("Deny: %v", err)
	}

	// No more pending.
	pending, _ := repo.ListPending(ctx)
	if len(pending) != 0 {
		t.Errorf("expected 0 pending after decisions, got %d", len(pending))
	}

	approved, err := repo.Get(ctx, "apr-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if approved.Status != "approved" {
		t.Errorf("Status = %q, want approved", approved.Status)
	}
	if approved.DecidedBy != "alice" {
		t.Errorf("DecidedBy = %q, want alice", approved.DecidedBy)
	}
}

func TestApprovalsRepository_Get_NotFound(t *testing.T) {
	store := testTempStore(t)
	repo := NewApprovalsRepository(store)
	ctx := context.Background()

	got, err := repo.Get(ctx, "nonexistent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Error("expected nil for missing approval")
	}
}
