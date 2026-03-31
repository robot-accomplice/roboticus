//go:build !windows && !linux

package security

// On unsupported platforms, sandboxing is a no-op.
func newPlatformSandbox(cfg SandboxConfig) Sandbox {
	return &noopSandbox{}
}
