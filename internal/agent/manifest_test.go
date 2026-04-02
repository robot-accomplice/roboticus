package agent

import (
	"testing"
)

func TestAgentManifest_Create(t *testing.T) {
	m := NewAgentManifest("id-1", "TestBot", "1.0.0", "A test agent")

	if m.ID != "id-1" {
		t.Errorf("ID = %q, want %q", m.ID, "id-1")
	}
	if m.Name != "TestBot" {
		t.Errorf("Name = %q, want %q", m.Name, "TestBot")
	}
	if m.Version != "1.0.0" {
		t.Errorf("Version = %q, want %q", m.Version, "1.0.0")
	}
	if m.Description != "A test agent" {
		t.Errorf("Description = %q, want %q", m.Description, "A test agent")
	}
	if m.CreatedAt.IsZero() {
		t.Error("CreatedAt should not be zero")
	}
	if len(m.Capabilities) != 0 {
		t.Errorf("Capabilities should be empty, got %v", m.Capabilities)
	}
}

func TestAgentManifest_AddFields(t *testing.T) {
	m := NewAgentManifest("id-2", "Bot", "2.0.0", "desc")
	m.AddCapability("chat")
	m.AddCapability("tool-use")
	m.AddChannel("api")
	m.AddTool("web_search")
	m.AddSkill("summarize")

	if len(m.Capabilities) != 2 {
		t.Errorf("Capabilities len = %d, want 2", len(m.Capabilities))
	}
	if len(m.Channels) != 1 {
		t.Errorf("Channels len = %d, want 1", len(m.Channels))
	}
	if len(m.Tools) != 1 {
		t.Errorf("Tools len = %d, want 1", len(m.Tools))
	}
	if len(m.Skills) != 1 {
		t.Errorf("Skills len = %d, want 1", len(m.Skills))
	}
}

func TestAgentManifest_JSONRoundtrip(t *testing.T) {
	m := NewAgentManifest("id-3", "RoundtripBot", "1.1.0", "roundtrip test")
	m.AddCapability("memory")
	m.AddChannel("slack")
	m.AddTool("fs_read")
	m.AddSkill("triage")

	data, err := m.ToJSON()
	if err != nil {
		t.Fatalf("ToJSON error: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("ToJSON returned empty bytes")
	}

	m2, err := ParseManifest(data)
	if err != nil {
		t.Fatalf("ParseManifest error: %v", err)
	}

	if m2.ID != m.ID {
		t.Errorf("ID mismatch: got %q, want %q", m2.ID, m.ID)
	}
	if m2.Name != m.Name {
		t.Errorf("Name mismatch: got %q, want %q", m2.Name, m.Name)
	}
	if m2.Version != m.Version {
		t.Errorf("Version mismatch: got %q, want %q", m2.Version, m.Version)
	}
	if len(m2.Capabilities) != 1 || m2.Capabilities[0] != "memory" {
		t.Errorf("Capabilities mismatch: %v", m2.Capabilities)
	}
	if len(m2.Channels) != 1 || m2.Channels[0] != "slack" {
		t.Errorf("Channels mismatch: %v", m2.Channels)
	}
	if len(m2.Tools) != 1 || m2.Tools[0] != "fs_read" {
		t.Errorf("Tools mismatch: %v", m2.Tools)
	}
	if len(m2.Skills) != 1 || m2.Skills[0] != "triage" {
		t.Errorf("Skills mismatch: %v", m2.Skills)
	}
}

func TestAgentManifest_HasCapability(t *testing.T) {
	m := NewAgentManifest("id-4", "CapBot", "1.0.0", "")
	m.AddCapability("chat")
	m.AddCapability("scheduling")

	if !m.HasCapability("chat") {
		t.Error("HasCapability('chat') = false, want true")
	}
	if !m.HasCapability("scheduling") {
		t.Error("HasCapability('scheduling') = false, want true")
	}
	if m.HasCapability("nonexistent") {
		t.Error("HasCapability('nonexistent') = true, want false")
	}
}

func TestAgentManifest_Empty(t *testing.T) {
	m := NewAgentManifest("", "", "", "")

	if m.HasCapability("anything") {
		t.Error("empty manifest should not have any capability")
	}

	data, err := m.ToJSON()
	if err != nil {
		t.Fatalf("ToJSON on empty manifest: %v", err)
	}

	m2, err := ParseManifest(data)
	if err != nil {
		t.Fatalf("ParseManifest on empty manifest: %v", err)
	}
	if m2.ID != "" || m2.Name != "" {
		t.Errorf("expected empty fields, got id=%q name=%q", m2.ID, m2.Name)
	}
}

func TestAgentManifest_ParseInvalidJSON(t *testing.T) {
	_, err := ParseManifest([]byte("not json"))
	if err == nil {
		t.Error("ParseManifest should return error for invalid JSON")
	}
}
