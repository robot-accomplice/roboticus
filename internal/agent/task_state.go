package agent

import (
	"fmt"
	"time"
)

// TaskPhase represents the lifecycle state of a task.
type TaskPhase int

const (
	TaskPending    TaskPhase = iota // Not yet started
	TaskPlanning                    // Planning steps
	TaskExecuting                   // Executing steps
	TaskValidating                  // Validating results
	TaskComplete                    // Successfully done
	TaskFailed                      // Failed
)

func (p TaskPhase) String() string {
	switch p {
	case TaskPending:
		return "pending"
	case TaskPlanning:
		return "planning"
	case TaskExecuting:
		return "executing"
	case TaskValidating:
		return "validating"
	case TaskComplete:
		return "complete"
	case TaskFailed:
		return "failed"
	default:
		return fmt.Sprintf("unknown(%d)", int(p))
	}
}

// validTransitions defines which phase transitions are allowed.
var validTransitions = map[TaskPhase][]TaskPhase{
	TaskPending:    {TaskPlanning, TaskFailed},
	TaskPlanning:   {TaskExecuting, TaskFailed},
	TaskExecuting:  {TaskValidating, TaskFailed},
	TaskValidating: {TaskComplete, TaskFailed, TaskExecuting}, // can loop back
}

// TaskState tracks the lifecycle of an agent task.
type TaskState struct {
	ID          string
	Phase       TaskPhase
	ParentID    string // for subtasks
	Goal        string
	Steps       []TaskStep
	CurrentStep int
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// TaskStep represents a single step within a task.
type TaskStep struct {
	Description string
	Status      TaskPhase
	ToolCalls   []string // tool call IDs that contributed
	Output      string
}

// NewTaskState creates a new task in Pending phase.
func NewTaskState(id, goal string) *TaskState {
	now := time.Now()
	return &TaskState{
		ID:        id,
		Phase:     TaskPending,
		Goal:      goal,
		CreatedAt: now,
		UpdatedAt: now,
	}
}

// NewSubtask creates a child task linked to a parent.
func NewSubtask(id, parentID, goal string) *TaskState {
	ts := NewTaskState(id, goal)
	ts.ParentID = parentID
	return ts
}

// TransitionTo moves the task to a new phase if the transition is valid.
func (ts *TaskState) TransitionTo(target TaskPhase) error {
	allowed, ok := validTransitions[ts.Phase]
	if !ok {
		return fmt.Errorf("no transitions from %v", ts.Phase)
	}
	for _, a := range allowed {
		if a == target {
			ts.Phase = target
			ts.UpdatedAt = time.Now()
			return nil
		}
	}
	return fmt.Errorf("invalid transition: %v -> %v", ts.Phase, target)
}

// AddStep appends a new step to the task.
func (ts *TaskState) AddStep(description string) {
	ts.Steps = append(ts.Steps, TaskStep{
		Description: description,
		Status:      TaskPending,
	})
}

// CompleteCurrentStep marks the current step as complete and advances.
func (ts *TaskState) CompleteCurrentStep(output string) {
	if ts.CurrentStep < len(ts.Steps) {
		ts.Steps[ts.CurrentStep].Status = TaskComplete
		ts.Steps[ts.CurrentStep].Output = output
		ts.CurrentStep++
		ts.UpdatedAt = time.Now()
	}
}
