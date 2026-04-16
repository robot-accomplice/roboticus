package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Transport abstracts the JSON-RPC communication layer for MCP.
type Transport interface {
	Send(ctx context.Context, msg json.RawMessage) error
	Receive(ctx context.Context) (json.RawMessage, error)
	Close() error
}

// Connection represents an active MCP server connection.
type Connection struct {
	Name          string
	Tools         []ToolDescriptor
	ServerName    string
	ServerVersion string
	transport     Transport
}

// jsonRPCRequest is a JSON-RPC 2.0 request (with mandatory id).
type jsonRPCRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int64  `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

// jsonRPCNotification is a JSON-RPC 2.0 notification (no id field).
// MCP spec: notifications MUST NOT include an "id" member.
type jsonRPCNotification struct {
	JSONRPC string `json:"jsonrpc"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

// jsonRPCResponse is a JSON-RPC 2.0 response.
type jsonRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *jsonRPCError   `json:"error,omitempty"`
}

type jsonRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// stderrBufferLimit is the maximum number of bytes we retain from a
// child MCP server's stderr stream. Cap exists so a chatty child can't
// blow up our memory; the most-recent N bytes are what's actionable
// for diagnosing a startup failure (typically a Python traceback or
// Node error message), so we keep the tail rather than the head.
const stderrBufferLimit = 8 * 1024

// StdioTransport communicates with an MCP server via subprocess stdin/stdout.
//
// v1.0.6: stderr is now captured into a bounded ring-style buffer
// (most-recent stderrBufferLimit bytes retained). The pre-v1.0.6
// implementation discarded child stderr entirely, which produced the
// notorious "mcp: initialize failed: EOF" symptom from the MCP release-
// blocker checklist (item 4): when the child crashed during startup
// the parent saw only EOF on stdout with zero context about the
// underlying cause. ChildDiagnostic() exposes the captured stderr +
// observed exit state so wrapping callers (ConnectStdio, the operator
// surface) can produce actionable error messages.
type StdioTransport struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Reader
	mu     sync.Mutex

	// stderr capture state. Guarded by stderrMu (separate from mu so
	// stderr collection doesn't contend with stdin writes). The
	// buffer is bounded — bytes past stderrBufferLimit are dropped
	// from the FRONT (we keep the most recent stderr because that's
	// what's diagnostic at the moment of failure, not the
	// prologue).
	stderrMu  sync.Mutex
	stderrBuf []byte

	// Child exit state. waitErr captures the result of cmd.Wait()
	// so failure paths can include "exit status N" alongside the
	// captured stderr. exited becomes true once Wait has returned.
	waitMu  sync.RWMutex
	exited  bool
	waitErr error
}

// NewStdioTransport spawns a subprocess and connects via JSON-RPC over stdio.
func NewStdioTransport(command string, args []string, env map[string]string) (*StdioTransport, error) {
	cmd := exec.Command(command, args...)
	cmd.Env = os.Environ()
	for k, v := range env {
		cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, v))
	}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("mcp stdio: stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("mcp stdio: stdout pipe: %w", err)
	}
	// v1.0.6: capture stderr instead of letting the child write into
	// the parent's terminal (or worse, /dev/null when the parent is a
	// daemon). Without this pipe, every "initialize failed: EOF"
	// reported back to the operator hides the actual reason — Python
	// traceback, missing dependency, version mismatch, etc.
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("mcp stdio: stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("mcp stdio: start: %w", err)
	}

	t := &StdioTransport{
		cmd:    cmd,
		stdin:  stdin,
		stdout: bufio.NewReader(stdout),
	}

	// Start the stderr-collection goroutine. Reads until the pipe
	// closes (which happens when the child exits or its stderr is
	// closed). The goroutine takes the stderr lock only when writing,
	// so reads from ChildDiagnostic don't block stderr collection.
	go t.collectStderr(stderrPipe)

	// Start a watcher that calls cmd.Wait() so the child gets reaped
	// promptly and we can record its exit state. Without the Wait,
	// short-lived crashed children become zombies in the daemon
	// process and we never observe their exit code.
	go t.watchExit()

	return t, nil
}

// collectStderr reads child stderr into the bounded buffer. Runs in
// its own goroutine for the life of the child process. Returns when
// the stderr pipe closes (child exit, explicit close, or pipe error).
func (t *StdioTransport) collectStderr(pipe io.ReadCloser) {
	defer func() { _ = pipe.Close() }()
	chunk := make([]byte, 4096)
	for {
		n, err := pipe.Read(chunk)
		if n > 0 {
			t.appendStderr(chunk[:n])
		}
		if err != nil {
			return
		}
	}
}

