package db

import (
	"context"
	"testing"
)

func TestTasksRepository_CRUD(t *testing.T) {
	store := testTempStore(t)
	repo := NewTasksRepository(store)
	ctx := context.Background()

	if err := repo.Create(ctx, TaskRow{ID: "t1", Phase: "pending", Goal: "test task"}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := repo.Get(ctx, "t1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Goal != "test task" {
		t.Errorf("Goal = %q, want %q", got.Goal, "test task")
	}

	if err := repo.UpdatePhase(ctx, "t1", "executing"); err != nil {
		t.Fatalf("UpdatePhase: %v", err)
	}
	got, _ = repo.Get(ctx, "t1")
	if got.Phase != "executing" {
		t.Errorf("Phase = %q, want %q", got.Phase, "executing")
	}
}

func TestTasksRepository_Subtasks(t *testing.T) {
	store := testTempStore(t)
	repo := NewTasksRepository(store)
	ctx := context.Background()

	_ = repo.Create(ctx, TaskRow{ID: "parent", Phase: "executing", Goal: "parent"})
	_ = repo.Create(ctx, TaskRow{ID: "child1", Phase: "pending", ParentID: "parent", Goal: "child 1"})
	_ = repo.Create(ctx, TaskRow{ID: "child2", Phase: "pending", ParentID: "parent", Goal: "child 2"})

	subs, err := repo.ListSubtasks(ctx, "parent")
	if err != nil {
		t.Fatalf("ListSubtasks: %v", err)
	}
	if len(subs) != 2 {
		t.Errorf("got %d subtasks, want 2", len(subs))
	}
}

func TestTasksRepository_ListByPhase(t *testing.T) {
	store := testTempStore(t)
	repo := NewTasksRepository(store)
	ctx := context.Background()

	repo.Create(ctx, TaskRow{ID: "t1", Phase: "pending", Goal: "a"})
	repo.Create(ctx, TaskRow{ID: "t2", Phase: "executing", Goal: "b"})
	repo.Create(ctx, TaskRow{ID: "t3", Phase: "pending", Goal: "c"})

	pending, err := repo.List(ctx, "pending")
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 2 {
		t.Errorf("got %d pending, want 2", len(pending))
	}

	all, _ := repo.List(ctx, "")
	if len(all) != 3 {
		t.Errorf("got %d total, want 3", len(all))
	}
}
