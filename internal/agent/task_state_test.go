package agent

import (
	"testing"
	"time"
)

func TestTaskState_Transitions(t *testing.T) {
	tests := []struct {
		name    string
		from    TaskPhase
		to      TaskPhase
		wantErr bool
	}{
		{"pending to planning", TaskPending, TaskPlanning, false},
		{"planning to executing", TaskPlanning, TaskExecuting, false},
		{"executing to validating", TaskExecuting, TaskValidating, false},
		{"validating to complete", TaskValidating, TaskComplete, false},
		{"executing to failed", TaskExecuting, TaskFailed, false},
		{"pending to complete (skip)", TaskPending, TaskComplete, true},
		{"complete to executing (backward)", TaskComplete, TaskExecuting, true},
		{"failed to executing (backward)", TaskFailed, TaskExecuting, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := NewTaskState("task-1", "do something")
			ts.Phase = tt.from
			err := ts.TransitionTo(tt.to)
			if (err != nil) != tt.wantErr {
				t.Errorf("TransitionTo(%v->%v) error = %v, wantErr %v", tt.from, tt.to, err, tt.wantErr)
			}
			if err == nil && ts.Phase != tt.to {
				t.Errorf("phase = %v after transition, want %v", ts.Phase, tt.to)
			}
		})
	}
}

func TestTaskState_AddStep(t *testing.T) {
	ts := NewTaskState("task-1", "test task")
	ts.AddStep("step 1")
	ts.AddStep("step 2")

	if len(ts.Steps) != 2 {
		t.Errorf("step count = %d, want 2", len(ts.Steps))
	}
	if ts.Steps[0].Description != "step 1" {
		t.Errorf("step[0] = %q, want %q", ts.Steps[0].Description, "step 1")
	}
}

func TestTaskState_CompleteCurrentStep(t *testing.T) {
	ts := NewTaskState("task-1", "test task")
	ts.AddStep("step 1")
	ts.AddStep("step 2")

	ts.CompleteCurrentStep("done")
	if ts.Steps[0].Status != TaskComplete {
		t.Errorf("step[0] status = %v, want Complete", ts.Steps[0].Status)
	}
	if ts.Steps[0].Output != "done" {
		t.Errorf("step[0] output = %q, want %q", ts.Steps[0].Output, "done")
	}
	if ts.CurrentStep != 1 {
		t.Errorf("currentStep = %d, want 1", ts.CurrentStep)
	}
}

func TestTaskState_Subtask(t *testing.T) {
	parent := NewTaskState("parent-1", "parent task")
	child := NewSubtask("child-1", "parent-1", "child task")

	if child.ParentID != "parent-1" {
		t.Errorf("ParentID = %q, want %q", child.ParentID, "parent-1")
	}
	_ = parent // parent exists for context
}

func TestTaskState_NewDefaults(t *testing.T) {
	ts := NewTaskState("t1", "goal")
	if ts.Phase != TaskPending {
		t.Errorf("default phase = %v, want Pending", ts.Phase)
	}
	if ts.CreatedAt.IsZero() {
		t.Error("CreatedAt should be set")
	}
}

// Ensure time package is used (referenced in TaskState struct via CreatedAt/UpdatedAt).
var _ = time.Now
