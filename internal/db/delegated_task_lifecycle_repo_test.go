package db

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func seedDelegatedTask(t *testing.T, store *Store, id, title, status string) {
	t.Helper()
	_, err := store.ExecContext(context.Background(),
		`INSERT INTO tasks (id, title, description, status, priority, source)
		 VALUES (?, ?, 'desc', ?, 1, '{"kind":"delegated"}')`,
		id, title, status,
	)
	if err != nil {
		t.Fatalf("seed task: %v", err)
	}
}

func seedTaskEvent(t *testing.T, store *Store, taskID, assignedTo, eventType string) {
	t.Helper()
	_, err := store.ExecContext(context.Background(),
		`INSERT INTO task_events (id, task_id, assigned_to, event_type, payload_json)
		 VALUES (?, ?, NULLIF(?, ''), ?, '{}')`,
		uuid.NewString(), taskID, assignedTo, eventType,
	)
	if err != nil {
		t.Fatalf("seed task event: %v", err)
	}
}

func seedDelegationOutcome(t *testing.T, store *Store, parentTaskID, subagentID, status string) {
	t.Helper()
	_, err := store.ExecContext(context.Background(),
		`INSERT INTO agent_delegation_outcomes (id, parent_task_id, subagent_id, status, result_summary)
		 VALUES (?, ?, ?, ?, 'done')`,
		uuid.NewString(), parentTaskID, subagentID, status,
	)
	if err != nil {
		t.Fatalf("seed delegation outcome: %v", err)
	}
}

func TestDelegatedTaskLifecycleRepository_ListOpen(t *testing.T) {
	store := testTempStore(t)
	ctx := context.Background()
	repo := NewDelegatedTaskLifecycleRepository(store)

	seedDelegatedTask(t, store, "open-1", "Open 1", "pending")
	seedDelegatedTask(t, store, "open-2", "Open 2", "submitting")
	seedDelegatedTask(t, store, "closed-1", "Closed", "failed")
	seedTaskEvent(t, store, "open-1", "scribe", "assigned")

	got, err := repo.ListOpen(ctx, 10)
	if err != nil {
		t.Fatalf("ListOpen: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d open tasks, want 2", len(got))
	}
	for _, task := range got {
		if strings.EqualFold(task.Status, "failed") {
			t.Fatalf("closed task leaked into open list: %+v", task)
		}
	}
	foundAssigned := false
	for _, task := range got {
		if task.ID == "open-1" {
			foundAssigned = true
			if task.AssignedTo != "scribe" {
				t.Fatalf("assigned_to = %q, want scribe", task.AssignedTo)
			}
		}
	}
	if !foundAssigned {
		t.Fatal("expected open-1 in results")
	}
}

func TestDelegatedTaskLifecycleRepository_GetStatus(t *testing.T) {
	store := testTempStore(t)
	ctx := context.Background()
	repo := NewDelegatedTaskLifecycleRepository(store)

	seedDelegatedTask(t, store, "task-1", "Task 1", "in_progress")
	seedTaskEvent(t, store, "task-1", "scribe", "started")
	seedDelegationOutcome(t, store, "task-1", "scribe", "completed")

	got, err := repo.GetStatus(ctx, "task-1", 10)
	if err != nil {
		t.Fatalf("GetStatus: %v", err)
	}
	if got == nil {
		t.Fatal("expected task status")
	}
	if got.Task.AssignedTo != "scribe" {
		t.Fatalf("AssignedTo = %q, want scribe", got.Task.AssignedTo)
	}
	if len(got.Events) != 1 {
		t.Fatalf("events = %d, want 1", len(got.Events))
	}
	if len(got.Outcomes) != 1 {
		t.Fatalf("outcomes = %d, want 1", len(got.Outcomes))
	}
}

func TestDelegatedTaskLifecycleRepository_Retry(t *testing.T) {
	store := testTempStore(t)
	ctx := context.Background()
	repo := NewDelegatedTaskLifecycleRepository(store)

	seedDelegatedTask(t, store, "task-1", "Task 1", "failed")

	result, err := repo.Retry(ctx, "task-1", "try again", "orchestrator")
	if err != nil {
		t.Fatalf("Retry: %v", err)
	}
	if result == nil || !result.Updated {
		t.Fatalf("expected updated retry result, got %+v", result)
	}
	if result.PriorStatus != "failed" {
		t.Fatalf("PriorStatus = %q, want failed", result.PriorStatus)
	}
	if result.Task == nil || result.Task.Task.Status != "pending" {
		t.Fatalf("task status = %+v, want pending", result.Task)
	}

	status, err := repo.GetStatus(ctx, "task-1", 10)
	if err != nil {
		t.Fatalf("GetStatus after retry: %v", err)
	}
	if len(status.Events) == 0 || status.Events[0].EventType != "retry_requested" {
		t.Fatalf("expected retry_requested event, got %+v", status.Events)
	}
}
