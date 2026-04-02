package plugin

import (
	"context"
	"encoding/json"
	"testing"
)

func TestScriptPlugin_Properties(t *testing.T) {
	manifest := Manifest{
		Name:    "test-plugin",
		Version: "1.0.0",
		Tools: []ManifestTool{
			{Name: "greet", Description: "Greet someone"},
		},
	}

	sp := NewScriptPlugin(manifest, t.TempDir())
	if sp.Name() != "test-plugin" {
		t.Errorf("name = %s", sp.Name())
	}
	if sp.Version() != "1.0.0" {
		t.Errorf("version = %s", sp.Version())
	}

	tools := sp.Tools()
	if len(tools) != 1 {
		t.Fatalf("tools = %d, want 1", len(tools))
	}
}

func TestScriptPlugin_ExecuteTool_NotFound(t *testing.T) {
	manifest := Manifest{Name: "test", Version: "1.0.0"}
	sp := NewScriptPlugin(manifest, t.TempDir())

	_, err := sp.ExecuteTool(context.Background(), "nonexistent", json.RawMessage(`{}`))
	if err == nil {
		t.Error("should return error for nonexistent tool")
	}
}

func TestScriptPlugin_Init(t *testing.T) {
	manifest := Manifest{Name: "test", Version: "1.0.0"}
	sp := NewScriptPlugin(manifest, t.TempDir())
	err := sp.Init()
	if err != nil {
		t.Fatalf("init: %v", err)
	}
}

func TestRegistry_RegisterAndList(t *testing.T) {
	reg := NewRegistry(nil, nil, PermissionPolicy{})
	manifest := Manifest{
		Name:    "test-plugin",
		Version: "1.0.0",
		Tools:   []ManifestTool{{Name: "echo", Description: "Echo"}},
	}

	sp := NewScriptPlugin(manifest, t.TempDir())
	if err := reg.Register(sp); err != nil {
		t.Fatalf("register: %v", err)
	}

	plugins := reg.List()
	if len(plugins) != 1 {
		t.Fatalf("plugins = %d, want 1", len(plugins))
	}
	if plugins[0].Name != "test-plugin" {
		t.Errorf("name = %s", plugins[0].Name)
	}

	// Enable to make tools visible via AllTools (only active plugins).
	_ = reg.Enable("test-plugin")

	tools := reg.AllTools()
	if len(tools) != 1 {
		t.Errorf("tools = %d, want 1", len(tools))
	}
}

func TestRegistry_EnableDisable_Script(t *testing.T) {
	reg := NewRegistry(nil, nil, PermissionPolicy{})
	manifest := Manifest{Name: "toggle-me", Version: "1.0.0"}
	sp := NewScriptPlugin(manifest, t.TempDir())
	_ = reg.Register(sp)

	if err := reg.Disable("toggle-me"); err != nil {
		t.Fatalf("disable: %v", err)
	}

	plugins := reg.List()
	for _, p := range plugins {
		if p.Name == "toggle-me" && p.Status != StatusDisabled {
			t.Error("should be disabled")
		}
	}

	if err := reg.Enable("toggle-me"); err != nil {
		t.Fatalf("enable: %v", err)
	}
}

func TestRegistry_DenyList_Script(t *testing.T) {
	reg := NewRegistry(nil, []string{"blocked-plugin"}, PermissionPolicy{})
	manifest := Manifest{Name: "blocked-plugin", Version: "1.0.0"}
	sp := NewScriptPlugin(manifest, t.TempDir())
	err := reg.Register(sp)
	if err == nil {
		t.Error("deny-listed plugin should fail")
	}
}

func TestRegistry_AllowList_Script(t *testing.T) {
	reg := NewRegistry([]string{"allowed-plugin"}, nil, PermissionPolicy{})

	manifest := Manifest{Name: "not-allowed", Version: "1.0.0"}
	sp := NewScriptPlugin(manifest, t.TempDir())
	err := reg.Register(sp)
	if err == nil {
		t.Error("non-allowed plugin should fail")
	}

	manifest2 := Manifest{Name: "allowed-plugin", Version: "1.0.0"}
	sp2 := NewScriptPlugin(manifest2, t.TempDir())
	err = reg.Register(sp2)
	if err != nil {
		t.Errorf("allowed plugin should register: %v", err)
	}
}

func TestRegistry_EnableDisable_NotFound(t *testing.T) {
	reg := NewRegistry(nil, nil, PermissionPolicy{})
	if err := reg.Enable("ghost"); err == nil {
		t.Error("enable nonexistent should error")
	}
	if err := reg.Disable("ghost"); err == nil {
		t.Error("disable nonexistent should error")
	}
}
