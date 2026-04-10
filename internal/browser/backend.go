// Browser backend abstraction.
//
// Enables pluggable browser backends — the native CDP implementation and
// external CLI tools (e.g., agent-browser) both implement this interface.
//
// The interface operates at the BrowserAction level: callers dispatch actions
// without knowing whether they're executed via CDP WebSocket, an external
// process, or a mock.
//
// Ported from Rust: crates/roboticus-browser/src/backend.rs

package browser

import "context"

// Backend is a pluggable browser backend that can execute BrowserAction variants.
//
// Implementations:
//   - CdpBackend — native Chrome DevTools Protocol (existing Browser behavior)
//   - AgentBrowserBackend — external agent-browser CLI with --json mode
type Backend interface {
	// Execute performs a browser action and returns the result.
	Execute(ctx context.Context, action *BrowserAction) ActionResult

	// Name returns a human-readable backend name for provenance/logging.
	Name() string

	// IsAvailable reports whether the backend is currently available.
	IsAvailable() bool
}

// CdpBackend wraps the existing Browser as a Backend implementation.
type CdpBackend struct {
	browser *Browser
}

// NewCdpBackend creates a Backend that delegates to the native CDP Browser.
func NewCdpBackend(b *Browser) *CdpBackend {
	return &CdpBackend{browser: b}
}

func (cb *CdpBackend) Execute(ctx context.Context, action *BrowserAction) ActionResult {
	return cb.browser.Execute(ctx, action)
}

func (cb *CdpBackend) Name() string { return "cdp" }

func (cb *CdpBackend) IsAvailable() bool { return cb.browser.IsRunning() }
