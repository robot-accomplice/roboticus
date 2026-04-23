package mcp

import (
	"context"
	"fmt"
	"sort"
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
		conn, err = ConnectSSEWithConfig(ctx, cfg)
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

// ConnectAll connects to all enabled MCP servers from the config list.
// Non-fatal: individual server failures are logged as warnings and do not
// block startup. Returns the count of successfully connected servers.
// Matches Rust's connect_all() with per-server error isolation.
func (m *ConnectionManager) ConnectAll(ctx context.Context, servers []McpServerConfig) int {
	connected := 0
	for _, srv := range servers {
		if !srv.Enabled {
			log.Debug().Str("server", srv.Name).Msg("MCP server disabled, skipping")
			continue
		}
		if err := m.Connect(ctx, srv); err != nil {
			log.Warn().Err(err).Str("server", srv.Name).Msg("MCP server connection failed, continuing without it")
			continue
		}
		connected++
	}
	return connected
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
	for _, conn := range orderedConnections(m.connections) {
		status := ServerStatus{
			Name:          conn.Name,
			Connected:     true,
			ToolCount:     len(conn.Tools),
			ServerName:    conn.ServerName,
			ServerVersion: conn.ServerVersion,
		}
		if err := conn.receiverErr(); err != nil {
			status.Connected = false
			status.ToolCount = 0
			status.Error = err.Error()
		}
		statuses = append(statuses, status)
	}
	return statuses
}

// AllTools returns all tools from all connected servers.
func (m *ConnectionManager) AllTools() []ToolDescriptor {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var tools []ToolDescriptor
	for _, conn := range orderedConnections(m.connections) {
		if conn.receiverErr() != nil {
			continue
		}
		tools = append(tools, conn.Tools...)
	}
	return tools
}

// ConnectedCount returns the number of live MCP connections whose transport is
// still healthy enough to serve tool calls.
func (m *ConnectionManager) ConnectedCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()

	count := 0
	for _, conn := range m.connections {
		if conn.receiverErr() == nil {
			count++
		}
	}
	return count
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

// RefreshTools re-discovers tools for a connected server and updates the live
// connection held by the manager.
func (m *ConnectionManager) RefreshTools(ctx context.Context, name string) ([]ToolDescriptor, error) {
	m.mu.RLock()
	conn, ok := m.connections[name]
	m.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("mcp: server %q not connected", name)
	}
	if err := conn.RefreshTools(ctx); err != nil {
		return nil, err
	}
	return append([]ToolDescriptor(nil), conn.Tools...), nil
}

// Connection returns a snapshot of a named connection if present.
func (m *ConnectionManager) Connection(name string) (*Connection, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	conn, ok := m.connections[name]
	if !ok {
		return nil, false
	}

	return &Connection{
		Name:          conn.Name,
		Tools:         append([]ToolDescriptor(nil), conn.Tools...),
		ServerName:    conn.ServerName,
		ServerVersion: conn.ServerVersion,
	}, true
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

func orderedConnections(connections map[string]*Connection) []*Connection {
	names := make([]string, 0, len(connections))
	for name := range connections {
		names = append(names, name)
	}
	sort.Strings(names)
	ordered := make([]*Connection, 0, len(names))
	for _, name := range names {
		ordered = append(ordered, connections[name])
	}
	return ordered
}
