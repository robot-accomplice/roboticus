package agent

import (
	"fmt"
	"sync"
	"sync/atomic"
)

// OrchestrationPattern defines how subtasks are coordinated.
type OrchestrationPattern int

const (
	PatternSequential OrchestrationPattern = iota // Tasks run one after another
	PatternParallel                               // Tasks run concurrently
	PatternFanOutFanIn                            // Distribute then collect
	PatternHandoff                                // Pass context in sequence
)

// SubtaskStatus tracks the state of a subtask.
type SubtaskStatus int

const (
	SubtaskPending   SubtaskStatus = iota
	SubtaskAssigned
	SubtaskRunning
	SubtaskCompleted
	SubtaskFailed
)

// Subtask represents a decomposed piece of work.
type Subtask struct {
	ID           string
	Description  string
	Capabilities []string // required agent capabilities
	AgentID      string   // assigned agent (empty if unassigned)
	Status       SubtaskStatus
	Model        string // optional model preference
	Result       string // filled on completion
	Error        string // filled on failure
}

// WorkflowStatus tracks the overall workflow state.
type WorkflowStatus int

const (
	WorkflowCreated   WorkflowStatus = iota
	WorkflowRunning
	WorkflowCompleted
	WorkflowFailed
	WorkflowCancelled
)

// Workflow is a named container of subtasks with an orchestration pattern.
type Workflow struct {
	ID       string
	Name     string
	Pattern  OrchestrationPattern
	Status   WorkflowStatus
	Subtasks []*Subtask
}

// Orchestrator manages multi-agent task decomposition and coordination.
type Orchestrator struct {
	mu        sync.Mutex
	workflows map[string]*Workflow
	counter   atomic.Int64
}

// NewOrchestrator creates an orchestrator.
func NewOrchestrator() *Orchestrator {
	return &Orchestrator{
		workflows: make(map[string]*Workflow),
	}
}

// CreateWorkflow creates a new workflow with the given subtasks.
func (o *Orchestrator) CreateWorkflow(name string, pattern OrchestrationPattern, subtasks []*Subtask) string {
	o.mu.Lock()
	defer o.mu.Unlock()

	id := fmt.Sprintf("wf_%d", o.counter.Add(1))
	for i, st := range subtasks {
		if st.ID == "" {
			st.ID = fmt.Sprintf("%s_t%d", id, i+1)
		}
	}

	wf := &Workflow{
		ID:       id,
		Name:     name,
		Pattern:  pattern,
		Status:   WorkflowCreated,
		Subtasks: subtasks,
	}
	o.workflows[id] = wf
	return id
}

// AssignAgent marks a subtask as assigned to an agent.
func (o *Orchestrator) AssignAgent(workflowID, taskID, agentID string) error {
	o.mu.Lock()
	defer o.mu.Unlock()

	wf, ok := o.workflows[workflowID]
	if !ok {
		return fmt.Errorf("workflow %s not found", workflowID)
	}

	for _, st := range wf.Subtasks {
		if st.ID == taskID {
			st.AgentID = agentID
			st.Status = SubtaskAssigned
			return nil
		}
	}
	return fmt.Errorf("task %s not found in workflow %s", taskID, workflowID)
}

// StartTask marks a subtask as running.
func (o *Orchestrator) StartTask(workflowID, taskID string) error {
	o.mu.Lock()
	defer o.mu.Unlock()

	wf, ok := o.workflows[workflowID]
	if !ok {
		return fmt.Errorf("workflow %s not found", workflowID)
	}

	if wf.Status == WorkflowCreated {
		wf.Status = WorkflowRunning
	}

	for _, st := range wf.Subtasks {
		if st.ID == taskID {
			st.Status = SubtaskRunning
			return nil
		}
	}
	return fmt.Errorf("task %s not found", taskID)
}

// CompleteTask marks a subtask as completed with a result.
func (o *Orchestrator) CompleteTask(workflowID, taskID, result string) error {
	o.mu.Lock()
	defer o.mu.Unlock()

	wf, ok := o.workflows[workflowID]
	if !ok {
		return fmt.Errorf("workflow %s not found", workflowID)
	}

	for _, st := range wf.Subtasks {
		if st.ID == taskID {
			st.Status = SubtaskCompleted
			st.Result = result
			o.checkWorkflowComplete(wf)
			return nil
		}
	}
	return fmt.Errorf("task %s not found", taskID)
}

// FailTask marks a subtask as failed.
func (o *Orchestrator) FailTask(workflowID, taskID, errMsg string) error {
	o.mu.Lock()
	defer o.mu.Unlock()

	wf, ok := o.workflows[workflowID]
	if !ok {
		return fmt.Errorf("workflow %s not found", workflowID)
	}

	for _, st := range wf.Subtasks {
		if st.ID == taskID {
			st.Status = SubtaskFailed
			st.Error = errMsg
			o.checkWorkflowComplete(wf)
			return nil
		}
	}
	return fmt.Errorf("task %s not found", taskID)
}

// NextTasks returns subtasks that are ready to run based on the pattern.
func (o *Orchestrator) NextTasks(workflowID string) []*Subtask {
	o.mu.Lock()
	defer o.mu.Unlock()

	wf, ok := o.workflows[workflowID]
	if !ok {
		return nil
	}

	switch wf.Pattern {
	case PatternParallel, PatternFanOutFanIn:
		// All pending/assigned tasks can run.
		var ready []*Subtask
		for _, st := range wf.Subtasks {
			if st.Status == SubtaskPending || st.Status == SubtaskAssigned {
				ready = append(ready, st)
			}
		}
		return ready

	case PatternSequential, PatternHandoff:
		// Only the first non-completed task can run.
		for _, st := range wf.Subtasks {
			if st.Status == SubtaskCompleted || st.Status == SubtaskFailed {
				continue
			}
			return []*Subtask{st}
		}
		return nil
	}
	return nil
}

// MatchCapabilities finds unassigned tasks matching the given capabilities.
func (o *Orchestrator) MatchCapabilities(workflowID string, capabilities []string) []*Subtask {
	o.mu.Lock()
	defer o.mu.Unlock()

	wf, ok := o.workflows[workflowID]
	if !ok {
		return nil
	}

	capSet := make(map[string]bool)
	for _, c := range capabilities {
		capSet[c] = true
	}

	var matches []*Subtask
	for _, st := range wf.Subtasks {
		if st.Status != SubtaskPending {
			continue
		}
		allMatch := true
		for _, req := range st.Capabilities {
			if !capSet[req] {
				allMatch = false
				break
			}
		}
		if allMatch {
			matches = append(matches, st)
		}
	}
	return matches
}

// GetWorkflow returns a workflow by ID.
func (o *Orchestrator) GetWorkflow(workflowID string) *Workflow {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.workflows[workflowID]
}

// checkWorkflowComplete updates workflow status when all tasks are terminal.
func (o *Orchestrator) checkWorkflowComplete(wf *Workflow) {
	allDone := true
	anyFailed := false
	for _, st := range wf.Subtasks {
		if st.Status != SubtaskCompleted && st.Status != SubtaskFailed {
			allDone = false
			break
		}
		if st.Status == SubtaskFailed {
			anyFailed = true
		}
	}
	if allDone {
		if anyFailed {
			wf.Status = WorkflowFailed
		} else {
			wf.Status = WorkflowCompleted
		}
	}
}
