package pipeline

import (
	"context"
	"testing"

	"roboticus/testutil"
)

// TestObserverDispatch_SendsToObservers verifies that post-turn ingest
// dispatches turn summaries to observer subagents.
func TestObserverDispatch_SendsToObservers(t *testing.T) {
	store := testutil.TempStore(t)
	ctx := context.Background()

	// Create a session and an observer subagent.
	_, _ = store.ExecContext(ctx,
		`INSERT INTO sessions (id, agent_id, scope_key) VALUES ('obs-sess', 'default', 'test:obs')`)
	_, _ = store.ExecContext(ctx,
		`INSERT INTO sub_agents (id, name, model, role, enabled)
		 VALUES ('obs-1', 'observer-1', 'test', 'observer', 1)`)

	pipe := New(PipelineDeps{
		Store:    store,
		Executor: &stubExecutor{response: "observed"},
		BGWorker: testutil.BGWorker(t, 4),
	})

	sess := NewSession("obs-sess", "default", "TestBot")
	sess.AddUserMessage("test message")

	// Dispatch to observers.
	pipe.dispatchToObservers(ctx, "obs-sess", "turn-1", "user says hello", "bot says hi")

	// Verify episodic memory was created with owner_id = observer.
	var count int
	row := store.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM episodic_memory WHERE owner_id = 'obs-1' AND classification = 'observation'`)
	if err := row.Scan(&count); err != nil {
		t.Fatalf("query: %v", err)
	}
	if count == 0 {
		t.Error("observer subagent did not receive turn observation")
	}

	// Verify last_used_at was updated.
	var lastUsed *string
	row = store.QueryRowContext(ctx, `SELECT last_used_at FROM sub_agents WHERE id = 'obs-1'`)
	_ = row.Scan(&lastUsed)
	if lastUsed == nil {
		t.Error("observer last_used_at should be updated")
	}
}

// TestObserverDispatch_NoObservers is a no-op when no observers exist.
func TestObserverDispatch_NoObservers(t *testing.T) {
	store := testutil.TempStore(t)
	pipe := New(PipelineDeps{
		Store:    store,
		BGWorker: testutil.BGWorker(t, 2),
	})

	// Should not panic or error.
	pipe.dispatchToObservers(context.Background(), "no-sess", "t-1", "hello", "world")
}
