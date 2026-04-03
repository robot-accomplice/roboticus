package security

import "os/exec"

// SandboxConfig holds OS-level process confinement settings.
type SandboxConfig struct {
	Enabled        bool     `json:"enabled" mapstructure:"enabled"`
	MaxMemoryBytes int64    `json:"max_memory_bytes" mapstructure:"max_memory_bytes"` // 0 = unlimited
	AllowedPaths   []string `json:"allowed_paths" mapstructure:"allowed_paths"`       // filesystem write allowlist
	WorkspaceDir   string   `json:"workspace_dir" mapstructure:"workspace_dir"`
}

// Sandbox applies OS-level process confinement to a child command.
// Implementations are platform-specific via build tags.
type Sandbox interface {
	// Apply configures the command for sandboxed execution.
	// Must be called before cmd.Start().
	Apply(cmd *exec.Cmd) error

	// Available returns true if the sandbox mechanism is supported on this OS.
	Available() bool
}

// NewSandbox creates a platform-appropriate sandbox.
// Returns a no-op sandbox on unsupported platforms.
func NewSandbox(cfg SandboxConfig) Sandbox {
	if !cfg.Enabled {
		return &noopSandbox{}
	}
	return newPlatformSandbox(cfg)
}

// noopSandbox is a no-op sandbox for when sandboxing is disabled.
type noopSandbox struct{}

func (s *noopSandbox) Apply(_ *exec.Cmd) error { return nil }
func (s *noopSandbox) Available() bool         { return false }
