package db

import (
	"context"
	"testing"
)

func TestToolsRepository_RecordAndGet(t *testing.T) {
	store := testTempStore(t)
	repo := NewToolsRepository(store)
	ctx := context.Background()

	seedTurnForTrace(t, store, "sess-1", "turn-1")

	row := ToolCallRow{
		ID:       "tc-1",
		TurnID:   "turn-1",
		ToolName: "web_search",
		Input:    `{"query":"golang testing"}`,
		Status:   "success",
	}
	if err := repo.Record(ctx, row); err != nil {
		t.Fatalf("Record: %v", err)
	}

	got, err := repo.GetByID(ctx, "tc-1")
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got == nil {
		t.Fatal("expected row, got nil")
	}
	if got.ToolName != "web_search" {
		t.Errorf("ToolName = %q, want web_search", got.ToolName)
	}
	if got.Status != "success" {
		t.Errorf("Status = %q, want success", got.Status)
	}
}

func TestToolsRepository_RecordIsIdempotentForDuplicateID(t *testing.T) {
	store := testTempStore(t)
	repo := NewToolsRepository(store)
	ctx := context.Background()

	seedTurnForTrace(t, store, "sess-1", "turn-1")

	if err := repo.Record(ctx, ToolCallRow{
		ID:         "turn-1:call-1",
		TurnID:     "turn-1",
		ToolName:   "list_directory",
		Input:      `{"path":"."}`,
		Output:     "first",
		Status:     "running",
		DurationMs: 1,
	}); err != nil {
		t.Fatalf("first Record: %v", err)
	}
	if err := repo.Record(ctx, ToolCallRow{
		ID:         "turn-1:call-1",
		TurnID:     "turn-1",
		ToolName:   "list_directory",
		Input:      `{"path":"."}`,
		Output:     "second",
		Status:     "success",
		DurationMs: 42,
	}); err != nil {
		t.Fatalf("duplicate Record should update idempotently: %v", err)
	}

	calls, err := repo.ListByTurn(ctx, "turn-1")
	if err != nil {
		t.Fatalf("ListByTurn: %v", err)
	}
	if len(calls) != 1 {
		t.Fatalf("duplicate execution id created %d rows, want 1", len(calls))
	}
	if calls[0].Output != "second" || calls[0].Status != "success" || calls[0].DurationMs != 42 {
		t.Fatalf("row not updated idempotently: %+v", calls[0])
	}
}

func TestToolsRepository_ListByTurn(t *testing.T) {
	store := testTempStore(t)
	repo := NewToolsRepository(store)
	ctx := context.Background()

	seedTurnForTrace(t, store, "sess-1", "turn-1")

	for i, name := range []string{"bash", "read_file", "write_file"} {
		_ = repo.Record(ctx, ToolCallRow{
			ID: NewID(), TurnID: "turn-1", ToolName: name,
			Input: `{}`, Status: "success",
			DurationMs: int64(i * 10),
		})
	}

	calls, err := repo.ListByTurn(ctx, "turn-1")
	if err != nil {
		t.Fatalf("ListByTurn: %v", err)
	}
	if len(calls) != 3 {
		t.Errorf("got %d calls, want 3", len(calls))
	}
}

func TestToolsRepository_UpdateOutput(t *testing.T) {
	store := testTempStore(t)
	repo := NewToolsRepository(store)
	ctx := context.Background()

	seedTurnForTrace(t, store, "sess-1", "turn-1")

	_ = repo.Record(ctx, ToolCallRow{
		ID: "tc-1", TurnID: "turn-1", ToolName: "bash",
		Input: `{"cmd":"ls"}`, Status: "running",
	})

	if err := repo.UpdateOutput(ctx, "tc-1", "file1.txt\nfile2.txt", "success", 42); err != nil {
		t.Fatalf("UpdateOutput: %v", err)
	}

	got, _ := repo.GetByID(ctx, "tc-1")
	if got.Output != "file1.txt\nfile2.txt" {
		t.Errorf("Output = %q, want file listing", got.Output)
	}
	if got.DurationMs != 42 {
		t.Errorf("DurationMs = %d, want 42", got.DurationMs)
	}
}

func TestToolsRepository_GetByID_NotFound(t *testing.T) {
	store := testTempStore(t)
	repo := NewToolsRepository(store)
	ctx := context.Background()

	got, err := repo.GetByID(ctx, "nonexistent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Error("expected nil for missing tool call")
	}
}
