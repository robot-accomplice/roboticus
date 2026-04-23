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
	"bytes"
	"context"
	"io"
	"os"
	"strings"
	"testing"
	"time"
)

const (
	mcpHelperProcessEnv = "ROBOTICUS_MCP_HELPER_PROCESS"
	mcpHelperModeEnv    = "ROBOTICUS_MCP_HELPER_MODE"
)

func mcpHelperCommand(t *testing.T, mode string) (string, []string, map[string]string) {
	t.Helper()
	return os.Args[0], []string{"-test.run=TestMCPHelperProcess"}, map[string]string{
		mcpHelperProcessEnv: "1",
		mcpHelperModeEnv:    mode,
	}
}

func TestMCPHelperProcess(t *testing.T) {
	if os.Getenv(mcpHelperProcessEnv) != "1" {
		return
	}

	switch os.Getenv(mcpHelperModeEnv) {
	case "broken-stderr-exit42":
		_ = writeAll(os.Stderr, []byte("DEPENDENCY_NOT_FOUND fixture marker\n"))
		os.Exit(42)
	case "stderr-flood-exit1":
		payload := append(bytes.Repeat([]byte("A"), 32768), []byte("TAIL_MARKER_OF_STDERR_FLOOD")...)
		_ = writeAll(os.Stderr, payload)
		os.Exit(1)
	case "clean-sleep", "hung-initialize":
		time.Sleep(30 * time.Second)
		os.Exit(0)
	default:
		os.Exit(2)
	}
}

func writeAll(w io.Writer, b []byte) error {
	for len(b) > 0 {
		n, err := w.Write(b)
		if err != nil {
			return err
		}
		b = b[n:]
	}
	return nil
}

// TestConnectStdio_FailureSurfacesChildStderr is the headline
// MCP-checklist-item-4 regression. We spawn a child that writes a
// recognizable error message to stderr then immediately exits.
// ConnectStdio should surface that stderr in the returned error so
// an operator can act on it without re-running anything.
//
// The child uses the current test binary as an in-process helper so
// the fixture is deterministic across CI runners instead of depending
// on shell/pipeline semantics.
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
	command, args, env := mcpHelperCommand(t, "broken-stderr-exit42")
	_, err := ConnectStdio(
		ctx,
		"test-broken-stdio",
		command,
		args,
		env,
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
	command, args, env := mcpHelperCommand(t, "clean-sleep")
	transport, err := NewStdioTransport(command, args, env)
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
	command, args, env := mcpHelperCommand(t, "stderr-flood-exit1")
	transport, err := NewStdioTransport(command, args, env)
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

// TestNewStdioTransport_InheritsParentEnvironment verifies that stdio MCP
// children inherit the parent process environment even when cfg.Env provides
// overrides. Pre-fix, setting any env override replaced the entire child
// environment, which broke PATH/HOME-sensitive MCP servers.
func TestNewStdioTransport_InheritsParentEnvironment(t *testing.T) {
	if testing.Short() {
		t.Skip("subprocess test skipped under -short")
	}
	t.Setenv("MCP_PARENT_FIXTURE", "parent-value")

	transport, err := NewStdioTransport("sleep", []string{"30"}, map[string]string{
		"MCP_CHILD_FIXTURE": "child-value",
	})
	if err != nil {
		t.Fatalf("NewStdioTransport: %v", err)
	}
	defer func() { _ = transport.Close() }()

	envJoined := strings.Join(transport.cmd.Env, "\n")
	if !strings.Contains(envJoined, "MCP_PARENT_FIXTURE=parent-value") {
		t.Fatalf("expected parent environment to be preserved, got %q", envJoined)
	}
	if !strings.Contains(envJoined, "MCP_CHILD_FIXTURE=child-value") {
		t.Fatalf("expected override environment to be appended, got %q", envJoined)
	}
}

// TestConnectStdio_ContextDeadlineCancelsHungInitialize verifies that a hung
// stdio child no longer outlives the caller's timeout. Pre-fix, Receive()
// blocked forever on ReadBytes('\n') and ConnectStdio ignored the context.
func TestConnectStdio_ContextDeadlineCancelsHungInitialize(t *testing.T) {
	if testing.Short() {
		t.Skip("subprocess test skipped under -short")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	start := time.Now()
	command, args, env := mcpHelperCommand(t, "hung-initialize")
	_, err := ConnectStdio(ctx, "test-hung-stdio", command, args, env)
	if err == nil {
		t.Fatal("expected ConnectStdio to fail on context deadline")
	}
	if !strings.Contains(err.Error(), context.DeadlineExceeded.Error()) {
		t.Fatalf("expected context deadline exceeded, got %v", err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("ConnectStdio should return promptly after ctx timeout; took %v", elapsed)
	}
}
