package mcp

import (
	"context"
	"fmt"
	"sync"

	"github.com/rs/zerolog/log"
)

// ConnectionManager manages the lifecycle of MCP server connections.
type ConnectionManager struct {
	mu          sync.RWMutex
	connections map[string]*Connection
}

// NewConnectionManager creates a new connection manager.
func NewConnectionManager() *ConnectionManager {
	return &ConnectionManager{
		connections: make(map[string]*Connection),
	}
}

// Connect establishes a connection to an MCP server based on config.
func (m *ConnectionManager) Connect(ctx context.Context, cfg McpServerConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Disconnect existing connection if any.
	if existing, ok := m.connections[cfg.Name]; ok {
		_ = existing.Close()
		delete(m.connections, cfg.Name)
	}

	var conn *Connection
	var err error

	switch cfg.Transport {
	case "stdio":
		conn, err = ConnectStdio(ctx, cfg.Name, cfg.Command, cfg.Args, cfg.Env)
	case "sse":
		if cfg.URL == "" {
			return fmt.Errorf("mcp: SSE transport requires a URL")
		}
		conn, err = ConnectSSE(ctx, cfg.Name, cfg.URL)
	default:
		return fmt.Errorf("mcp: unsupported transport %q (supported: stdio, sse)", cfg.Transport)
	}

	if err != nil {
		return fmt.Errorf("mcp: connect %s: %w", cfg.Name, err)
	}

	log.Info().
		Str("server", cfg.Name).
		Int("tools", len(conn.Tools)).
		Str("server_name", conn.ServerName).
		Msg("MCP server connected")

	m.connections[cfg.Name] = conn
	return nil
}

// Disconnect terminates a connection by name.
func (m *ConnectionManager) Disconnect(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	conn, ok := m.connections[name]
	if !ok {
		return fmt.Errorf("mcp: server %q not connected", name)
	}

	err := conn.Close()
	delete(m.connections, name)
	return err
}

// Statuses returns the health of all connections.
func (m *ConnectionManager) Statuses() []ServerStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()

	statuses := make([]ServerStatus, 0, len(m.connections))
	for _, conn := range m.connections {
		statuses = append(statuses, ServerStatus{
			Name:          conn.Name,
			Connected:     true,
			ToolCount:     len(conn.Tools),
			ServerName:    conn.ServerName,
			ServerVersion: conn.ServerVersion,
		})
	}
	return statuses
}

// AllTools returns all tools from all connected servers.
func (m *ConnectionManager) AllTools() []ToolDescriptor {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var tools []ToolDescriptor
	for _, conn := range m.connections {
		tools = append(tools, conn.Tools...)
	}
	return tools
}

// CallTool dispatches a tool call to the appropriate server.
func (m *ConnectionManager) CallTool(ctx context.Context, serverName, toolName string, input []byte) (*ToolCallResult, error) {
	m.mu.RLock()
	conn, ok := m.connections[serverName]
	m.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("mcp: server %q not connected", serverName)
	}
	return conn.CallTool(ctx, toolName, input)
}

// Connection returns a snapshot of a named connection if present.
func (m *ConnectionManager) Connection(name string) (*Connection, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	conn, ok := m.connections[name]
	if !ok {
		return nil, false
	}

	copyConn := *conn
	copyConn.Tools = append([]ToolDescriptor(nil), conn.Tools...)
	return &copyConn, true
}

// CloseAll disconnects all servers.
func (m *ConnectionManager) CloseAll() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for name, conn := range m.connections {
		if err := conn.Close(); err != nil {
			log.Warn().Err(err).Str("server", name).Msg("error closing MCP connection")
		}
	}
	m.connections = make(map[string]*Connection)
}
