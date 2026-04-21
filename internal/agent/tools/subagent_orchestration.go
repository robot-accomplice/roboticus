package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"roboticus/internal/db"
)

// OrchestrateSubagentsTool creates a bounded, persisted subagent orchestration
// workflow on the live runtime path. It records the workflow in the delegated
// task lifecycle store so orchestrators can inspect, retry, and repackage the
// results instead of narrating invisible orchestration.
type OrchestrateSubagentsTool struct{}

func (t *OrchestrateSubagentsTool) Name() string { return "orchestrate-subagents" }
func (t *OrchestrateSubagentsTool) Description() string {
	return "Create a bounded multi-subagent workflow, assign enabled subagents, and persist orchestration evidence into the delegated task lifecycle."
}
func (t *OrchestrateSubagentsTool) Risk() RiskLevel { return RiskDangerous }
func (t *OrchestrateSubagentsTool) ParameterSchema() json.RawMessage {
	return json.RawMessage(`{
		"type":"object",
		"properties":{
			"workflow_name":{"type":"string","description":"Short workflow name"},
			"pattern":{"type":"string","description":"One of: sequential, parallel, fan_out_fan_in, handoff"},
			"subtasks":{
				"type":"array",
				"description":"Bounded list of subtasks to orchestrate",
				"items":{
					"type":"object",
					"properties":{
						"description":{"type":"string"},
						"required_skills":{"type":"array","items":{"type":"string"}},
						"preferred_subagent":{"type":"string"},
						"model":{"type":"string"}
					},
					"required":["description"]
				}
			}
		},
		"required":["subtasks"]
	}`)
}

func (t *OrchestrateSubagentsTool) Execute(ctx context.Context, params string, tctx *Context) (*Result, error) {
	if tctx.Store == nil {
		return nil, fmt.Errorf("database store not available")
	}

	isSubagent, err := db.IsSubagentName(ctx, tctx.Store, firstNonEmpty(tctx.AgentID, tctx.AgentName))
	if err != nil {
		return nil, err
	}
	if isSubagent {
		return nil, fmt.Errorf("subagents may not orchestrate other subagents directly")
	}

	var args struct {
		WorkflowName string `json:"workflow_name"`
		Pattern      string `json:"pattern"`
		Subtasks     []struct {
			Description       string   `json:"description"`
			RequiredSkills    []string `json:"required_skills"`
			PreferredSubagent string   `json:"preferred_subagent"`
			Model             string   `json:"model"`
		} `json:"subtasks"`
	}
	if err := json.Unmarshal([]byte(params), &args); err != nil {
		return nil, fmt.Errorf("invalid parameters: %w", err)
	}
	if len(args.Subtasks) == 0 {
		return nil, fmt.Errorf("subtasks are required")
	}

	spec := db.OrchestrationPlanSpec{
		WorkflowName: strings.TrimSpace(args.WorkflowName),
		Pattern:      strings.TrimSpace(args.Pattern),
		RequestedBy:  strings.TrimSpace(firstNonEmpty(tctx.AgentName, tctx.AgentID)),
		Subtasks:     make([]db.OrchestrationSubtaskSpec, 0, len(args.Subtasks)),
	}
	for _, subtask := range args.Subtasks {
		spec.Subtasks = append(spec.Subtasks, db.OrchestrationSubtaskSpec{
			Description:       strings.TrimSpace(subtask.Description),
			RequiredSkills:    subtask.RequiredSkills,
			PreferredSubagent: strings.TrimSpace(subtask.PreferredSubagent),
			Model:             strings.TrimSpace(subtask.Model),
		})
	}

	repo := db.NewSubagentOrchestrationRepository(tctx.Store)
	workflow, err := repo.CreateWorkflow(ctx, spec)
	if err != nil {
		return nil, err
	}
	data, _ := json.Marshal(map[string]any{
		"workflow": workflow,
		"created":  true,
	})
	return &Result{Output: string(data)}, nil
}
