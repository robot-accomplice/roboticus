package security

import (
	"os/exec"
	"testing"
)

func TestNewSandbox_Disabled(t *testing.T) {
	s := NewSandbox(SandboxConfig{Enabled: false})
	if s.Available() {
		t.Error("disabled sandbox should not be available")
	}
}

func TestNewSandbox_NoopApply(t *testing.T) {
	s := NewSandbox(SandboxConfig{Enabled: false})
	cmd := exec.Command("echo", "hello")
	if err := s.Apply(cmd); err != nil {
		t.Errorf("noop apply should not error: %v", err)
	}
}

func TestSandboxConfig_Defaults(t *testing.T) {
	cfg := SandboxConfig{
		Enabled:      true,
		AllowedPaths: []string{"/tmp", "/workspace"},
	}
	if !cfg.Enabled {
		t.Error("should be enabled")
	}
	if len(cfg.AllowedPaths) != 2 {
		t.Errorf("allowed paths = %d", len(cfg.AllowedPaths))
	}
}