// appendStderr adds bytes to the bounded buffer, trimming the front
// when over capacity so the most-recent stderr (the part that's
// diagnostic at failure time) is preserved.
func (t *StdioTransport) appendStderr(b []byte) {
	t.stderrMu.Lock()
	defer t.stderrMu.Unlock()
	t.stderrBuf = append(t.stderrBuf, b...)
	if overflow := len(t.stderrBuf) - stderrBufferLimit; overflow > 0 {
		t.stderrBuf = t.stderrBuf[overflow:]
	}
}

// watchExit calls cmd.Wait() so the child is reaped and we can record
// its exit state. Stash the wait error (which carries exit code) so
// ChildDiagnostic can surface it alongside the captured stderr.
func (t *StdioTransport) watchExit() {
	err := t.cmd.Wait()
	t.waitMu.Lock()
	t.waitErr = err
	t.exited = true
	t.waitMu.Unlock()
}

// ChildDiagnostic returns a human-readable summary of the child's
// observed state — captured stderr (truncated indicator if we hit
// the buffer cap) plus exit status if the child has been reaped.
// Empty string when nothing useful is available (still alive, no
// stderr, no exit observed).
//
// Used by ConnectStdio and operator-facing error wrappers to turn
// the legacy "initialize failed: EOF" into actionable messages
// like:
//
//	mcp: initialize failed: EOF (child exit status 127; stderr:
//	  "Error: Cannot find module 'mcp-server-foo'")
//
// Operators can act on that without re-running anything.
func (t *StdioTransport) ChildDiagnostic() string {
	t.stderrMu.Lock()
	stderrSnap := append([]byte(nil), t.stderrBuf...)
	t.stderrMu.Unlock()

	t.waitMu.RLock()
	exited := t.exited
	waitErr := t.waitErr
	t.waitMu.RUnlock()

	parts := []string{}
	if exited {
		if waitErr != nil {
			parts = append(parts, fmt.Sprintf("child exit: %v", waitErr))
		} else {
			parts = append(parts, "child exit: 0")
		}
	} else {
		// Child still running — useful to note explicitly so
		// operators don't conflate "EOF on stdout" with
		// "process died."
		parts = append(parts, "child still running")
	}

	if len(stderrSnap) > 0 {
		// Trim trailing whitespace for cleaner one-line wrapping.
		stderrText := string(stderrSnap)
		for len(stderrText) > 0 && (stderrText[len(stderrText)-1] == '\n' || stderrText[len(stderrText)-1] == ' ' || stderrText[len(stderrText)-1] == '\t') {
			stderrText = stderrText[:len(stderrText)-1]
		}
		hit := ""
		if len(stderrSnap) >= stderrBufferLimit {
			hit = " (truncated to last " + fmt.Sprintf("%d", stderrBufferLimit) + " bytes)"
		}
		parts = append(parts, fmt.Sprintf("stderr%s: %q", hit, stderrText))
	}

	return strings.Join(parts, "; ")
}

func (t *StdioTransport) Send(_ context.Context, msg json.RawMessage) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	line := append(msg, '\n')
	_, err := t.stdin.Write(line)
	return err
}

func (t *StdioTransport) Receive(_ context.Context) (json.RawMessage, error) {
	line, err := t.stdout.ReadBytes('\n')
	if err != nil {
		return nil, err
	}
	return json.RawMessage(line), nil
}

func (t *StdioTransport) Close() error {
	_ = t.stdin.Close()
	return t.cmd.Process.Kill()
}

// ConnectStdio connects to an MCP server via subprocess stdio.
//
// v1.0.6: the initialize / list-tools error paths now include the
// child's captured stderr and exit state via
// transport.ChildDiagnostic(). Pre-v1.0.6 these failures returned
// only "initialize failed: EOF" (or similar) which forced operators
// to re-run the child manually under strace / dtruss to see what
// actually went wrong. With the diagnostic appended, a missing
// dependency or version mismatch is visible in the original error.
func ConnectStdio(ctx context.Context, name, command string, args []string, env map[string]string) (*Connection, error) {
	transport, err := NewStdioTransport(command, args, env)
	if err != nil {
		return nil, err
	}

	conn := &Connection{
		Name:      name,
		transport: transport,
	}

	// Initialize: send "initialize" request. On failure, give the
	// child a brief moment to flush any final stderr (collectStderr
	// runs in a goroutine; without this pause we sometimes report
	// EOF before the child's death-throes stderr has been read).
	// Then surface child-diagnostic alongside the raw error.
	if err := conn.initialize(ctx); err != nil {
		_ = transport.Close()
		// transport is concretely *StdioTransport from
		// NewStdioTransport above — no interface assertion needed.
		// Wait briefly for stderr collector to drain post-close, then
		// fold the captured stderr + child exit state into the error.
		waitForStderrDrain(transport, 250*time.Millisecond)
		if diag := transport.ChildDiagnostic(); diag != "" {
			return nil, fmt.Errorf("mcp: initialize failed: %w (%s)", err, diag)
		}
		return nil, fmt.Errorf("mcp: initialize failed: %w", err)
	}

	// List tools.
	if err := conn.listTools(ctx); err != nil {
		_ = transport.Close()
		waitForStderrDrain(transport, 250*time.Millisecond)
		if diag := transport.ChildDiagnostic(); diag != "" {
			return nil, fmt.Errorf("mcp: list tools failed: %w (%s)", err, diag)
		}
		return nil, fmt.Errorf("mcp: list tools failed: %w", err)
	}

	return conn, nil
}

