package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"sync"
	"sync/atomic"
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

// jsonRPCRequest is a JSON-RPC 2.0 request.
type jsonRPCRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int64  `json:"id"`
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

// StdioTransport communicates with an MCP server via subprocess stdin/stdout.
type StdioTransport struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Reader
	mu     sync.Mutex
}

// NewStdioTransport spawns a subprocess and connects via JSON-RPC over stdio.
func NewStdioTransport(command string, args []string, env map[string]string) (*StdioTransport, error) {
	cmd := exec.Command(command, args...)
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

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("mcp stdio: start: %w", err)
	}

	return &StdioTransport{
		cmd:    cmd,
		stdin:  stdin,
		stdout: bufio.NewReader(stdout),
	}, nil
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
func ConnectStdio(ctx context.Context, name, command string, args []string, env map[string]string) (*Connection, error) {
	transport, err := NewStdioTransport(command, args, env)
	if err != nil {
		return nil, err
	}

	conn := &Connection{
		Name:      name,
		transport: transport,
	}

	// Initialize: send "initialize" request.
	if err := conn.initialize(ctx); err != nil {
		_ = transport.Close()
		return nil, fmt.Errorf("mcp: initialize failed: %w", err)
	}

	// List tools.
	if err := conn.listTools(ctx); err != nil {
		_ = transport.Close()
		return nil, fmt.Errorf("mcp: list tools failed: %w", err)
	}

	return conn, nil
}

var nextID atomic.Int64

func (c *Connection) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	id := nextID.Add(1)
	req := jsonRPCRequest{JSONRPC: "2.0", ID: id, Method: method, Params: params}
	data, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	if err := c.transport.Send(ctx, data); err != nil {
		return nil, err
	}
	respData, err := c.transport.Receive(ctx)
	if err != nil {
		return nil, err
	}
	var resp jsonRPCResponse
	if err := json.Unmarshal(respData, &resp); err != nil {
		return nil, fmt.Errorf("mcp: invalid response: %w", err)
	}
	if resp.Error != nil {
		return nil, fmt.Errorf("mcp rpc error %d: %s", resp.Error.Code, resp.Error.Message)
	}
	return resp.Result, nil
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

	// Send initialized notification.
	notif := jsonRPCRequest{JSONRPC: "2.0", Method: "notifications/initialized"}
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
