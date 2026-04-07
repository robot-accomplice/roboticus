package pipeline

import (
	"sync"
	"time"
)

// TaskState represents the lifecycle state of a pipeline task.
type TaskState int

const (
	TaskPending   TaskState = iota // created, waiting to execute
	TaskRunning                    // actively being processed
	TaskCompleted                  // finished successfully
	TaskFailed                     // finished with error
	TaskDelegated                  // handed off to a subagent
)

// String returns the human-readable state name.
func (s TaskState) String() string {
	switch s {
	case TaskPending:
		return "pending"
	case TaskRunning:
		return "running"
	case TaskCompleted:
		return "completed"
	case TaskFailed:
		return "failed"
	case TaskDelegated:
		return "delegated"
	default:
		return "unknown"
	}
}

// TaskClassification describes the nature of a pipeline task for planning.
type TaskClassification int

const (
	TaskSimple       TaskClassification = iota // single-turn, no decomposition needed
	TaskComplex                                // may benefit from decomposition
	TaskMultiStep                              // requires explicit planning
	TaskSpecialist                             // needs specialist agent creation
)

// String returns the classification name.
func (c TaskClassification) String() string {
	switch c {
	case TaskSimple:
		return "simple"
	case TaskComplex:
		return "complex"
	case TaskMultiStep:
		return "multi_step"
	case TaskSpecialist:
		return "specialist"
	default:
		return "unknown"
	}
}

// Task tracks the lifecycle of a single pipeline request through the system.
type Task struct {
	ID             string
	SessionID      string
	TurnID         string
	State          TaskState
	Classification TaskClassification
	Content        string
	Subtasks       []string // IDs of delegated subtasks
	ParentID       string   // non-empty if this is a subtask
	AssignedTo     string   // agent name if delegated
	CreatedAt      time.Time
	StartedAt      *time.Time
	CompletedAt    *time.Time
	Error          string
}

// TaskTracker manages task lifecycle state across the pipeline.
type TaskTracker struct {
	mu    sync.RWMutex
	tasks map[string]*Task
}

// NewTaskTracker creates an empty tracker.
func NewTaskTracker() *TaskTracker {
	return &TaskTracker{
		tasks: make(map[string]*Task),
	}
}

// Create registers a new task in pending state.
func (tt *TaskTracker) Create(id, sessionID, content string) *Task {
	tt.mu.Lock()
	defer tt.mu.Unlock()
	t := &Task{
		ID:        id,
		SessionID: sessionID,
		State:     TaskPending,
		Content:   content,
		CreatedAt: time.Now(),
	}
	tt.tasks[id] = t
	return t
}

// Start transitions a task to running.
func (tt *TaskTracker) Start(id, turnID string) {
	tt.mu.Lock()
	defer tt.mu.Unlock()
	if t, ok := tt.tasks[id]; ok {
		t.State = TaskRunning
		t.TurnID = turnID
		now := time.Now()
		t.StartedAt = &now
	}
}

// Complete transitions a task to completed.
func (tt *TaskTracker) Complete(id string) {
	tt.mu.Lock()
	defer tt.mu.Unlock()
	if t, ok := tt.tasks[id]; ok {
		t.State = TaskCompleted
		now := time.Now()
		t.CompletedAt = &now
	}
}

// Fail transitions a task to failed with an error message.
func (tt *TaskTracker) Fail(id, errMsg string) {
	tt.mu.Lock()
	defer tt.mu.Unlock()
	if t, ok := tt.tasks[id]; ok {
		t.State = TaskFailed
		t.Error = errMsg
		now := time.Now()
		t.CompletedAt = &now
	}
}

// Delegate transitions a task to delegated state with agent assignment.
func (tt *TaskTracker) Delegate(id, agentName string, subtaskIDs []string) {
	tt.mu.Lock()
	defer tt.mu.Unlock()
	if t, ok := tt.tasks[id]; ok {
		t.State = TaskDelegated
		t.AssignedTo = agentName
		t.Subtasks = subtaskIDs
	}
}

// Classify sets the task classification.
func (tt *TaskTracker) Classify(id string, cls TaskClassification) {
	tt.mu.Lock()
	defer tt.mu.Unlock()
	if t, ok := tt.tasks[id]; ok {
		t.Classification = cls
	}
}

// Get returns a copy of a task by ID.
func (tt *TaskTracker) Get(id string) *Task {
	tt.mu.RLock()
	defer tt.mu.RUnlock()
	if t, ok := tt.tasks[id]; ok {
		copy := *t
		return &copy
	}
	return nil
}

// Active returns all tasks in pending or running state.
func (tt *TaskTracker) Active() []*Task {
	tt.mu.RLock()
	defer tt.mu.RUnlock()
	var result []*Task
	for _, t := range tt.tasks {
		if t.State == TaskPending || t.State == TaskRunning {
			copy := *t
			result = append(result, &copy)
		}
	}
	return result
}

// Cleanup removes completed/failed tasks older than the given age.
func (tt *TaskTracker) Cleanup(maxAge time.Duration) int {
	tt.mu.Lock()
	defer tt.mu.Unlock()
	cutoff := time.Now().Add(-maxAge)
	removed := 0
	for id, t := range tt.tasks {
		if (t.State == TaskCompleted || t.State == TaskFailed) && t.CompletedAt != nil && t.CompletedAt.Before(cutoff) {
			delete(tt.tasks, id)
			removed++
		}
	}
	return removed
}