// waitForStderrDrain pauses briefly to let the stderr-collection
// goroutine catch up after the child process has been killed/closed.
// Without this small wait, the diagnostic returned by
// ChildDiagnostic() can be empty even when the child wrote real
// error text just before exiting — collectStderr's Read returns EOF
// immediately on Close but the bytes already in the pipe buffer may
// not have been appended yet.
//
// Polls until stderr buffer stops growing OR timeout elapses,
// whichever comes first. The poll interval is short (10ms) so a
// quickly-flushing child doesn't pay the full timeout.
func waitForStderrDrain(t *StdioTransport, timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	prevLen := -1
	for time.Now().Before(deadline) {
		t.stderrMu.Lock()
		curLen := len(t.stderrBuf)
		t.stderrMu.Unlock()
		if curLen == prevLen && curLen > 0 {
			return // stable
		}
		prevLen = curLen
		time.Sleep(10 * time.Millisecond)
	}
}

var nextID atomic.Int64

func (c *Connection) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	id := nextID.Add(1)
	req := jsonRPCRequest{JSONRPC: "2.0", ID: id, Method: method, Params: params}
	data, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	type callResult struct {
		result json.RawMessage
		err    error
	}
	done := make(chan callResult, 1)
	go func() {
		if err := c.transport.Send(ctx, data); err != nil {
			done <- callResult{err: err}
			return
		}
		respData, err := c.transport.Receive(ctx)
		if err != nil {
			done <- callResult{err: err}
			return
		}
		var resp jsonRPCResponse
		if err := json.Unmarshal(respData, &resp); err != nil {
			done <- callResult{err: fmt.Errorf("mcp: invalid response: %w", err)}
			return
		}
		if resp.Error != nil {
			done <- callResult{err: fmt.Errorf("mcp rpc error %d: %s", resp.Error.Code, resp.Error.Message)}
			return
		}
		done <- callResult{result: resp.Result}
	}()

	select {
	case <-ctx.Done():
		_ = c.Close()
		return nil, ctx.Err()
	case res := <-done:
		return res.result, res.err
	}
}

func (c *Connection) initialize(ctx context.Context) error {
	result, err := c.call(ctx, "initialize", map[string]any{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]any{},
		"clientInfo": map[string]string{
			"name":    "roboticus",
			"version": "0.1.0",
		},
	})
	if err != nil {
		return err
	}

	var info struct {
		ServerInfo struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		} `json:"serverInfo"`
	}
	if json.Unmarshal(result, &info) == nil {
		c.ServerName = info.ServerInfo.Name
		c.ServerVersion = info.ServerInfo.Version
	}

	// Send initialized notification (no id — MCP spec requirement).
	notif := jsonRPCNotification{JSONRPC: "2.0", Method: "notifications/initialized"}
	data, _ := json.Marshal(notif)
	_ = c.transport.Send(ctx, data)

	return nil
}

func (c *Connection) listTools(ctx context.Context) error {
	result, err := c.call(ctx, "tools/list", nil)
	if err != nil {
		return err
	}

	var resp struct {
		Tools []struct {
			Name        string          `json:"name"`
			Description string          `json:"description"`
			InputSchema json.RawMessage `json:"inputSchema"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(result, &resp); err != nil {
		return err
	}

	c.Tools = make([]ToolDescriptor, len(resp.Tools))
	for i, t := range resp.Tools {
		c.Tools[i] = ToolDescriptor{
			Name:        t.Name,
			Description: t.Description,
			InputSchema: t.InputSchema,
		}
	}
	return nil
}

// RefreshTools re-discovers tools from the connected MCP server.
func (c *Connection) RefreshTools(ctx context.Context) error {
	return c.listTools(ctx)
}

// CallTool invokes a tool on the connected MCP server.
func (c *Connection) CallTool(ctx context.Context, name string, input json.RawMessage) (*ToolCallResult, error) {
	result, err := c.call(ctx, "tools/call", map[string]any{
		"name":      name,
		"arguments": input,
	})
	if err != nil {
		return nil, err
	}

	var resp struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	if err := json.Unmarshal(result, &resp); err != nil {
		return &ToolCallResult{Content: string(result)}, nil
	}

	var text string
	for _, c := range resp.Content {
		if c.Type == "text" {
			text += c.Text
		}
	}
	return &ToolCallResult{Content: text, IsError: resp.IsError}, nil
}

// Close terminates the connection.
func (c *Connection) Close() error {
	if c.transport != nil {
		return c.transport.Close()
	}
	return nil
}
