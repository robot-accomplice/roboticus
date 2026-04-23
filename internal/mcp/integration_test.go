//go:build integration

// Integration tests for MCP client against real servers.
// Run with: go test -tags=integration ./internal/mcp/... -v
//
// Prerequisites:
// - Playwright MCP: npx @playwright/mcp (stdio)
// - Or any MCP server available via stdio/SSE transport
//
// These tests validate the full lifecycle: connect → tool discovery → call → response.

package mcp

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

// TestIntegration_StdioConnect connects to a real stdio MCP server.
// Set MCP_TEST_COMMAND to the server command (e.g., "npx @playwright/mcp").
func splitCommand(raw string) (string, []string) {
	parts := strings.Fields(raw)
	if len(parts) == 0 {
		return "", nil
	}
	return parts[0], parts[1:]
}

func TestIntegration_StdioConnect(t *testing.T) {
	raw := os.Getenv("MCP_TEST_COMMAND")
	if raw == "" {
		t.Skip("MCP_TEST_COMMAND not set — skipping stdio integration test")
	}
	cmd, args := splitCommand(raw)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	conn, err := ConnectStdio(ctx, "test-stdio", cmd, args, nil)
	if err != nil {
		t.Fatalf("ConnectStdio: %v", err)
	}
	defer func() { _ = conn.Close() }()

	t.Logf("Connected to %s (server: %s %s)", conn.Name, conn.ServerName, conn.ServerVersion)
	t.Logf("Discovered %d tools", len(conn.Tools))

	if len(conn.Tools) == 0 {
		t.Error("expected at least 1 tool from MCP server")
	}

	for _, tool := range conn.Tools {
		t.Logf("  Tool: %s — %s", tool.Name, tool.Description)
	}
}

// TestIntegration_SSEConnect connects to a real SSE MCP server.
// Set MCP_TEST_SSE_URL to the server URL (e.g., "http://localhost:3000/sse").
func TestIntegration_SSEConnect(t *testing.T) {
	url := os.Getenv("MCP_TEST_SSE_URL")
	if url == "" {
		t.Skip("MCP_TEST_SSE_URL not set — skipping SSE integration test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	conn, err := ConnectSSE(ctx, "test-sse", url)
	if err != nil {
		t.Fatalf("ConnectSSE: %v", err)
	}
	defer func() { _ = conn.Close() }()

	t.Logf("Connected to %s (server: %s %s)", conn.Name, conn.ServerName, conn.ServerVersion)
	t.Logf("Discovered %d tools", len(conn.Tools))

	if len(conn.Tools) == 0 {
		t.Error("expected at least 1 tool from MCP server")
	}
}

// TestIntegration_ToolCall executes a tool call against a real server.
// Uses the first discovered tool with empty parameters.
func TestIntegration_ToolCall(t *testing.T) {
	raw := os.Getenv("MCP_TEST_COMMAND")
	if raw == "" {
		t.Skip("MCP_TEST_COMMAND not set — skipping tool call integration test")
	}
	cmd, args := splitCommand(raw)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	conn, err := ConnectStdio(ctx, "test-call", cmd, args, nil)
	if err != nil {
		t.Fatalf("ConnectStdio: %v", err)
	}
	defer func() { _ = conn.Close() }()

	if len(conn.Tools) == 0 {
		t.Skip("no tools discovered — skipping tool call test")
	}

	// Call the first tool with empty args (may fail, but should not panic).
	tool := conn.Tools[0]
	t.Logf("Calling tool: %s", tool.Name)

	result, err := conn.CallTool(ctx, tool.Name, nil)
	if err != nil {
		t.Logf("Tool call returned error (expected for empty args): %v", err)
	} else {
		t.Logf("Tool call result: %+v", result)
	}
}

// TestIntegration_ConnectionManager validates the full manager lifecycle.
func TestIntegration_ConnectionManager(t *testing.T) {
	raw := os.Getenv("MCP_TEST_COMMAND")
	if raw == "" {
		t.Skip("MCP_TEST_COMMAND not set")
	}
	cmd, args := splitCommand(raw)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	mgr := NewConnectionManager()

	err := mgr.Connect(ctx, McpServerConfig{
		Name:      "integration-test",
		Transport: "stdio",
		Command:   cmd,
		Args:      args,
	})
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}

	tools := mgr.AllTools()
	t.Logf("Manager discovered %d tools for integration-test", len(tools))

	if len(tools) == 0 {
		t.Error("expected tools from managed connection")
	}

	// Disconnect.
	_ = mgr.Disconnect("integration-test")
	tools = mgr.AllTools()
	if len(tools) != 0 {
		t.Error("expected 0 tools after disconnect")
	}
}
