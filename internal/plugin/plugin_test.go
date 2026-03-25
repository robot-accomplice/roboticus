package plugin

import (
	"context"
	"encoding/json"
	"testing"
)

type testPlugin struct {
	name    string
	version string
	tools   []ToolDef
}

func (p *testPlugin) Name() string     { return p.name }
func (p *testPlugin) Version() string  { return p.version }
func (p *testPlugin) Tools() []ToolDef { return p.tools }
func (p *testPlugin) Init() error      { return nil }
func (p *testPlugin) ExecuteTool(ctx context.Context, toolName string, input json.RawMessage) (*ToolResult, error) {
	return &ToolResult{Success: true, Output: "executed: " + toolName}, nil
}
func (p *testPlugin) Shutdown() error { return nil }

func TestRegistry_RegisterAndExecute(t *testing.T) {
	reg := NewRegistry(nil, nil, PermissionPolicy{})

	p := &testPlugin{
		name:    "test-plugin",
		version: "1.0.0",
		tools: []ToolDef{
			{Name: "greet", Description: "Says hello"},
		},
	}

	if err := reg.Register(p); err != nil {
		t.Fatal(err)
	}

	errs := reg.InitAll()
	if len(errs) > 0 {
		t.Fatalf("init errors: %v", errs)
	}

	result, err := reg.ExecuteTool(context.Background(), "greet", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Success {
		t.Error("expected success")
	}
	if result.Output != "executed: greet" {
		t.Errorf("unexpected output: %q", result.Output)
	}
}

func TestRegistry_DenyList(t *testing.T) {
	reg := NewRegistry(nil, []string{"bad-plugin"}, PermissionPolicy{})

	p := &testPlugin{name: "bad-plugin", version: "1.0.0"}
	err := reg.Register(p)
	if err == nil {
		t.Error("denied plugin should be rejected")
	}
}

func TestRegistry_AllowList(t *testing.T) {
	reg := NewRegistry([]string{"approved"}, nil, PermissionPolicy{})

	err := reg.Register(&testPlugin{name: "unapproved", version: "1.0.0"})
	if err == nil {
		t.Error("unapproved plugin should be rejected")
	}

	err = reg.Register(&testPlugin{name: "approved", version: "1.0.0"})
	if err != nil {
		t.Errorf("approved plugin should be accepted: %v", err)
	}
}

func TestRegistry_InvalidName(t *testing.T) {
	reg := NewRegistry(nil, nil, PermissionPolicy{})

	err := reg.Register(&testPlugin{name: "../escape", version: "1.0.0"})
	if err == nil {
		t.Error("path traversal in name should be rejected")
	}
}

func TestRegistry_EnableDisable(t *testing.T) {
	reg := NewRegistry(nil, nil, PermissionPolicy{})
	reg.Register(&testPlugin{
		name: "toggle", version: "1.0.0",
		tools: []ToolDef{{Name: "tool1"}},
	})
	reg.InitAll()

	if err := reg.Disable("toggle"); err != nil {
		t.Fatal(err)
	}

	_, err := reg.ExecuteTool(context.Background(), "tool1", nil)
	if err == nil {
		t.Error("disabled plugin tools should not execute")
	}

	reg.Enable("toggle")
	_, err = reg.ExecuteTool(context.Background(), "tool1", nil)
	if err != nil {
		t.Errorf("re-enabled plugin should work: %v", err)
	}
}

func TestValidateManifest(t *testing.T) {
	valid := &Manifest{Name: "my-plugin", Version: "1.0.0"}
	if err := ValidateManifest(valid); err != nil {
		t.Errorf("valid manifest rejected: %v", err)
	}

	invalid := &Manifest{Name: "../bad", Version: "1.0.0"}
	if err := ValidateManifest(invalid); err == nil {
		t.Error("path traversal should be rejected")
	}

	empty := &Manifest{Name: "", Version: "1.0.0"}
	if err := ValidateManifest(empty); err == nil {
		t.Error("empty name should be rejected")
	}
}
