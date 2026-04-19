package tools

import (
	"context"
	"encoding/json"
	"testing"

	"roboticus/internal/plugin"
)

type testPlugin struct {
	name    string
	version string
	tools   []plugin.ToolDef
}

func (p *testPlugin) Name() string    { return p.name }
func (p *testPlugin) Version() string { return p.version }
func (p *testPlugin) Tools() []plugin.ToolDef {
	return p.tools
}
func (p *testPlugin) Init() error { return nil }
func (p *testPlugin) ExecuteTool(_ context.Context, toolName string, _ json.RawMessage) (*plugin.ToolResult, error) {
	return &plugin.ToolResult{Success: true, Output: "ok:" + toolName}, nil
}
func (p *testPlugin) Shutdown() error { return nil }

func TestRegisterPluginTools_SyncsActivePluginTools(t *testing.T) {
	reg := NewRegistry()
	pluginReg := plugin.NewRegistry(nil, nil, plugin.PermissionPolicy{})
	if err := pluginReg.Register(&testPlugin{
		name:    "plug",
		version: "1.0.0",
		tools:   []plugin.ToolDef{{Name: "plug-tool", Description: "plugin tool"}},
	}); err != nil {
		t.Fatal(err)
	}
	if errs := pluginReg.InitAll(); len(errs) > 0 {
		t.Fatal(errs[0])
	}

	n := RegisterPluginTools(reg, pluginReg)
	if n != 1 {
		t.Fatalf("registered = %d, want 1", n)
	}
	if reg.Get("plug-tool") == nil {
		t.Fatal("plugin tool missing from main tool registry")
	}
	defs := reg.ToolDefs()
	if len(defs) != 1 || defs[0].Function.Name != "plug-tool" {
		t.Fatalf("tool defs = %+v", defs)
	}
}

func TestRegisterPluginTools_RemovesDisabledPluginTools(t *testing.T) {
	reg := NewRegistry()
	pluginReg := plugin.NewRegistry(nil, nil, plugin.PermissionPolicy{})
	if err := pluginReg.Register(&testPlugin{
		name:    "plug",
		version: "1.0.0",
		tools:   []plugin.ToolDef{{Name: "plug-tool", Description: "plugin tool"}},
	}); err != nil {
		t.Fatal(err)
	}
	if errs := pluginReg.InitAll(); len(errs) > 0 {
		t.Fatal(errs[0])
	}
	RegisterPluginTools(reg, pluginReg)
	if reg.Get("plug-tool") == nil {
		t.Fatal("plugin tool missing from main tool registry")
	}
	if err := pluginReg.Disable("plug"); err != nil {
		t.Fatal(err)
	}
	RegisterPluginTools(reg, pluginReg)
	if reg.Get("plug-tool") != nil {
		t.Fatal("disabled plugin tool should be removed from main tool registry")
	}
}
