package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"roboticus/internal/db"
)

// ComposeSkillTool creates or updates a runtime skill through the authoritative
// skill composition repository. It is orchestrator-only; subagents may inspect
// skills but may not create or modify them directly.
type ComposeSkillTool struct {
	skillsDir string
}

func NewComposeSkillTool(skillsDir string) *ComposeSkillTool {
	return &ComposeSkillTool{skillsDir: strings.TrimSpace(skillsDir)}
}

func (t *ComposeSkillTool) Name() string { return "compose-skill" }
func (t *ComposeSkillTool) Description() string {
	return "Create or update a runtime skill by writing the durable skill artifact and authoritative skills row through one shared repository."
}
func (t *ComposeSkillTool) Risk() RiskLevel { return RiskDangerous }
func (t *ComposeSkillTool) ParameterSchema() json.RawMessage {
	return json.RawMessage(`{
		"type":"object",
		"properties":{
			"name":{"type":"string","description":"Stable skill name (letters, numbers, underscore, dash)"},
			"kind":{"type":"string","enum":["instruction","structured"],"description":"Skill representation kind (default: instruction)"},
			"description":{"type":"string","description":"What this skill is for"},
			"content":{"type":"string","description":"Instruction body for instruction skills"},
			"triggers":{"type":"array","items":{"type":"string"},"description":"Keyword triggers for auto-activation"},
			"priority":{"type":"integer","description":"Higher priority skills win trigger conflicts"},
			"tool_chain":{"type":"array","description":"Structured-skill tool chain","items":{"type":"object","properties":{"tool_name":{"type":"string"},"params":{"type":"object"}},"required":["tool_name"]}},
			"policy_overrides":{"type":"object","description":"Optional policy override payload stored with the skill row"},
			"script_path":{"type":"string","description":"Optional script path stored with the skill row"},
			"risk_level":{"type":"string","enum":["Safe","Caution","Dangerous","Forbidden"],"description":"Operator-facing risk classification"},
			"version":{"type":"string","description":"Skill version label"},
			"author":{"type":"string","description":"Skill author label"},
			"registry_source":{"type":"string","description":"Where this skill came from (default: runtime)"},
			"enabled":{"type":"boolean","description":"Whether the skill should be enabled immediately"}
		},
		"required":["name"]
	}`)
}

func (t *ComposeSkillTool) Execute(ctx context.Context, params string, tctx *Context) (*Result, error) {
	if tctx.Store == nil {
		return nil, fmt.Errorf("database store not available")
	}

	var args struct {
		Name            string                    `json:"name"`
		Kind            string                    `json:"kind"`
		Description     string                    `json:"description"`
		Content         string                    `json:"content"`
		Triggers        []string                  `json:"triggers"`
		Priority        int                       `json:"priority"`
		ToolChain       []db.SkillCompositionStep `json:"tool_chain"`
		PolicyOverrides json.RawMessage           `json:"policy_overrides"`
		ScriptPath      string                    `json:"script_path"`
		RiskLevel       string                    `json:"risk_level"`
		Version         string                    `json:"version"`
		Author          string                    `json:"author"`
		RegistrySource  string                    `json:"registry_source"`
		Enabled         *bool                     `json:"enabled"`
	}
	if err := json.Unmarshal([]byte(params), &args); err != nil {
		return nil, fmt.Errorf("invalid parameters: %w", err)
	}

	isSubagent, err := db.IsSubagentName(ctx, tctx.Store, firstNonEmpty(tctx.AgentID, tctx.AgentName))
	if err != nil {
		return nil, err
	}
	if isSubagent {
		return nil, fmt.Errorf("subagents may not compose skills directly")
	}

	enabled := true
	if args.Enabled != nil {
		enabled = *args.Enabled
	}
	repo := db.NewSkillCompositionRepository(tctx.Store, t.skillsDir)
	created, spec, err := repo.Upsert(ctx, db.SkillCompositionSpec{
		Name:            args.Name,
		Kind:            args.Kind,
		Description:     args.Description,
		Content:         args.Content,
		Triggers:        args.Triggers,
		Priority:        args.Priority,
		ToolChain:       args.ToolChain,
		PolicyOverrides: args.PolicyOverrides,
		ScriptPath:      args.ScriptPath,
		RiskLevel:       args.RiskLevel,
		Version:         args.Version,
		Author:          args.Author,
		RegistrySource:  args.RegistrySource,
		Enabled:         enabled,
	})
	if err != nil {
		return nil, err
	}

	data, _ := json.Marshal(map[string]any{
		"created": created,
		"skill":   spec,
	})
	return &Result{Output: string(data)}, nil
}
