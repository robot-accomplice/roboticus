package agent

import (
	"fmt"
	"strings"
)

// Capability describes a single available capability.
type Capability struct {
	Name        string
	Description string
	Source      string // "builtin", "skill", "plugin:name", "mcp:server"
}

// CapabilityManifest summarizes all available capabilities.
type CapabilityManifest struct {
	Tools       []Capability
	Skills      []Capability
	Plugins     []Capability
	MCP         []Capability
	ToolCount   int
	SkillCount  int
	PluginCount int
	MCPCount    int
	TotalCount  int
}

// CapabilityRegistry tracks all available capabilities for self-knowledge.
type CapabilityRegistry struct {
	tools   []Capability
	skills  []Capability
	plugins []Capability
	mcp     []Capability
}

// NewCapabilityRegistry creates an empty capability registry.
func NewCapabilityRegistry() *CapabilityRegistry {
	return &CapabilityRegistry{}
}

// AddTool registers a tool capability.
func (cr *CapabilityRegistry) AddTool(c Capability) { cr.tools = append(cr.tools, c) }

// AddSkill registers a skill capability.
func (cr *CapabilityRegistry) AddSkill(c Capability) { cr.skills = append(cr.skills, c) }

// AddPlugin registers a plugin capability.
func (cr *CapabilityRegistry) AddPlugin(c Capability) { cr.plugins = append(cr.plugins, c) }

// AddMCP registers an MCP server capability.
func (cr *CapabilityRegistry) AddMCP(c Capability) { cr.mcp = append(cr.mcp, c) }

// Discover returns a complete manifest of all capabilities.
func (cr *CapabilityRegistry) Discover() CapabilityManifest {
	return CapabilityManifest{
		Tools:       cr.tools,
		Skills:      cr.skills,
		Plugins:     cr.plugins,
		MCP:         cr.mcp,
		ToolCount:   len(cr.tools),
		SkillCount:  len(cr.skills),
		PluginCount: len(cr.plugins),
		MCPCount:    len(cr.mcp),
		TotalCount:  len(cr.tools) + len(cr.skills) + len(cr.plugins) + len(cr.mcp),
	}
}

// DiscoverForPrompt returns a token-budget-aware summary for system prompt injection.
func (cr *CapabilityRegistry) DiscoverForPrompt(budget int) string {
	var b strings.Builder

	total := len(cr.tools) + len(cr.skills) + len(cr.plugins) + len(cr.mcp)
	fmt.Fprintf(&b, "You have %d capabilities available:\n", total)

	remaining := budget * 4 // chars (4 chars/token heuristic)
	remaining -= b.Len()

	writeSection := func(name string, caps []Capability) {
		if len(caps) == 0 || remaining <= 0 {
			return
		}
		header := fmt.Sprintf("\n%s (%d):\n", name, len(caps))
		b.WriteString(header)
		remaining -= len(header)

		for i, c := range caps {
			line := fmt.Sprintf("- %s: %s\n", c.Name, c.Description)
			if remaining-len(line) < 0 {
				fmt.Fprintf(&b, "- ... and %d more\n", len(caps)-i)
				break
			}
			b.WriteString(line)
			remaining -= len(line)
		}
	}

	writeSection("Tools", cr.tools)
	writeSection("Skills", cr.skills)
	writeSection("Plugins", cr.plugins)
	writeSection("MCP Servers", cr.mcp)

	return b.String()
}
