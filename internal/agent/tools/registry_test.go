package tools

import "testing"

func TestRegistry_RegisterAndGet(t *testing.T) {
	reg := NewRegistry()
	reg.Register(&EchoTool{})

	tool := reg.Get("echo")
	if tool == nil {
		t.Fatal("should find echo tool")
	}
	if tool.Name() != "echo" {
		t.Errorf("name = %s", tool.Name())
	}
}

func TestRegistry_Get_NotFound(t *testing.T) {
	reg := NewRegistry()
	tool := reg.Get("nonexistent")
	if tool != nil {
		t.Error("should return nil for missing tool")
	}
}

func TestRegistry_List(t *testing.T) {
	reg := NewRegistry()
	reg.Register(&EchoTool{})
	reg.Register(&ReadFileTool{})

	tools := reg.List()
	if len(tools) != 2 {
		t.Errorf("tools = %d, want 2", len(tools))
	}
}

func TestRegistry_Names(t *testing.T) {
	reg := NewRegistry()
	reg.Register(&EchoTool{})
	reg.Register(&BashTool{})

	names := reg.Names()
	if len(names) != 2 {
		t.Errorf("names = %d, want 2", len(names))
	}
}

func TestRegistry_ToolDefs(t *testing.T) {
	reg := NewRegistry()
	reg.Register(&EchoTool{})
	reg.Register(&RuntimeContextTool{})

	defs := reg.ToolDefs()
	if len(defs) != 2 {
		t.Errorf("defs = %d, want 2", len(defs))
	}
	for _, d := range defs {
		if d.Function.Name == "" {
			t.Error("def function name should not be empty")
		}
	}
}

func TestRegistry_Get_IntrospectionAlias(t *testing.T) {
	reg := NewRegistry()
	base := NewIntrospectionTool("roboticus", "0.1.0", reg.Names)
	reg.Register(base)
	reg.Register(NewIntrospectionAliasTool("introspection", base))

	tool := reg.Get("introspection")
	if tool == nil {
		t.Fatal("should find introspection alias")
	}
	if tool.Name() != "introspection" {
		t.Fatalf("alias name = %q", tool.Name())
	}

	canonical := reg.Get("introspect")
	if canonical == nil {
		t.Fatal("should find canonical introspection tool")
	}
	if canonical == tool {
		t.Fatal("alias lookup should not replace canonical tool instance")
	}
}
