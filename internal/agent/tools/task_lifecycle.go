package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"roboticus/internal/db"
)

// TaskStatusTool returns the canonical delegated task lifecycle artifact for a
// specific task. Orchestrators use this to inspect delegated work before
// deciding whether to retry, wait, or repackage results for the operator.
type TaskStatusTool struct{}

func (t *TaskStatusTool) Name() string { return "task-status" }
func (t *TaskStatusTool) Description() string {
	return "Get canonical status, recent events, and delegation outcomes for a delegated task."
}
func (t *TaskStatusTool) Risk() RiskLevel { return RiskSafe }
func (t *TaskStatusTool) ParameterSchema() json.RawMessage {
	return json.RawMessage(`{
		"type":"object",
		"properties":{
			"task_id":{"type":"string","description":"Delegated task ID to inspect"},
			"event_limit":{"type":"integer","description":"Max recent events to include (default: 20)"}
		},
		"required":["task_id"]
	}`)
}

func (t *TaskStatusTool) Execute(ctx context.Context, params string, tctx *Context) (*Result, error) {
	if tctx.Store == nil {
		return nil, fmt.Errorf("database store not available")
	}
	var args struct {
		TaskID     string `json:"task_id"`
		EventLimit int    `json:"event_limit"`
	}
	if err := json.Unmarshal([]byte(params), &args); err != nil {
		return nil, fmt.Errorf("invalid parameters: %w", err)
	}
	args.TaskID = strings.TrimSpace(args.TaskID)
	if args.TaskID == "" {
		return nil, fmt.Errorf("task_id is required")
	}

	repo := db.NewDelegatedTaskLifecycleRepository(tctx.Store)
	status, err := repo.GetStatus(ctx, args.TaskID, args.EventLimit)
	if err != nil {
		return nil, err
	}
	if status == nil {
		data, _ := json.Marshal(map[string]any{
			"found":   false,
			"task_id": args.TaskID,
		})
		return &Result{Output: string(data)}, nil
	}

	data, _ := json.Marshal(map[string]any{
		"found": true,
		"task":  status,
	})
	return &Result{Output: string(data)}, nil
}

// ListOpenTasksTool returns open delegated work using the shared delegated
// task lifecycle repository rather than status-sidecar SQL.
type ListOpenTasksTool struct{}

func (t *ListOpenTasksTool) Name() string { return "list-open-tasks" }
func (t *ListOpenTasksTool) Description() string {
	return "List open delegated tasks with latest lifecycle state, assignee, and event counts."
}
func (t *ListOpenTasksTool) Risk() RiskLevel { return RiskSafe }
func (t *ListOpenTasksTool) ParameterSchema() json.RawMessage {
	return json.RawMessage(`{
		"type":"object",
		"properties":{
			"limit":{"type":"integer","description":"Maximum open tasks to return (default: 50)"}
		}
	}`)
}

func (t *ListOpenTasksTool) Execute(ctx context.Context, params string, tctx *Context) (*Result, error) {
	if tctx.Store == nil {
		return nil, fmt.Errorf("database store not available")
	}
	var args struct {
		Limit int `json:"limit"`
	}
	_ = json.Unmarshal([]byte(params), &args)

	repo := db.NewDelegatedTaskLifecycleRepository(tctx.Store)
	tasks, err := repo.ListOpen(ctx, args.Limit)
	if err != nil {
		return nil, err
	}

	data, _ := json.Marshal(map[string]any{
		"tasks": tasks,
		"count": len(tasks),
	})
	return &Result{Output: string(data)}, nil
}

// RetryTaskTool reopens delegated work through the shared lifecycle repository.
// It is an orchestrator control-plane tool, not a direct operator-facing
// reporting path.
type RetryTaskTool struct{}

func (t *RetryTaskTool) Name() string { return "retry-task" }
func (t *RetryTaskTool) Description() string {
	return "Request a retry of a delegated task by resetting it to pending and recording a retry lifecycle event."
}
func (t *RetryTaskTool) Risk() RiskLevel { return RiskDangerous }
func (t *RetryTaskTool) ParameterSchema() json.RawMessage {
	return json.RawMessage(`{
		"type":"object",
		"properties":{
			"task_id":{"type":"string","description":"Delegated task ID to retry"},
			"reason":{"type":"string","description":"Why the task should be retried"}
		},
		"required":["task_id"]
	}`)
}

func (t *RetryTaskTool) Execute(ctx context.Context, params string, tctx *Context) (*Result, error) {
	if tctx.Store == nil {
		return nil, fmt.Errorf("database store not available")
	}
	var args struct {
		TaskID string `json:"task_id"`
		Reason string `json:"reason"`
	}
	if err := json.Unmarshal([]byte(params), &args); err != nil {
		return nil, fmt.Errorf("invalid parameters: %w", err)
	}
	args.TaskID = strings.TrimSpace(args.TaskID)
	if args.TaskID == "" {
		return nil, fmt.Errorf("task_id is required")
	}

	repo := db.NewDelegatedTaskLifecycleRepository(tctx.Store)
	result, err := repo.Retry(ctx, args.TaskID, args.Reason, tctx.AgentName)
	if err != nil {
		return nil, err
	}
	if result == nil {
		data, _ := json.Marshal(map[string]any{
			"found":   false,
			"task_id": args.TaskID,
		})
		return &Result{Output: string(data)}, nil
	}

	data, _ := json.Marshal(map[string]any{
		"found":        true,
		"updated":      result.Updated,
		"prior_status": result.PriorStatus,
		"task":         result.Task,
	})
	return &Result{Output: string(data)}, nil
}
