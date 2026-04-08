package plugin

import (
	"testing"

	"roboticus/internal/core"
)

func TestPluginCatalog_RegisterAndList(t *testing.T) {
	cat := NewPluginCatalog()
	cat.Register(PluginCatalogEntry{Name: "beta", Version: "1.0", Description: "plugin b"})
	cat.Register(PluginCatalogEntry{Name: "alpha", Version: "2.0", Description: "plugin a"})

	list := cat.List()
	if len(list) != 2 {
		t.Fatalf("expected 2, got %d", len(list))
	}
	if list[0].Name != "alpha" {
		t.Fatalf("expected sorted, got %s first", list[0].Name)
	}
}

func TestPluginCatalog_Find(t *testing.T) {
	cat := NewPluginCatalog()
	cat.Register(PluginCatalogEntry{
		Name:        "MyPlugin",
		Version:     "1.0",
		Description: "test",
		Manifest:    core.DefaultSkillManifest(),
	})

	entry, ok := cat.Find("myplugin") // case-insensitive
	if !ok {
		t.Fatal("expected to find plugin")
	}
	if entry.Name != "MyPlugin" {
		t.Fatalf("unexpected name: %s", entry.Name)
	}
}

func TestPluginCatalog_FindMissing(t *testing.T) {
	cat := NewPluginCatalog()
	_, ok := cat.Find("nope")
	if ok {
		t.Fatal("expected not found")
	}
}

func TestPluginCatalog_RegisterOverwrite(t *testing.T) {
	cat := NewPluginCatalog()
	cat.Register(PluginCatalogEntry{Name: "foo", Version: "1.0"})
	cat.Register(PluginCatalogEntry{Name: "foo", Version: "2.0"})

	list := cat.List()
	if len(list) != 1 {
		t.Fatalf("expected 1, got %d", len(list))
	}
	if list[0].Version != "2.0" {
		t.Fatalf("expected overwrite, got version %s", list[0].Version)
	}
}
