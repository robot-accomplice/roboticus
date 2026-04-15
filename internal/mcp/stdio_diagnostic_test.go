// stdio_diagnostic_test.go pins the v1.0.6 contract for
// MCP-release-blocker-checklist item 4: stdio startup failures must
// produce actionable diagnostics. Pre-v1.0.6 a child that crashed
// during initialization produced only "mcp: initialize failed: EOF"
// — operators had no way to identify the cause without re-running
// the child manually. This suite asserts that the captured-stderr +
// child-exit-state plumbing surfaces real diagnostic content in the
// error message.

package mcp

import (
	"context"
	"strings"
	"testing"
	"time"
)

// TestConnectStdio_FailureSurfacesChildStderr is the headline
// MCP-checklist-item-4 regression. We spawn a child that writes a
// recognizable error message to stderr then immediately exits.
// ConnectStdio should surface that stderr in the returned error so
// an operator can act on it without re-running anything.
//
// The child uses `sh -c` to combine stderr-write + exit so we don't
// have to ship a fixture binary. The stderr text "DEPENDENCY_NOT_FOUND
// fixture marker" is intentionally distinctive so the assertion can
// match it precisely without false positives from other test output.
func TestConnectStdio_FailureSurfacesChildStderr(t *testing.T) {
	if testing.Short() {
		t.Skip("subprocess test skipped under -short")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Child writes a marker to stderr, then exits non-zero. The
	// initialize handshake will fail with EOF on stdout (child closed
	// without responding); the diagnostic plumbing must surface the
	// stderr marker AND the non-zero exit.
	const stderrMarker = "DEPENDENCY_NOT_FOUND fixture marker"
	_, err := ConnectStdio(
		ctx,
		"test-broken-stdio",
		"sh",
		[]string{"-c", "echo '" + stderrMarker + "' >&2; exit 42"},
		nil,
	)
	if err == nil {
		t.Fatalf("expected ConnectStdio to fail; got nil")
	}
	msg := err.Error()

	// Pre-v1.0.6 contract: error contained only "initialize failed:
	// EOF" — that's still part of the message but must NOT be the
	// only thing.
	if !strings.Contains(msg, "initialize failed") {
		t.Fatalf("expected error to mention 'initialize failed'; got %v", msg)
	}

	// v1.0.6 contract: stderr marker MUST appear in the error so
	// operators see the real cause.
	if !strings.Contains(msg, stderrMarker) {
		t.Fatalf("expected error to surface child stderr marker %q for actionable diagnostics; got %v", stderrMarker, msg)
	}

	// v1.0.6 contract: child exit state MUST appear (either explicit
	// "exit status N" or "child exit:" prefix). This distinguishes
	// "child died" from "child still running but stdout closed."
	if !strings.Contains(msg, "child exit") {
		t.Fatalf("expected error to surface child exit state; got %v", msg)
	}
}

// TestStdioTransport_ChildDiagnosticEmptyOnSuccessfulRun verifies the
// no-op case: a long-lived child that hasn't exited and hasn't
// emitted stderr should produce a "child still running" diagnostic
// (not nothing, since "running" itself is meaningful state) but no
// stderr noise. This rules out spurious diagnostic content polluting
// successful operations.
func TestStdioTransport_ChildDiagnosticEmptyOnSuccessfulRun(t *testing.T) {
	if testing.Short() {
		t.Skip("subprocess test skipped under -short")
	}

	// Use `sleep 30` as a stand-in for a well-behaved child: alive
	// for the duration of the test, no stderr.
	transport, err := NewStdioTransport("sleep", []string{"30"}, nil)
	if err != nil {
		t.Fatalf("NewStdioTransport: %v", err)
	}
	defer func() { _ = transport.Close() }()

	// Give the goroutines a moment to settle (in case the test is
	// run on an under-loaded machine where collectStderr hasn't yet
	// done its first read).
	time.Sleep(100 * time.Millisecond)

	diag := transport.ChildDiagnostic()
	if !strings.Contains(diag, "still running") {
		t.Fatalf("expected diagnostic to indicate child still running; got %q", diag)
	}
	if strings.Contains(diag, "stderr:") {
		t.Fatalf("expected no stderr in diagnostic for clean child; got %q", diag)
	}
}

// TestStdioTransport_ChildDiagnosticHandlesLargeStderr verifies the
// bounded-buffer behavior: a child that floods stderr can't blow up
// the transport's memory. Most-recent stderrBufferLimit bytes are
// retained; the front is trimmed. The diagnostic must indicate
// truncation so operators know they're seeing only the tail.
func TestStdioTransport_ChildDiagnosticHandlesLargeStderr(t *testing.T) {
	if testing.Short() {
		t.Skip("subprocess test skipped under -short")
	}

	// Generate 32KB of stderr (4× the buffer limit) then exit. The
	// last bytes are a recognizable marker that MUST survive
	// truncation. The prologue ('A' bytes) MUST be dropped.
	const tailMarker = "TAIL_MARKER_OF_STDERR_FLOOD"
	cmd := "head -c 32768 /dev/zero | tr '\\0' 'A' >&2; printf '" + tailMarker + "' >&2; exit 1"

	transport, err := NewStdioTransport("sh", []string{"-c", cmd}, nil)
	if err != nil {
		t.Fatalf("NewStdioTransport: %v", err)
	}
	// Wait for the child to exit and the collector to drain.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		transport.waitMu.RLock()
		exited := transport.exited
		transport.waitMu.RUnlock()
		if exited {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	waitForStderrDrain(transport, 500*time.Millisecond)

	diag := transport.ChildDiagnostic()
	_ = transport.Close()

	if !strings.Contains(diag, tailMarker) {
		t.Fatalf("expected tail marker %q to survive truncation; got %q", tailMarker, diag)
	}
	if !strings.Contains(diag, "truncated") {
		t.Fatalf("expected diagnostic to indicate truncation when buffer was overflowed; got %q", diag)
	}
}
