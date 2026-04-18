package schedule

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"
	"time"

	"roboticus/internal/core"
)

type stubDB struct {
	execQuery string
	execArgs  []any
	execErr   error
	execCount int
}

func (s *stubDB) ExecContext(_ context.Context, query string, args ...any) (sql.Result, error) {
	s.execCount++
	s.execQuery = query
	s.execArgs = args
	return stubResult(1), s.execErr
}

func (s *stubDB) QueryRowContext(_ context.Context, _ string, _ ...any) *sql.Row {
	return &sql.Row{}
}

type stubResult int64

func (r stubResult) LastInsertId() (int64, error) { return int64(r), nil }
func (r stubResult) RowsAffected() (int64, error) { return int64(r), nil }

func TestMemoryLoopTask_Run_UsesConsolidationFunc(t *testing.T) {
	called := false
	task := &MemoryLoopTask{
		Consolidate: func(ctx context.Context, force bool) string {
			called = true
			if force {
				t.Fatal("force should be false on heartbeat consolidation")
			}
			return "indexed=1 deduped=2 promoted=3 pruned=4"
		},
	}

	result := task.Run(context.Background(), &TickContext{})
	if !called {
		t.Fatal("consolidation function was not called")
	}
	if !result.Success {
		t.Fatal("task should succeed")
	}
	if result.Message != "indexed=1 deduped=2 promoted=3 pruned=4" {
		t.Fatalf("message = %q", result.Message)
	}
}

func TestMetricSnapshotTask_Run_WritesCurrentSchema(t *testing.T) {
	store := &stubDB{}
	task := &MetricSnapshotTask{Store: store}
	tctx := &TickContext{
		CreditBalance: 12.5,
		USDCBalance:   42.75,
		SurvivalTier:  core.SurvivalTierStable,
		Timestamp:     time.Date(2026, 4, 17, 10, 30, 0, 0, time.UTC),
	}

	result := task.Run(context.Background(), tctx)
	if !result.Success {
		t.Fatalf("task should succeed: %+v", result)
	}
	if store.execQuery == "" {
		t.Fatal("expected insert query to run")
	}
	if want := "INSERT INTO metric_snapshots (id, metrics_json, alerts_json)"; store.execQuery[:len(want)] != want {
		t.Fatalf("query = %q", store.execQuery)
	}
	if len(store.execArgs) != 1 {
		t.Fatalf("arg count = %d, want 1", len(store.execArgs))
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(store.execArgs[0].(string)), &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if payload["survival_tier"] != "stable" {
		t.Fatalf("survival_tier = %v", payload["survival_tier"])
	}
	if payload["timestamp"] != "2026-04-17T10:30:00Z" {
		t.Fatalf("timestamp = %v", payload["timestamp"])
	}
}

func TestMaintenanceLoopTask_Run_ExecutesCleanupQueries(t *testing.T) {
	store := &stubDB{}
	task := &MaintenanceLoopTask{Store: store}

	result := task.Run(context.Background(), &TickContext{})
	if !result.Success {
		t.Fatalf("task should succeed: %+v", result)
	}
	if store.execCount != 2 {
		t.Fatalf("exec count = %d, want 2", store.execCount)
	}
	if result.Message != "evicted=1 leases_cleared=1" {
		t.Fatalf("message = %q", result.Message)
	}
}
