package agent

import (
	"encoding/json"
	"time"

	"roboticus/internal/core"
)

type AgentManifest struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	Version      string    `json:"version"`
	Description  string    `json:"description"`
	Capabilities []string  `json:"capabilities"`
	Channels     []string  `json:"channels"`
	Tools        []string  `json:"tools"`
	Skills       []string  `json:"skills"`
	CreatedAt    time.Time `json:"created_at"`
	// SkillManifests holds the full skill metadata for each loaded skill.
	// This complements the Skills []string field (which holds just names)
	// with the complete manifest including triggers, priority, tool chains,
	// and policy overrides — matching the Rust reference's agent manifest.
	SkillManifests []core.SkillManifest `json:"skill_manifests,omitempty"`
}

func NewAgentManifest(id, name, version, description string) *AgentManifest {
	return &AgentManifest{
		ID: id, Name: name, Version: version, Description: description,
		CreatedAt: time.Now(),
	}
}

func (m *AgentManifest) AddCapability(cap string) { m.Capabilities = append(m.Capabilities, cap) }
func (m *AgentManifest) AddChannel(ch string)     { m.Channels = append(m.Channels, ch) }
func (m *AgentManifest) AddTool(tool string)      { m.Tools = append(m.Tools, tool) }
func (m *AgentManifest) AddSkill(skill string)    { m.Skills = append(m.Skills, skill) }

func (m *AgentManifest) ToJSON() ([]byte, error) {
	return json.MarshalIndent(m, "", "  ")
}

func ParseManifest(data []byte) (*AgentManifest, error) {
	var m AgentManifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	return &m, nil
}

func (m *AgentManifest) HasCapability(cap string) bool {
	for _, c := range m.Capabilities {
		if c == cap {
			return true
		}
	}
	return false
}
