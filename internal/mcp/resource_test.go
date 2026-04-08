package mcp

import "testing"

func TestMCPResourceRegistry_RegisterAndGet(t *testing.T) {
	reg := NewMCPResourceRegistry()

	r := MCPResource{URI: "file:///a.txt", Name: "a", Description: "file a", MIMEType: "text/plain"}
	reg.Register(r)

	got, ok := reg.Get("file:///a.txt")
	if !ok {
		t.Fatal("expected resource to exist")
	}
	if got.Name != "a" || got.MIMEType != "text/plain" {
		t.Fatalf("unexpected resource: %+v", got)
	}
}

func TestMCPResourceRegistry_GetMissing(t *testing.T) {
	reg := NewMCPResourceRegistry()
	_, ok := reg.Get("file:///missing")
	if ok {
		t.Fatal("expected not found")
	}
}

func TestMCPResourceRegistry_List(t *testing.T) {
	reg := NewMCPResourceRegistry()
	reg.Register(MCPResource{URI: "file:///b.txt", Name: "b"})
	reg.Register(MCPResource{URI: "file:///a.txt", Name: "a"})

	list := reg.List()
	if len(list) != 2 {
		t.Fatalf("expected 2 resources, got %d", len(list))
	}
	if list[0].URI != "file:///a.txt" {
		t.Fatalf("expected sorted by URI, got %s first", list[0].URI)
	}
}

func TestMCPResourceRegistry_Overwrite(t *testing.T) {
	reg := NewMCPResourceRegistry()
	reg.Register(MCPResource{URI: "file:///a.txt", Name: "old"})
	reg.Register(MCPResource{URI: "file:///a.txt", Name: "new"})

	got, _ := reg.Get("file:///a.txt")
	if got.Name != "new" {
		t.Fatalf("expected overwrite, got %s", got.Name)
	}
	if len(reg.List()) != 1 {
		t.Fatal("expected 1 resource after overwrite")
	}
}
