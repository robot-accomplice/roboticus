package core

import "testing"

func TestProfileRegistry_DefaultExists(t *testing.T) {
	reg := NewProfileRegistry(t.TempDir())
	profiles := reg.List()
	if len(profiles) != 1 {
		t.Fatalf("expected 1 default profile, got %d", len(profiles))
	}
	if profiles[0].Name != "Default" {
		t.Errorf("default profile name = %q", profiles[0].Name)
	}
	if !profiles[0].Active {
		t.Error("default profile should be active")
	}
}

func TestProfileRegistry_CreateAndList(t *testing.T) {
	reg := NewProfileRegistry(t.TempDir())
	entry, err := reg.Create("dev", "Development profile")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if entry.Name != "dev" {
		t.Errorf("name = %q, want dev", entry.Name)
	}

	profiles := reg.List()
	if len(profiles) != 2 {
		t.Errorf("expected 2 profiles, got %d", len(profiles))
	}
}

func TestProfileRegistry_Switch(t *testing.T) {
	reg := NewProfileRegistry(t.TempDir())
	_, _ = reg.Create("staging", "Staging env")

	if err := reg.Switch("staging"); err != nil {
		t.Fatalf("switch: %v", err)
	}

	active := reg.Active()
	if active.Name != "staging" {
		t.Errorf("active = %q, want staging", active.Name)
	}
}

func TestProfileRegistry_CannotDeleteDefault(t *testing.T) {
	reg := NewProfileRegistry(t.TempDir())
	err := reg.Delete("default")
	if err == nil {
		t.Error("should not allow deleting default profile")
	}
}

func TestProfileRegistry_Delete(t *testing.T) {
	reg := NewProfileRegistry(t.TempDir())
	_, _ = reg.Create("temp", "Temporary")

	if err := reg.Delete("temp"); err != nil {
		t.Fatalf("delete: %v", err)
	}

	profiles := reg.List()
	if len(profiles) != 1 {
		t.Errorf("expected 1 profile after delete, got %d", len(profiles))
	}
}

func TestProfileRegistry_DeleteActiveSwitchesToDefault(t *testing.T) {
	reg := NewProfileRegistry(t.TempDir())
	_, _ = reg.Create("active-one", "")
	_ = reg.Switch("active-one")
	_ = reg.Delete("active-one")

	active := reg.Active()
	if active.Name != "Default" {
		t.Errorf("after deleting active, should fall back to default, got %q", active.Name)
	}
}

func TestProfileRegistry_DuplicateCreate(t *testing.T) {
	reg := NewProfileRegistry(t.TempDir())
	_, _ = reg.Create("dup", "")
	_, err := reg.Create("dup", "")
	if err == nil {
		t.Error("duplicate create should fail")
	}
}
