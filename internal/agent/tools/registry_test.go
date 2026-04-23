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

func TestRegistry_PreservesRegistrationOrderAcrossSurfaces(t *testing.T) {
	reg := NewRegistry()
	reg.Register(&RuntimeContextTool{})
	reg.Register(&EchoTool{})
	reg.Register(&BashTool{})

	names := reg.Names()
	want := []string{"get_runtime_context", "echo", "bash"}
	if len(names) != len(want) {
		t.Fatalf("Names len = %d, want %d", len(names), len(want))
	}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("Names[%d] = %q, want %q", i, names[i], want[i])
		}
	}

	defs := reg.ToolDefs()
	for i := range want {
		if defs[i].Function.Name != want[i] {
			t.Fatalf("ToolDefs[%d] = %q, want %q", i, defs[i].Function.Name, want[i])
		}
	}

	descs := reg.Descriptors()
	for i := range want {
		if descs[i].Name != want[i] {
			t.Fatalf("Descriptors[%d] = %q, want %q", i, descs[i].Name, want[i])
		}
	}
}

func TestRegistry_UnregisterRemovesFromStableOrder(t *testing.T) {
	reg := NewRegistry()
	reg.Register(&EchoTool{})
	reg.Register(&BashTool{})
	reg.Register(&RuntimeContextTool{})

	reg.Unregister("bash")

	names := reg.Names()
	want := []string{"echo", "get_runtime_context"}
	if len(names) != len(want) {
		t.Fatalf("Names len = %d, want %d", len(names), len(want))
	}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("Names[%d] = %q, want %q", i, names[i], want[i])
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
