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

func TestNewSandbox_Enabled(t *testing.T) {
	s := NewSandbox(SandboxConfig{
		Enabled:        true,
		MaxMemoryBytes: 1 << 30,
		AllowedPaths:   []string{"/tmp"},
		WorkspaceDir:   "/workspace",
	})
	// On non-Linux/non-Windows (macOS, etc.) the platform sandbox is a noop,
	// so Available() returns false. On Linux it would attempt Landlock.
	// Either way, NewSandbox must return a non-nil Sandbox.
	if s == nil {
		t.Fatal("NewSandbox with Enabled=true must not return nil")
	}
}

func TestNewSandbox_EnabledApply(t *testing.T) {
	s := NewSandbox(SandboxConfig{
		Enabled:      true,
		WorkspaceDir: t.TempDir(),
	})
	cmd := exec.Command("echo", "sandbox-test")
	if err := s.Apply(cmd); err != nil {
		t.Fatalf("Apply on enabled sandbox should not error on this platform: %v", err)
	}
}

func TestNewPlatformSandbox(t *testing.T) {
	// Directly exercise the platform-specific constructor.
	s := newPlatformSandbox(SandboxConfig{
		Enabled:      true,
		WorkspaceDir: "/tmp",
	})
	if s == nil {
		t.Fatal("newPlatformSandbox must not return nil")
	}
	// On macOS/other, this returns a noopSandbox.
	cmd := exec.Command("true")
	if err := s.Apply(cmd); err != nil {
		t.Fatalf("platform sandbox Apply should not error: %v", err)
	}
}

func TestSandboxInterface(t *testing.T) {
	// Verify both enabled and disabled sandboxes satisfy the interface
	// and behave consistently with Apply/Available.
	cases := []struct {
		name    string
		cfg     SandboxConfig
		wantNil bool
	}{
		{"disabled", SandboxConfig{Enabled: false}, false},
		{"enabled_no_paths", SandboxConfig{Enabled: true}, false},
		{"enabled_with_config", SandboxConfig{
			Enabled:        true,
			MaxMemoryBytes: 512 * 1024 * 1024,
			AllowedPaths:   []string{"/tmp", "/var/data"},
			WorkspaceDir:   "/home/user/project",
		}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := NewSandbox(tc.cfg)
			if s == nil {
				t.Fatal("sandbox must not be nil")
			}
			cmd := exec.Command("echo", "test")
			if err := s.Apply(cmd); err != nil {
				t.Errorf("Apply error: %v", err)
			}
		})
	}
}

func TestSandboxConfig_ZeroValue(t *testing.T) {
	// Zero-value config should produce a disabled (noop) sandbox.
	var cfg SandboxConfig
	s := NewSandbox(cfg)
	if s.Available() {
		t.Error("zero-value config sandbox should not be available")
	}
	if err := s.Apply(exec.Command("true")); err != nil {
		t.Errorf("zero-value sandbox Apply should not error: %v", err)
	}
}
