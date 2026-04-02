package db

import (
	"context"
	"testing"
)

func testTempStore(t *testing.T) *Store {
	t.Helper()
	store, err := Open(":memory:")
	if err != nil {
		t.Fatalf("testTempStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

// seedTurnForTrace creates the parent session+turn rows needed for FK constraints.
func seedTurnForTrace(t *testing.T, store *Store, sessionID, turnID string) {
	t.Helper()
	ctx := context.Background()
	_, _ = store.ExecContext(ctx, `INSERT OR IGNORE INTO sessions (id, agent_id, scope_key) VALUES (?, 'test-agent', 'test')`, sessionID)
	_, err := store.ExecContext(ctx, `INSERT OR IGNORE INTO turns (id, session_id) VALUES (?, ?)`, turnID, sessionID)
	if err != nil {
		t.Fatalf("seedTurnForTrace: %v", err)
	}
}

func TestTraceRepository_SaveAndLoad(t *testing.T) {
	store := testTempStore(t)
	repo := NewTraceRepository(store)
	ctx := context.Background()

	seedTurnForTrace(t, store, "sess-1", "turn-1")

	row := PipelineTraceRow{
		ID:         "trace-1",
		TurnID:     "turn-1",
		SessionID:  "sess-1",
		Channel:    "api",
		TotalMs:    150,
		StagesJSON: `[{"name":"validation","duration_ms":5}]`,
	}

	if err := repo.SavePipelineTrace(ctx, row); err != nil {
		t.Fatalf("SavePipelineTrace: %v", err)
	}

	if err := repo.SaveReactTrace(ctx, "react-1", "trace-1", `{"steps":[]}`); err != nil {
		t.Fatalf("SaveReactTrace: %v", err)
	}

	got, err := repo.GetByTurnID(ctx, "turn-1")
	if err != nil {
		t.Fatalf("GetByTurnID: %v", err)
	}
	if got == nil {
		t.Fatal("GetByTurnID returned nil")
	}
	if got.Channel != "api" {
		t.Errorf("Channel = %q, want %q", got.Channel, "api")
	}
	if got.TotalMs != 150 {
		t.Errorf("TotalMs = %d, want 150", got.TotalMs)
	}
	if got.SessionID != "sess-1" {
		t.Errorf("SessionID = %q, want %q", got.SessionID, "sess-1")
	}
}

func TestTraceRepository_GetByTurnID_NotFound(t *testing.T) {
	store := testTempStore(t)
	repo := NewTraceRepository(store)
	ctx := context.Background()

	got, err := repo.GetByTurnID(ctx, "nonexistent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Error("expected nil for nonexistent turn")
	}
}

func TestTraceRepository_ListTraces(t *testing.T) {
	store := testTempStore(t)
	repo := NewTraceRepository(store)
	ctx := context.Background()

	for i, ch := range []string{"api", "telegram", "api"} {
		turnID := NewID()
		seedTurnForTrace(t, store, "sess-1", turnID)
		row := PipelineTraceRow{
			ID:         NewID(),
			TurnID:     turnID,
			SessionID:  "sess-1",
			Channel:    ch,
			TotalMs:    int64(i * 100),
			StagesJSON: "[]",
		}
		if err := repo.SavePipelineTrace(ctx, row); err != nil {
			t.Fatal(err)
		}
	}

	all, err := repo.ListTraces(ctx, TraceFilter{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 3 {
		t.Errorf("got %d traces, want 3", len(all))
	}

	filtered, err := repo.ListTraces(ctx, TraceFilter{Channel: "api", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(filtered) != 2 {
		t.Errorf("got %d api traces, want 2", len(filtered))
	}

	bySession, err := repo.ListTraces(ctx, TraceFilter{SessionID: "sess-1", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(bySession) != 3 {
		t.Errorf("got %d session traces, want 3", len(bySession))
	}
}
