package agent

import (
	"fmt"
	"testing"
)

func TestCapabilityRegistry_Discover(t *testing.T) {
	reg := NewCapabilityRegistry()
	reg.AddTool(Capability{Name: "web_search", Description: "search the web", Source: "builtin"})
	reg.AddTool(Capability{Name: "file_read", Description: "read files", Source: "builtin"})
	reg.AddSkill(Capability{Name: "weather", Description: "get weather", Source: "skill"})
	reg.AddPlugin(Capability{Name: "calculator", Description: "math ops", Source: "plugin:calc"})
	reg.AddMCP(Capability{Name: "github", Description: "github tools", Source: "mcp:github"})

	manifest := reg.Discover()
	if manifest.ToolCount != 2 {
		t.Errorf("ToolCount = %d, want 2", manifest.ToolCount)
	}
	if manifest.SkillCount != 1 {
		t.Errorf("SkillCount = %d, want 1", manifest.SkillCount)
	}
	if manifest.PluginCount != 1 {
		t.Errorf("PluginCount = %d, want 1", manifest.PluginCount)
	}
	if manifest.MCPCount != 1 {
		t.Errorf("MCPCount = %d, want 1", manifest.MCPCount)
	}
	if manifest.TotalCount != 5 {
		t.Errorf("TotalCount = %d, want 5", manifest.TotalCount)
	}
}

func TestCapabilityRegistry_DiscoverForPrompt(t *testing.T) {
	reg := NewCapabilityRegistry()
	for i := 0; i < 50; i++ {
		reg.AddTool(Capability{
			Name:        fmt.Sprintf("tool_%d", i),
			Description: "a tool that does something useful and interesting",
			Source:      "builtin",
		})
	}

	// Small budget should truncate
	prompt := reg.DiscoverForPrompt(200)
	if len(prompt) > 1000 { // generous char limit for 200 tokens
		t.Errorf("prompt too long for budget: %d chars", len(prompt))
	}
	if prompt == "" {
		t.Error("prompt should not be empty")
	}
}

func TestCapabilityRegistry_Empty(t *testing.T) {
	reg := NewCapabilityRegistry()
	manifest := reg.Discover()
	if manifest.TotalCount != 0 {
		t.Errorf("empty registry should have 0 total, got %d", manifest.TotalCount)
	}

	prompt := reg.DiscoverForPrompt(1000)
	if prompt == "" {
		t.Error("even empty registry should produce a prompt")
	}
}
