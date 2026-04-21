package db_test

import (
	"context"
	"testing"

	"roboticus/internal/db"
	"roboticus/testutil"
)

func seedOrchestrationSubagent(t *testing.T, store *db.Store, id, name string, skillsJSON string) {
	t.Helper()
	if _, err := store.ExecContext(context.Background(),
		`INSERT INTO sub_agents (id, name, model, role, description, skills_json, enabled)
		 VALUES (?, ?, 'ollama/phi4-mini:latest', 'subagent', 'delegated specialist', ?, 1)`,
		id, name, skillsJSON,
	); err != nil {
		t.Fatalf("seed subagent: %v", err)
	}
}

func TestSubagentOrchestrationRepository_CreateWorkflow(t *testing.T) {
	store := testutil.TempStore(t)
	seedOrchestrationSubagent(t, store, "sa-1", "researcher", `["research","search"]`)
	seedOrchestrationSubagent(t, store, "sa-2", "writer", `["writing","summarize"]`)

	repo := db.NewSubagentOrchestrationRepository(store)
	workflow, err := repo.CreateWorkflow(context.Background(), db.OrchestrationPlanSpec{
		WorkflowName: "Evidence sweep",
		Pattern:      db.OrchestrationPatternFanOut,
		RequestedBy:  "Duncan",
		Subtasks: []db.OrchestrationSubtaskSpec{
			{Description: "Find source evidence", RequiredSkills: []string{"research"}},
			{Description: "Write summary", RequiredSkills: []string{"writing"}},
		},
	})
	if err != nil {
		t.Fatalf("CreateWorkflow: %v", err)
	}
	if workflow.WorkflowID == "" || len(workflow.Assignments) != 2 {
		t.Fatalf("unexpected workflow: %+v", workflow)
	}
	if workflow.Assignments[0].AssignedSubagent != "researcher" {
		t.Fatalf("first assignment = %+v, want researcher", workflow.Assignments[0])
	}
	if workflow.Assignments[1].AssignedSubagent != "writer" {
		t.Fatalf("second assignment = %+v, want writer", workflow.Assignments[1])
	}

	var taskCount, eventCount, outcomeCount int
	if err := store.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM tasks WHERE id = ?`, workflow.WorkflowID).Scan(&taskCount); err != nil {
		t.Fatalf("count tasks: %v", err)
	}
	if err := store.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM task_events WHERE task_id = ?`, workflow.WorkflowID).Scan(&eventCount); err != nil {
		t.Fatalf("count task_events: %v", err)
	}
	if err := store.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM agent_delegation_outcomes WHERE parent_task_id = ?`, workflow.WorkflowID).Scan(&outcomeCount); err != nil {
		t.Fatalf("count outcomes: %v", err)
	}
	if taskCount != 1 || eventCount != 3 || outcomeCount != 2 {
		t.Fatalf("unexpected persisted counts: tasks=%d events=%d outcomes=%d", taskCount, eventCount, outcomeCount)
	}
}

func TestSubagentOrchestrationRepository_WorkflowLifecycle(t *testing.T) {
	store := testutil.TempStore(t)
	seedOrchestrationSubagent(t, store, "sa-1", "researcher", `["research"]`)

	repo := db.NewSubagentOrchestrationRepository(store)
	workflow, err := repo.CreateWorkflow(context.Background(), db.OrchestrationPlanSpec{
		WorkflowName: "Evidence sweep",
		Subtasks: []db.OrchestrationSubtaskSpec{
			{Description: "Find source evidence", RequiredSkills: []string{"research"}},
		},
	})
	if err != nil {
		t.Fatalf("CreateWorkflow: %v", err)
	}

	if err := repo.MarkThreadedToInference(context.Background(), workflow.WorkflowID, "partial evidence returned"); err != nil {
		t.Fatalf("MarkThreadedToInference: %v", err)
	}
	var status string
	if err := store.QueryRowContext(context.Background(), `SELECT status FROM tasks WHERE id = ?`, workflow.WorkflowID).Scan(&status); err != nil {
		t.Fatalf("query status: %v", err)
	}
	if status != "in_progress" {
		t.Fatalf("status after threaded = %q, want in_progress", status)
	}

	if err := repo.MarkCompleted(context.Background(), workflow.WorkflowID, "complete synthesis"); err != nil {
		t.Fatalf("MarkCompleted: %v", err)
	}
	if err := store.QueryRowContext(context.Background(), `SELECT status FROM tasks WHERE id = ?`, workflow.WorkflowID).Scan(&status); err != nil {
		t.Fatalf("query completed status: %v", err)
	}
	if status != "completed" {
		t.Fatalf("status after complete = %q, want completed", status)
	}

	var outcomeStatus, outcomeSummary string
	if err := store.QueryRowContext(context.Background(),
		`SELECT status, result_summary FROM agent_delegation_outcomes WHERE parent_task_id = ? LIMIT 1`, workflow.WorkflowID).
		Scan(&outcomeStatus, &outcomeSummary); err != nil {
		t.Fatalf("query outcome: %v", err)
	}
	if outcomeStatus != "completed" || outcomeSummary != "complete synthesis" {
		t.Fatalf("unexpected outcome update: status=%q summary=%q", outcomeStatus, outcomeSummary)
	}
}
