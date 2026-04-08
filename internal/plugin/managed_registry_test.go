package plugin

import (
	"testing"

	"roboticus/internal/core"
)

func TestPluginRegistry_RegisterAndGet(t *testing.T) {
	pr := NewPluginRegistry()
	manifest := core.DefaultSkillManifest()
	manifest.Name = "test-plugin"

	if err := pr.Register("test-plugin", manifest); err != nil {
		t.Fatal(err)
	}

	p, ok := pr.Get("test-plugin")
	if !ok {
		t.Fatal("expected plugin to exist")
	}
	if p.Name != "test-plugin" || !p.Active {
		t.Fatalf("unexpected plugin: %+v", p)
	}
}

func TestPluginRegistry_RegisterDuplicate(t *testing.T) {
	pr := NewPluginRegistry()
	_ = pr.Register("dup", core.DefaultSkillManifest())
	err := pr.Register("dup", core.DefaultSkillManifest())
	if err == nil {
		t.Fatal("expected error on duplicate")
	}
}

func TestPluginRegistry_RegisterEmptyName(t *testing.T) {
	pr := NewPluginRegistry()
	err := pr.Register("", core.DefaultSkillManifest())
	if err == nil {
		t.Fatal("expected error on empty name")
	}
}

func TestPluginRegistry_GetMissing(t *testing.T) {
	pr := NewPluginRegistry()
	_, ok := pr.Get("nope")
	if ok {
		t.Fatal("expected not found")
	}
}

func TestPluginRegistry_List(t *testing.T) {
	pr := NewPluginRegistry()
	_ = pr.Register("a", core.DefaultSkillManifest())
	_ = pr.Register("b", core.DefaultSkillManifest())

	list := pr.List()
	if len(list) != 2 {
		t.Fatalf("expected 2, got %d", len(list))
	}
}

func TestPluginRegistry_SetActive(t *testing.T) {
	pr := NewPluginRegistry()
	_ = pr.Register("toggle", core.DefaultSkillManifest())

	if err := pr.SetActive("toggle", false); err != nil {
		t.Fatal(err)
	}

	p, _ := pr.Get("toggle")
	if p.Active {
		t.Fatal("expected inactive")
	}

	if err := pr.SetActive("toggle", true); err != nil {
		t.Fatal(err)
	}

	p2, _ := pr.Get("toggle")
	if !p2.Active {
		t.Fatal("expected active")
	}
}

func TestPluginRegistry_SetActiveMissing(t *testing.T) {
	pr := NewPluginRegistry()
	err := pr.SetActive("missing", true)
	if err == nil {
		t.Fatal("expected error")
	}
}
