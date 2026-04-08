package orchestration

import (
	"context"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Orchestrator: creation
// ---------------------------------------------------------------------------

func TestNewOrchestrator(t *testing.T) {
	o := NewOrchestrator()
	if o == nil {
		t.Fatal("NewOrchestrator returned nil")
	}
	if o.workflows == nil {
		t.Fatal("workflows map not initialised")
	}
}

// ---------------------------------------------------------------------------
// Orchestrator: workflow creation
// ---------------------------------------------------------------------------

func TestCreateWorkflow_AssignsIDs(t *testing.T) {
	o := NewOrchestrator()
	tasks := []*Subtask{
		{Description: "first"},
		{Description: "second"},
	}
	wfID := o.CreateWorkflow("test-wf", PatternSequential, tasks)

	if wfID == "" {
		t.Fatal("expected non-empty workflow ID")
	}
	wf := o.GetWorkflow(wfID)
	if wf == nil {
		t.Fatal("GetWorkflow returned nil")
	}
	if wf.Name != "test-wf" {
		t.Errorf("name = %q, want %q", wf.Name, "test-wf")
	}
	if wf.Status != WorkflowCreated {
		t.Errorf("status = %d, want WorkflowCreated", wf.Status)
	}
	// Auto-generated subtask IDs
	for i, st := range wf.Subtasks {
		if st.ID == "" {
			t.Errorf("subtask %d has empty ID", i)
		}
	}
}

func TestCreateWorkflow_PreservesExistingSubtaskIDs(t *testing.T) {
	o := NewOrchestrator()
	tasks := []*Subtask{
		{ID: "custom-id", Description: "explicit"},
		{Description: "auto-id"},
	}
	wfID := o.CreateWorkflow("wf", PatternParallel, tasks)
	wf := o.GetWorkflow(wfID)

	if wf.Subtasks[0].ID != "custom-id" {
		t.Errorf("explicit ID overwritten: got %q", wf.Subtasks[0].ID)
	}
	if wf.Subtasks[1].ID == "" {
		t.Error("auto-generated ID should not be empty")
	}
}

func TestCreateWorkflow_UniqueIDs(t *testing.T) {
	o := NewOrchestrator()
	id1 := o.CreateWorkflow("a", PatternSequential, nil)
	id2 := o.CreateWorkflow("b", PatternSequential, nil)
	if id1 == id2 {
		t.Errorf("workflow IDs should be unique: both %q", id1)
	}
}

// ---------------------------------------------------------------------------
// Orchestrator: GetWorkflow
// ---------------------------------------------------------------------------

func TestGetWorkflow_NotFound(t *testing.T) {
	o := NewOrchestrator()
	if wf := o.GetWorkflow("nonexistent"); wf != nil {
		t.Error("expected nil for unknown workflow")
	}
}

// ---------------------------------------------------------------------------
// Orchestrator: AssignAgent
// ---------------------------------------------------------------------------

func TestAssignAgent_Success(t *testing.T) {
	o := NewOrchestrator()
	wfID := o.CreateWorkflow("wf", PatternSequential, []*Subtask{{Description: "t"}})
	wf := o.GetWorkflow(wfID)
	taskID := wf.Subtasks[0].ID

	if err := o.AssignAgent(wfID, taskID, "agent-1"); err != nil {
		t.Fatalf("AssignAgent: %v", err)
	}
	wf = o.GetWorkflow(wfID)
	if wf.Subtasks[0].AgentID != "agent-1" {
		t.Errorf("AgentID = %q, want %q", wf.Subtasks[0].AgentID, "agent-1")
	}
	if wf.Subtasks[0].Status != SubtaskAssigned {
		t.Errorf("status = %d, want SubtaskAssigned", wf.Subtasks[0].Status)
	}
}

func TestAssignAgent_WorkflowNotFound(t *testing.T) {
	o := NewOrchestrator()
	err := o.AssignAgent("bad-wf", "t1", "a1")
	if err == nil {
		t.Error("expected error for missing workflow")
	}
}

func TestAssignAgent_TaskNotFound(t *testing.T) {
	o := NewOrchestrator()
	wfID := o.CreateWorkflow("wf", PatternSequential, []*Subtask{{Description: "t"}})
	err := o.AssignAgent(wfID, "no-such-task", "a1")
	if err == nil {
		t.Error("expected error for missing task")
	}
}

// ---------------------------------------------------------------------------
// Orchestrator: StartTask
// ---------------------------------------------------------------------------

func TestStartTask_Success(t *testing.T) {
	o := NewOrchestrator()
	wfID := o.CreateWorkflow("wf", PatternSequential, []*Subtask{{Description: "t"}})
	wf := o.GetWorkflow(wfID)
	taskID := wf.Subtasks[0].ID

	if err := o.StartTask(wfID, taskID); err != nil {
		t.Fatalf("StartTask: %v", err)
	}

	wf = o.GetWorkflow(wfID)
	if wf.Status != WorkflowRunning {
		t.Errorf("workflow status = %d, want WorkflowRunning", wf.Status)
	}
	if wf.Subtasks[0].Status != SubtaskRunning {
		t.Errorf("subtask status = %d, want SubtaskRunning", wf.Subtasks[0].Status)
	}
}

func TestStartTask_AlreadyRunningWorkflow(t *testing.T) {
	o := NewOrchestrator()
	wfID := o.CreateWorkflow("wf", PatternParallel, []*Subtask{
		{Description: "a"},
		{Description: "b"},
	})
	wf := o.GetWorkflow(wfID)

	// Start first task -- transitions workflow to Running
	_ = o.StartTask(wfID, wf.Subtasks[0].ID)
	// Start second task -- workflow already Running, should remain Running
	_ = o.StartTask(wfID, wf.Subtasks[1].ID)

	wf = o.GetWorkflow(wfID)
	if wf.Status != WorkflowRunning {
		t.Errorf("workflow status = %d, want WorkflowRunning", wf.Status)
	}
}

func TestStartTask_WorkflowNotFound(t *testing.T) {
	o := NewOrchestrator()
	if err := o.StartTask("bad", "t1"); err == nil {
		t.Error("expected error for missing workflow")
	}
}

func TestStartTask_TaskNotFound(t *testing.T) {
	o := NewOrchestrator()
	wfID := o.CreateWorkflow("wf", PatternSequential, []*Subtask{{Description: "t"}})
	if err := o.StartTask(wfID, "missing"); err == nil {
		t.Error("expected error for missing task")
	}
}

// ---------------------------------------------------------------------------
// Orchestrator: CompleteTask
// ---------------------------------------------------------------------------

func TestCompleteTask_Success(t *testing.T) {
	o := NewOrchestrator()
	wfID := o.CreateWorkflow("wf", PatternSequential, []*Subtask{{Description: "t"}})
	wf := o.GetWorkflow(wfID)
	taskID := wf.Subtasks[0].ID

	if err := o.CompleteTask(wfID, taskID, "done"); err != nil {
		t.Fatalf("CompleteTask: %v", err)
	}

	wf = o.GetWorkflow(wfID)
	if wf.Subtasks[0].Status != SubtaskCompleted {
		t.Errorf("subtask status = %d, want SubtaskCompleted", wf.Subtasks[0].Status)
	}
	if wf.Subtasks[0].Result != "done" {
		t.Errorf("result = %q, want %q", wf.Subtasks[0].Result, "done")
	}
	// Single-task workflow should now be completed
	if wf.Status != WorkflowCompleted {
		t.Errorf("workflow status = %d, want WorkflowCompleted", wf.Status)
	}
}

func TestCompleteTask_WorkflowNotFound(t *testing.T) {
	o := NewOrchestrator()
	if err := o.CompleteTask("bad", "t1", "x"); err == nil {
		t.Error("expected error for missing workflow")
	}
}

func TestCompleteTask_TaskNotFound(t *testing.T) {
	o := NewOrchestrator()
	wfID := o.CreateWorkflow("wf", PatternSequential, []*Subtask{{Description: "t"}})
	if err := o.CompleteTask(wfID, "missing", "x"); err == nil {
		t.Error("expected error for missing task")
	}
}

// ---------------------------------------------------------------------------
// Orchestrator: FailTask
// ---------------------------------------------------------------------------

func TestFailTask_Success(t *testing.T) {
	o := NewOrchestrator()
	wfID := o.CreateWorkflow("wf", PatternSequential, []*Subtask{{Description: "t"}})
	wf := o.GetWorkflow(wfID)
	taskID := wf.Subtasks[0].ID

	if err := o.FailTask(wfID, taskID, "boom"); err != nil {
		t.Fatalf("FailTask: %v", err)
	}

	wf = o.GetWorkflow(wfID)
	if wf.Subtasks[0].Status != SubtaskFailed {
		t.Errorf("subtask status = %d, want SubtaskFailed", wf.Subtasks[0].Status)
	}
	if wf.Subtasks[0].Error != "boom" {
		t.Errorf("error = %q, want %q", wf.Subtasks[0].Error, "boom")
	}
	// Single-task workflow with failure -> WorkflowFailed
	if wf.Status != WorkflowFailed {
		t.Errorf("workflow status = %d, want WorkflowFailed", wf.Status)
	}
}

func TestFailTask_WorkflowNotFound(t *testing.T) {
	o := NewOrchestrator()
	if err := o.FailTask("bad", "t1", "err"); err == nil {
		t.Error("expected error for missing workflow")
	}
}

func TestFailTask_TaskNotFound(t *testing.T) {
	o := NewOrchestrator()
	wfID := o.CreateWorkflow("wf", PatternSequential, []*Subtask{{Description: "t"}})
	if err := o.FailTask(wfID, "missing", "err"); err == nil {
		t.Error("expected error for missing task")
	}
}

// ---------------------------------------------------------------------------
// Orchestrator: checkWorkflowComplete (via CompleteTask/FailTask)
// ---------------------------------------------------------------------------

func TestCheckWorkflowComplete_AllComplete(t *testing.T) {
	o := NewOrchestrator()
	wfID := o.CreateWorkflow("wf", PatternParallel, []*Subtask{
		{Description: "a"},
		{Description: "b"},
	})
	wf := o.GetWorkflow(wfID)
	_ = o.CompleteTask(wfID, wf.Subtasks[0].ID, "r1")
	_ = o.CompleteTask(wfID, wf.Subtasks[1].ID, "r2")

	wf = o.GetWorkflow(wfID)
	if wf.Status != WorkflowCompleted {
		t.Errorf("status = %d, want WorkflowCompleted", wf.Status)
	}
}

func TestCheckWorkflowComplete_MixedCompleteFail(t *testing.T) {
	o := NewOrchestrator()
	wfID := o.CreateWorkflow("wf", PatternParallel, []*Subtask{
		{Description: "a"},
		{Description: "b"},
	})
	wf := o.GetWorkflow(wfID)
	_ = o.CompleteTask(wfID, wf.Subtasks[0].ID, "ok")
	_ = o.FailTask(wfID, wf.Subtasks[1].ID, "bad")

	wf = o.GetWorkflow(wfID)
	if wf.Status != WorkflowFailed {
		t.Errorf("status = %d, want WorkflowFailed (partial failure)", wf.Status)
	}
}

func TestCheckWorkflowComplete_NotAllDone(t *testing.T) {
	o := NewOrchestrator()
	wfID := o.CreateWorkflow("wf", PatternParallel, []*Subtask{
		{Description: "a"},
		{Description: "b"},
	})
	wf := o.GetWorkflow(wfID)
	_ = o.CompleteTask(wfID, wf.Subtasks[0].ID, "ok")
	// Second subtask still pending

	wf = o.GetWorkflow(wfID)
	if wf.Status == WorkflowCompleted || wf.Status == WorkflowFailed {
		t.Errorf("status = %d; should not be terminal with pending tasks", wf.Status)
	}
}

// ---------------------------------------------------------------------------
// Orchestrator: NextTasks
// ---------------------------------------------------------------------------

func TestNextTasks_Parallel(t *testing.T) {
	o := NewOrchestrator()
	wfID := o.CreateWorkflow("wf", PatternParallel, []*Subtask{
		{Description: "a"},
		{Description: "b"},
		{Description: "c"},
	})

	ready := o.NextTasks(wfID)
	if len(ready) != 3 {
		t.Errorf("parallel: got %d ready tasks, want 3", len(ready))
	}
}

func TestNextTasks_FanOutFanIn(t *testing.T) {
	o := NewOrchestrator()
	wfID := o.CreateWorkflow("wf", PatternFanOutFanIn, []*Subtask{
		{Description: "a"},
		{Description: "b"},
	})

	ready := o.NextTasks(wfID)
	if len(ready) != 2 {
		t.Errorf("fan-out-fan-in: got %d ready tasks, want 2", len(ready))
	}
}

func TestNextTasks_Sequential(t *testing.T) {
	o := NewOrchestrator()
	wfID := o.CreateWorkflow("wf", PatternSequential, []*Subtask{
		{Description: "first"},
		{Description: "second"},
	})

	ready := o.NextTasks(wfID)
	if len(ready) != 1 {
		t.Fatalf("sequential: got %d ready tasks, want 1", len(ready))
	}
	if ready[0].Description != "first" {
		t.Errorf("first ready task = %q, want %q", ready[0].Description, "first")
	}
}

func TestNextTasks_Handoff(t *testing.T) {
	o := NewOrchestrator()
	wfID := o.CreateWorkflow("wf", PatternHandoff, []*Subtask{
		{Description: "first"},
		{Description: "second"},
	})

	ready := o.NextTasks(wfID)
	if len(ready) != 1 {
		t.Fatalf("handoff: got %d ready tasks, want 1", len(ready))
	}
}

func TestNextTasks_SequentialAfterCompletion(t *testing.T) {
	o := NewOrchestrator()
	wfID := o.CreateWorkflow("wf", PatternSequential, []*Subtask{
		{Description: "first"},
		{Description: "second"},
	})
	wf := o.GetWorkflow(wfID)
	_ = o.CompleteTask(wfID, wf.Subtasks[0].ID, "done")

	ready := o.NextTasks(wfID)
	if len(ready) != 1 {
		t.Fatalf("sequential after complete: got %d, want 1", len(ready))
	}
	if ready[0].Description != "second" {
		t.Errorf("next task = %q, want %q", ready[0].Description, "second")
	}
}

func TestNextTasks_SequentialSkipsFailedToo(t *testing.T) {
	o := NewOrchestrator()
	wfID := o.CreateWorkflow("wf", PatternSequential, []*Subtask{
		{Description: "first"},
		{Description: "second"},
		{Description: "third"},
	})
	wf := o.GetWorkflow(wfID)
	_ = o.FailTask(wfID, wf.Subtasks[0].ID, "err")

	ready := o.NextTasks(wfID)
	if len(ready) != 1 {
		t.Fatalf("got %d, want 1", len(ready))
	}
	if ready[0].Description != "second" {
		t.Errorf("next = %q, want %q", ready[0].Description, "second")
	}
}

func TestNextTasks_SequentialAllDone(t *testing.T) {
	o := NewOrchestrator()
	wfID := o.CreateWorkflow("wf", PatternSequential, []*Subtask{
		{Description: "only"},
	})
	wf := o.GetWorkflow(wfID)
	_ = o.CompleteTask(wfID, wf.Subtasks[0].ID, "done")

	ready := o.NextTasks(wfID)
	if len(ready) != 0 {
		t.Errorf("all done: got %d ready tasks, want 0", len(ready))
	}
}

func TestNextTasks_ParallelExcludesCompleted(t *testing.T) {
	o := NewOrchestrator()
	wfID := o.CreateWorkflow("wf", PatternParallel, []*Subtask{
		{Description: "a"},
		{Description: "b"},
	})
	wf := o.GetWorkflow(wfID)
	_ = o.CompleteTask(wfID, wf.Subtasks[0].ID, "done")

	ready := o.NextTasks(wfID)
	if len(ready) != 1 {
		t.Errorf("parallel after one complete: got %d, want 1", len(ready))
	}
}

func TestNextTasks_WorkflowNotFound(t *testing.T) {
	o := NewOrchestrator()
	if tasks := o.NextTasks("bad"); tasks != nil {
		t.Error("expected nil for missing workflow")
	}
}

// ---------------------------------------------------------------------------
// Orchestrator: MatchCapabilities
// ---------------------------------------------------------------------------

func TestMatchCapabilities_BasicMatch(t *testing.T) {
	o := NewOrchestrator()
	wfID := o.CreateWorkflow("wf", PatternParallel, []*Subtask{
		{Description: "code", Capabilities: []string{"go", "testing"}},
		{Description: "review", Capabilities: []string{"go"}},
		{Description: "deploy", Capabilities: []string{"docker"}},
	})

	matches := o.MatchCapabilities(wfID, []string{"go", "testing"})
	if len(matches) != 2 {
		t.Errorf("got %d matches, want 2", len(matches))
	}
}

func TestMatchCapabilities_NoMatch(t *testing.T) {
	o := NewOrchestrator()
	wfID := o.CreateWorkflow("wf", PatternParallel, []*Subtask{
		{Description: "deploy", Capabilities: []string{"docker"}},
	})

	matches := o.MatchCapabilities(wfID, []string{"python"})
	if len(matches) != 0 {
		t.Errorf("got %d matches, want 0", len(matches))
	}
}

func TestMatchCapabilities_SkipsNonPending(t *testing.T) {
	o := NewOrchestrator()
	wfID := o.CreateWorkflow("wf", PatternParallel, []*Subtask{
		{Description: "a", Capabilities: []string{"go"}},
		{Description: "b", Capabilities: []string{"go"}},
	})
	wf := o.GetWorkflow(wfID)
	_ = o.AssignAgent(wfID, wf.Subtasks[0].ID, "agent-1") // moves to Assigned

	matches := o.MatchCapabilities(wfID, []string{"go"})
	if len(matches) != 1 {
		t.Errorf("got %d matches, want 1 (skip assigned)", len(matches))
	}
}

func TestMatchCapabilities_EmptyCapabilities(t *testing.T) {
	o := NewOrchestrator()
	wfID := o.CreateWorkflow("wf", PatternParallel, []*Subtask{
		{Description: "generic"}, // no required capabilities
	})

	matches := o.MatchCapabilities(wfID, []string{"anything"})
	if len(matches) != 1 {
		t.Errorf("got %d matches, want 1 (task with no requirements)", len(matches))
	}
}

func TestMatchCapabilities_WorkflowNotFound(t *testing.T) {
	o := NewOrchestrator()
	if m := o.MatchCapabilities("bad", []string{"go"}); m != nil {
		t.Error("expected nil for missing workflow")
	}
}

// ---------------------------------------------------------------------------
// Orchestrator: full workflow lifecycle
// ---------------------------------------------------------------------------

func TestWorkflowLifecycle_SequentialEnd2End(t *testing.T) {
	o := NewOrchestrator()
	wfID := o.CreateWorkflow("e2e", PatternSequential, []*Subtask{
		{Description: "step1", Capabilities: []string{"go"}},
		{Description: "step2", Capabilities: []string{"go"}},
	})

	// Step 1
	ready := o.NextTasks(wfID)
	if len(ready) != 1 || ready[0].Description != "step1" {
		t.Fatalf("unexpected first ready task: %+v", ready)
	}
	_ = o.AssignAgent(wfID, ready[0].ID, "a1")
	_ = o.StartTask(wfID, ready[0].ID)
	_ = o.CompleteTask(wfID, ready[0].ID, "result1")

	// Step 2
	ready = o.NextTasks(wfID)
	if len(ready) != 1 || ready[0].Description != "step2" {
		t.Fatalf("unexpected second ready task: %+v", ready)
	}
	_ = o.StartTask(wfID, ready[0].ID)
	_ = o.CompleteTask(wfID, ready[0].ID, "result2")

	wf := o.GetWorkflow(wfID)
	if wf.Status != WorkflowCompleted {
		t.Errorf("final status = %d, want WorkflowCompleted", wf.Status)
	}
}

func TestWorkflowLifecycle_ParallelEnd2End(t *testing.T) {
	o := NewOrchestrator()
	wfID := o.CreateWorkflow("par", PatternParallel, []*Subtask{
		{Description: "a"},
		{Description: "b"},
		{Description: "c"},
	})

	wf := o.GetWorkflow(wfID)
	for _, st := range wf.Subtasks {
		_ = o.StartTask(wfID, st.ID)
	}
	for _, st := range wf.Subtasks {
		_ = o.CompleteTask(wfID, st.ID, "ok")
	}

	wf = o.GetWorkflow(wfID)
	if wf.Status != WorkflowCompleted {
		t.Errorf("status = %d, want WorkflowCompleted", wf.Status)
	}
}

// ---------------------------------------------------------------------------
// Orchestrator: timestamps (#48)
// ---------------------------------------------------------------------------

func TestWorkflow_CreatedAtSet(t *testing.T) {
	o := NewOrchestrator()
	before := time.Now()
	wfID := o.CreateWorkflow("ts", PatternSequential, []*Subtask{{Description: "t"}})
	after := time.Now()

	wf := o.GetWorkflow(wfID)
	if wf.CreatedAt.Before(before) || wf.CreatedAt.After(after) {
		t.Errorf("workflow CreatedAt %v not in [%v, %v]", wf.CreatedAt, before, after)
	}
	if wf.CompletedAt != nil {
		t.Error("workflow CompletedAt should be nil before completion")
	}
}

func TestSubtask_CreatedAtSet(t *testing.T) {
	o := NewOrchestrator()
	before := time.Now()
	wfID := o.CreateWorkflow("ts", PatternSequential, []*Subtask{{Description: "t"}})
	after := time.Now()

	wf := o.GetWorkflow(wfID)
	st := wf.Subtasks[0]
	if st.CreatedAt.Before(before) || st.CreatedAt.After(after) {
		t.Errorf("subtask CreatedAt %v not in [%v, %v]", st.CreatedAt, before, after)
	}
	if st.CompletedAt != nil {
		t.Error("subtask CompletedAt should be nil before completion")
	}
}

func TestSubtask_CompletedAtSetOnComplete(t *testing.T) {
	o := NewOrchestrator()
	wfID := o.CreateWorkflow("ts", PatternSequential, []*Subtask{{Description: "t"}})
	wf := o.GetWorkflow(wfID)

	before := time.Now()
	_ = o.CompleteTask(wfID, wf.Subtasks[0].ID, "done")
	after := time.Now()

	wf = o.GetWorkflow(wfID)
	st := wf.Subtasks[0]
	if st.CompletedAt == nil {
		t.Fatal("subtask CompletedAt should be set after completion")
	}
	if st.CompletedAt.Before(before) || st.CompletedAt.After(after) {
		t.Errorf("subtask CompletedAt %v not in [%v, %v]", *st.CompletedAt, before, after)
	}
}

func TestSubtask_CompletedAtSetOnFail(t *testing.T) {
	o := NewOrchestrator()
	wfID := o.CreateWorkflow("ts", PatternSequential, []*Subtask{{Description: "t"}})
	wf := o.GetWorkflow(wfID)

	_ = o.FailTask(wfID, wf.Subtasks[0].ID, "err")

	wf = o.GetWorkflow(wfID)
	if wf.Subtasks[0].CompletedAt == nil {
		t.Error("subtask CompletedAt should be set after failure")
	}
}

func TestWorkflow_CompletedAtSetOnAllDone(t *testing.T) {
	o := NewOrchestrator()
	wfID := o.CreateWorkflow("ts", PatternSequential, []*Subtask{{Description: "t"}})
	wf := o.GetWorkflow(wfID)

	before := time.Now()
	_ = o.CompleteTask(wfID, wf.Subtasks[0].ID, "done")
	after := time.Now()

	wf = o.GetWorkflow(wfID)
	if wf.CompletedAt == nil {
		t.Fatal("workflow CompletedAt should be set when all done")
	}
	if wf.CompletedAt.Before(before) || wf.CompletedAt.After(after) {
		t.Errorf("workflow CompletedAt %v not in [%v, %v]", *wf.CompletedAt, before, after)
	}
}

// ---------------------------------------------------------------------------
// SubagentManager: additional coverage for gaps
// ---------------------------------------------------------------------------

func TestAgentStatus_String(t *testing.T) {
	tests := []struct {
		status AgentStatus
		want   string
	}{
		{AgentRegistered, "registered"},
		{AgentRunning, "running"},
		{AgentStopped, "stopped"},
		{AgentError, "error"},
		{AgentStatus(99), "unknown"},
	}
	for _, tt := range tests {
		if got := tt.status.String(); got != tt.want {
			t.Errorf("AgentStatus(%d).String() = %q, want %q", tt.status, got, tt.want)
		}
	}
}

func TestSubagentManager_MaxConcurrent(t *testing.T) {
	mgr := NewSubagentManager(5, nil)
	if got := mgr.MaxConcurrent(); got != 5 {
		t.Errorf("MaxConcurrent = %d, want 5", got)
	}
}

func TestSubagentManager_AvailableSlots(t *testing.T) {
	mgr := NewSubagentManager(3, nil)
	if got := mgr.AvailableSlots(); got != 3 {
		t.Errorf("AvailableSlots = %d, want 3", got)
	}

	_ = mgr.Register("a", AgentInstanceConfig{Name: "a"})
	_ = mgr.Start("a")

	if got := mgr.AvailableSlots(); got != 2 {
		t.Errorf("after start: AvailableSlots = %d, want 2", got)
	}
}

func TestSubagentManager_GetAgent_NotFound(t *testing.T) {
	mgr := NewSubagentManager(4, nil)
	_, ok := mgr.GetAgent("nonexistent")
	if ok {
		t.Error("expected ok=false for missing agent")
	}
}

func TestSubagentManager_Start_NotFound(t *testing.T) {
	mgr := NewSubagentManager(4, nil)
	if err := mgr.Start("ghost"); err == nil {
		t.Error("expected error starting unregistered agent")
	}
}

func TestSubagentManager_Stop_NotFound(t *testing.T) {
	mgr := NewSubagentManager(4, nil)
	if err := mgr.Stop("ghost"); err == nil {
		t.Error("expected error stopping unregistered agent")
	}
}

func TestSubagentManager_Stop_NonRunning(t *testing.T) {
	mgr := NewSubagentManager(4, nil)
	_ = mgr.Register("a", AgentInstanceConfig{Name: "a"})
	// Stop without starting -- should still succeed (transitions to Stopped)
	if err := mgr.Stop("a"); err != nil {
		t.Fatalf("Stop non-running agent: %v", err)
	}
	inst, _ := mgr.GetAgent("a")
	if inst.Status != AgentStopped {
		t.Errorf("status = %v, want Stopped", inst.Status)
	}
}

func TestSubagentManager_MarkError_NonRunning(t *testing.T) {
	mgr := NewSubagentManager(4, nil)
	_ = mgr.Register("a", AgentInstanceConfig{Name: "a"})
	// MarkError on a Registered (non-running) agent -- should not panic
	mgr.MarkError("a", "test error")
	inst, _ := mgr.GetAgent("a")
	if inst.Status != AgentError {
		t.Errorf("status = %v, want Error", inst.Status)
	}
}

func TestSubagentManager_MarkError_UnknownAgent(t *testing.T) {
	mgr := NewSubagentManager(4, nil)
	// Should not panic
	mgr.MarkError("ghost", "no-op")
}

func TestSubagentManager_UnregisterRunningAgent(t *testing.T) {
	mgr := NewSubagentManager(4, nil)
	_ = mgr.Register("a", AgentInstanceConfig{Name: "a"})
	_ = mgr.Start("a")

	// Unregistering a running agent should release the slot
	if !mgr.Unregister("a") {
		t.Error("expected Unregister to return true")
	}
	if mgr.AvailableSlots() != 4 {
		t.Errorf("AvailableSlots = %d, want 4 after unregister", mgr.AvailableSlots())
	}
}

func TestSubagentManager_StartWithContext_Cancelled(t *testing.T) {
	mgr := NewSubagentManager(1, nil)
	_ = mgr.Register("a", AgentInstanceConfig{Name: "a"})
	_ = mgr.Start("a") // fills the single slot

	_ = mgr.Register("b", AgentInstanceConfig{Name: "b"})
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	err := mgr.StartWithContext(ctx, "b")
	if err == nil {
		t.Error("expected error for cancelled context")
	}
}

func TestSubagentManager_StartWithContext_NotFound(t *testing.T) {
	mgr := NewSubagentManager(4, nil)
	err := mgr.StartWithContext(context.Background(), "ghost")
	if err == nil {
		t.Error("expected error for unregistered agent")
	}
}

func TestSubagentManager_SlotReleasedOnStop(t *testing.T) {
	mgr := NewSubagentManager(1, nil)
	_ = mgr.Register("a", AgentInstanceConfig{Name: "a"})
	_ = mgr.Start("a")

	if mgr.AvailableSlots() != 0 {
		t.Fatalf("expected 0 available slots")
	}

	_ = mgr.Stop("a")
	if mgr.AvailableSlots() != 1 {
		t.Errorf("AvailableSlots after stop = %d, want 1", mgr.AvailableSlots())
	}

	// Now a second agent should be able to start
	_ = mgr.Register("b", AgentInstanceConfig{Name: "b"})
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if err := mgr.StartWithContext(ctx, "b"); err != nil {
		t.Fatalf("Start after slot freed: %v", err)
	}
}
