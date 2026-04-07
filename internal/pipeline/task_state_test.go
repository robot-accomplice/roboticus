package pipeline

import (
	"testing"
	"time"
)

func TestTaskTracker_Lifecycle(t *testing.T) {
	tt := NewTaskTracker()

	// Create.
	task := tt.Create("t1", "s1", "hello world")
	if task.State != TaskPending {
		t.Fatalf("new task state = %v, want pending", task.State)
	}
	if task.ID != "t1" {
		t.Fatalf("task ID = %v, want t1", task.ID)
	}

	// Start.
	tt.Start("t1", "turn-1")
	got := tt.Get("t1")
	if got.State != TaskRunning {
		t.Fatalf("started task state = %v, want running", got.State)
	}
	if got.TurnID != "turn-1" {
		t.Fatalf("turn ID = %v, want turn-1", got.TurnID)
	}
	if got.StartedAt == nil {
		t.Fatal("started task should have StartedAt")
	}

	// Complete.
	tt.Complete("t1")
	got = tt.Get("t1")
	if got.State != TaskCompleted {
		t.Fatalf("completed task state = %v, want completed", got.State)
	}
	if got.CompletedAt == nil {
		t.Fatal("completed task should have CompletedAt")
	}
}

func TestTaskTracker_Fail(t *testing.T) {
	tt := NewTaskTracker()
	tt.Create("t1", "s1", "content")
	tt.Start("t1", "turn-1")
	tt.Fail("t1", "inference error")

	got := tt.Get("t1")
	if got.State != TaskFailed {
		t.Fatalf("state = %v, want failed", got.State)
	}
	if got.Error != "inference error" {
		t.Fatalf("error = %v", got.Error)
	}
}

func TestTaskTracker_Delegate(t *testing.T) {
	tt := NewTaskTracker()
	tt.Create("t1", "s1", "complex task")
	tt.Delegate("t1", "researcher", []string{"sub-1", "sub-2"})

	got := tt.Get("t1")
	if got.State != TaskDelegated {
		t.Fatalf("state = %v, want delegated", got.State)
	}
	if got.AssignedTo != "researcher" {
		t.Fatalf("assigned to = %v", got.AssignedTo)
	}
	if len(got.Subtasks) != 2 {
		t.Fatalf("subtask count = %d, want 2", len(got.Subtasks))
	}
}

func TestTaskTracker_Classify(t *testing.T) {
	tt := NewTaskTracker()
	tt.Create("t1", "s1", "content")
	tt.Classify("t1", TaskComplex)

	got := tt.Get("t1")
	if got.Classification != TaskComplex {
		t.Fatalf("classification = %v, want complex", got.Classification)
	}
}

func TestTaskTracker_Active(t *testing.T) {
	tt := NewTaskTracker()
	tt.Create("t1", "s1", "a")
	tt.Create("t2", "s1", "b")
	tt.Create("t3", "s1", "c")
	tt.Start("t1", "turn-1")
	tt.Complete("t2")

	active := tt.Active()
	// t1 = running, t3 = pending — both are active. t2 = completed — not active.
	if len(active) != 2 {
		t.Fatalf("active count = %d, want 2", len(active))
	}
}

func TestTaskTracker_Cleanup(t *testing.T) {
	tt := NewTaskTracker()
	tt.Create("t1", "s1", "a")
	tt.Complete("t1")
	// Cheat: set CompletedAt to the past.
	tt.tasks["t1"].CompletedAt = func() *time.Time { t := time.Now().Add(-2 * time.Hour); return &t }()

	removed := tt.Cleanup(1 * time.Hour)
	if removed != 1 {
		t.Fatalf("removed = %d, want 1", removed)
	}
	if tt.Get("t1") != nil {
		t.Fatal("cleaned task should be gone")
	}
}

func TestTaskTracker_GetMissing(t *testing.T) {
	tt := NewTaskTracker()
	if tt.Get("nonexistent") != nil {
		t.Fatal("missing task should return nil")
	}
}

func TestTaskState_String(t *testing.T) {
	tests := []struct {
		state TaskState
		want  string
	}{
		{TaskPending, "pending"},
		{TaskRunning, "running"},
		{TaskCompleted, "completed"},
		{TaskFailed, "failed"},
		{TaskDelegated, "delegated"},
		{TaskState(99), "unknown"},
	}
	for _, tt := range tests {
		if got := tt.state.String(); got != tt.want {
			t.Errorf("TaskState(%d).String() = %q, want %q", tt.state, got, tt.want)
		}
	}
}

func TestTaskClassification_String(t *testing.T) {
	tests := []struct {
		cls  TaskClassification
		want string
	}{
		{TaskSimple, "simple"},
		{TaskComplex, "complex"},
		{TaskMultiStep, "multi_step"},
		{TaskSpecialist, "specialist"},
		{TaskClassification(99), "unknown"},
	}
	for _, tt := range tests {
		if got := tt.cls.String(); got != tt.want {
			t.Errorf("TaskClassification(%d).String() = %q, want %q", tt.cls, got, tt.want)
		}
	}
}
