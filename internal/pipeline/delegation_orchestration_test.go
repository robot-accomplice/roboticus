package pipeline

import (
	"context"
	"strings"
	"testing"

	"roboticus/internal/db"
	"roboticus/internal/session"
	"roboticus/testutil"
)

func seedDelegationSubagent(t *testing.T, store *db.Store, id, name string, skillsJSON string) {
	t.Helper()
	if _, err := store.ExecContext(context.Background(),
		`INSERT INTO sub_agents (id, name, model, role, description, skills_json, enabled)
		 VALUES (?, ?, 'ollama/phi4-mini:latest', 'subagent', 'delegated specialist', ?, 1)`,
		id, name, skillsJSON,
	); err != nil {
		t.Fatalf("seed subagent: %v", err)
	}
}

func TestExecuteDelegation_UsesOrchestrationControlPlane(t *testing.T) {
	store := testutil.TempStore(t)
	seedDelegationSubagent(t, store, "sa-1", "researcher", `["research"]`)
	seedDelegationSubagent(t, store, "sa-2", "writer", `["writing"]`)

	pipe := New(PipelineDeps{
		Store:    store,
		Executor: &stubExecutor{response: "Found the evidence and wrote the final summary in a structured report."},
		BGWorker: testutil.BGWorker(t, 1),
	})
	sess := session.New("sess-1", "duncan", "Duncan")

	outcome := pipe.executeDelegation(context.Background(), sess, &DecompositionResult{
		Decision: DecompDelegated,
		Subtasks: []string{"Find source evidence", "Write final summary"},
	}, "turn-1")
	if outcome == nil || !outcome.Complete {
		t.Fatalf("unexpected delegation outcome: %+v", outcome)
	}

	var workflowID, status string
	if err := store.QueryRowContext(context.Background(),
		`SELECT id, status FROM tasks WHERE source = 'orchestrate-subagents' LIMIT 1`).
		Scan(&workflowID, &status); err != nil {
		t.Fatalf("query workflow task: %v", err)
	}
	if status != "completed" {
		t.Fatalf("workflow status = %q, want completed", status)
	}

	var outcomeCount int
	if err := store.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM agent_delegation_outcomes WHERE parent_task_id = ?`, workflowID).
		Scan(&outcomeCount); err != nil {
		t.Fatalf("query outcomes: %v", err)
	}
	if outcomeCount != 2 {
		t.Fatalf("outcome count = %d, want 2", outcomeCount)
	}

	foundPlan := false
	for _, msg := range sess.Messages() {
		if msg.Role == "system" && strings.Contains(msg.Content, "[Delegation Plan]") {
			foundPlan = true
			break
		}
	}
	if !foundPlan {
		t.Fatal("expected delegation plan system message")
	}
}

func TestExecuteDelegation_ThreadsIncompleteWorkflowBackToInference(t *testing.T) {
	store := testutil.TempStore(t)
	seedDelegationSubagent(t, store, "sa-1", "researcher", `["research"]`)

	pipe := New(PipelineDeps{
		Store:    store,
		Executor: &stubExecutor{response: "Too short"},
		BGWorker: testutil.BGWorker(t, 1),
	})
	sess := session.New("sess-1", "duncan", "Duncan")

	outcome := pipe.executeDelegation(context.Background(), sess, &DecompositionResult{
		Decision: DecompDelegated,
		Subtasks: []string{"Find source evidence"},
	}, "turn-1")
	if outcome == nil || outcome.Complete {
		t.Fatalf("unexpected delegation outcome: %+v", outcome)
	}

	var status string
	if err := store.QueryRowContext(context.Background(),
		`SELECT status FROM tasks WHERE source = 'orchestrate-subagents' LIMIT 1`).
		Scan(&status); err != nil {
		t.Fatalf("query workflow task: %v", err)
	}
	if status != "in_progress" {
		t.Fatalf("workflow status = %q, want in_progress", status)
	}
}
