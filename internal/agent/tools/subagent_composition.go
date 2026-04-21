package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"roboticus/internal/db"
)

var subagentNamePattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{1,63}$`)

// ComposeSubagentTool creates or updates a subagent through the authoritative
// runtime repository. It is orchestrator-only; subagents may not recursively
// compose new workers.
type ComposeSubagentTool struct{}

func (t *ComposeSubagentTool) Name() string { return "compose-subagent" }
func (t *ComposeSubagentTool) Description() string {
	return "Create or update a subagent on the live runtime path with model, role, skills, fallback models, and description."
}
func (t *ComposeSubagentTool) Risk() RiskLevel { return RiskDangerous }
func (t *ComposeSubagentTool) ParameterSchema() json.RawMessage {
	return json.RawMessage(`{
		"type":"object",
		"properties":{
			"name":{"type":"string","description":"Stable subagent name (letters, numbers, underscore, dash)"},
			"display_name":{"type":"string","description":"Human-friendly display name"},
			"model":{"type":"string","description":"Primary model for this subagent"},
			"role":{"type":"string","description":"Subagent role (default: subagent)"},
			"description":{"type":"string","description":"What this subagent is for"},
			"fixed_skills":{"type":"array","items":{"type":"string"},"description":"Pinned skills for this subagent"},
			"fallback_models":{"type":"array","items":{"type":"string"},"description":"Fallback models for this subagent"},
			"enabled":{"type":"boolean","description":"Whether this subagent should be enabled immediately"}
		},
		"required":["name","model"]
	}`)
}

func (t *ComposeSubagentTool) Execute(ctx context.Context, params string, tctx *Context) (*Result, error) {
	if tctx.Store == nil {
		return nil, fmt.Errorf("database store not available")
	}
	var args struct {
		Name           string   `json:"name"`
		DisplayName    string   `json:"display_name"`
		Model          string   `json:"model"`
		Role           string   `json:"role"`
		Description    string   `json:"description"`
		FixedSkills    []string `json:"fixed_skills"`
		FallbackModels []string `json:"fallback_models"`
		Enabled        *bool    `json:"enabled"`
	}
	if err := json.Unmarshal([]byte(params), &args); err != nil {
		return nil, fmt.Errorf("invalid parameters: %w", err)
	}
	args.Name = strings.TrimSpace(args.Name)
	args.Model = strings.TrimSpace(args.Model)
	args.DisplayName = strings.TrimSpace(args.DisplayName)
	args.Role = strings.ToLower(strings.TrimSpace(args.Role))
	args.Description = strings.TrimSpace(args.Description)

	if args.Name == "" || args.Model == "" {
		return nil, fmt.Errorf("name and model are required")
	}
	if !subagentNamePattern.MatchString(args.Name) {
		return nil, fmt.Errorf("name must match %q", subagentNamePattern.String())
	}
	if args.Role == "" {
		args.Role = "subagent"
	}
	if args.Role == "orchestrator" {
		return nil, fmt.Errorf("compose-subagent may not create orchestrator roles")
	}

	isSubagent, err := db.IsSubagentName(ctx, tctx.Store, firstNonEmpty(tctx.AgentID, tctx.AgentName))
	if err != nil {
		return nil, err
	}
	if isSubagent {
		return nil, fmt.Errorf("subagents may not compose subagents directly")
	}

	enabled := true
	if args.Enabled != nil {
		enabled = *args.Enabled
	}

	repo := db.NewSubagentCompositionRepository(tctx.Store)
	created, spec, err := repo.Upsert(ctx, db.SubagentSpec{
		Name:           args.Name,
		DisplayName:    args.DisplayName,
		Model:          args.Model,
		Role:           args.Role,
		Description:    args.Description,
		FixedSkills:    args.FixedSkills,
		FallbackModels: args.FallbackModels,
		Enabled:        enabled,
	})
	if err != nil {
		return nil, err
	}

	data, _ := json.Marshal(map[string]any{
		"created":  created,
		"subagent": spec,
	})
	return &Result{Output: string(data)}, nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
