package core

import "encoding/json"

// SkillKind distinguishes between structured (manifest-driven) and instruction
// (inline text) skills.
type SkillKind int

const (
	SkillKindStructured SkillKind = iota
	SkillKindInstruction
)

func (k SkillKind) String() string {
	switch k {
	case SkillKindStructured:
		return "structured"
	case SkillKindInstruction:
		return "instruction"
	default:
		return "unknown"
	}
}

// SkillTrigger defines the conditions under which a skill activates.
type SkillTrigger struct {
	Keywords      []string `json:"keywords,omitempty"`
	ToolNames     []string `json:"tool_names,omitempty"`
	RegexPatterns []string `json:"regex_patterns,omitempty"`
}

// SkillManifest describes a structured skill with full metadata.
type SkillManifest struct {
	Name            string          `json:"name"`
	Description     string          `json:"description"`
	Kind            SkillKind       `json:"kind"`
	Triggers        SkillTrigger    `json:"triggers"`
	Priority        int             `json:"priority"`
	ToolChain       []ToolChainStep `json:"tool_chain,omitempty"`
	PolicyOverrides json.RawMessage `json:"policy_overrides,omitempty"`
	ScriptPath      string          `json:"script_path,omitempty"`
	RiskLevel       RiskLevel       `json:"risk_level"`
	Version         string          `json:"version"`
	Author          string          `json:"author"`
}

// DefaultSkillManifest returns a SkillManifest with sensible defaults.
func DefaultSkillManifest() SkillManifest {
	return SkillManifest{
		Priority:  5,
		RiskLevel: RiskLevelCaution,
		Version:   "0.0.0",
		Author:    "local",
	}
}

// ToolChainStep is a single step in a skill's tool chain.
type ToolChainStep struct {
	ToolName string          `json:"tool_name"`
	Params   json.RawMessage `json:"params,omitempty"`
}

// InstructionSkill is a lightweight skill defined by inline text content.
type InstructionSkill struct {
	Name        string       `json:"name"`
	Description string       `json:"description"`
	Triggers    SkillTrigger `json:"triggers"`
	Priority    int          `json:"priority"`
	Body        string       `json:"body"`
	Version     string       `json:"version"`
	Author      string       `json:"author"`
}

// DefaultInstructionSkill returns an InstructionSkill with sensible defaults.
func DefaultInstructionSkill() InstructionSkill {
	return InstructionSkill{
		Priority: 5,
		Version:  "0.0.0",
		Author:   "local",
	}
}
