package routes

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"

	"roboticus/internal/core"
	"roboticus/internal/mcp"
)

type MCPToolSurface interface {
	SyncMCPToolSurface(context.Context, *mcp.ConnectionManager)
}

// ListMCPConnections returns all MCP server connection statuses.
func ListMCPConnections(mgr *mcp.ConnectionManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if mgr == nil {
			writeJSON(w, http.StatusOK, map[string]any{"connections": []any{}})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"connections": mgr.Statuses()})
	}
}

// ListMCPTools returns all tools from all connected MCP servers.
func ListMCPTools(mgr *mcp.ConnectionManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if mgr == nil {
			writeJSON(w, http.StatusOK, map[string]any{"tools": []any{}})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"tools": mgr.AllTools()})
	}
}

// ConnectMCPServer connects to an MCP server by config.
func ConnectMCPServer(mgr *mcp.ConnectionManager, surface MCPToolSurface) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if mgr == nil {
			writeError(w, http.StatusServiceUnavailable, "MCP not configured")
			return
		}
		var cfg mcp.McpServerConfig
		if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON")
			return
		}
		if err := mgr.Connect(r.Context(), cfg); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		syncMCPToolSurface(r.Context(), surface, mgr)
		writeJSON(w, http.StatusOK, map[string]string{"status": "connected"})
	}
}

// DisconnectMCPServer disconnects an MCP server by name.
func DisconnectMCPServer(mgr *mcp.ConnectionManager, surface MCPToolSurface) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if mgr == nil {
			writeError(w, http.StatusServiceUnavailable, "MCP not configured")
			return
		}
		name := chi.URLParam(r, "name")
		if err := mgr.Disconnect(name); err != nil {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		syncMCPToolSurface(r.Context(), surface, mgr)
		writeJSON(w, http.StatusOK, map[string]string{"status": "disconnected"})
	}
}

// DiscoverMCPTools triggers tool discovery on a connected MCP client by name.
func DiscoverMCPTools(mgr *mcp.ConnectionManager, surface MCPToolSurface) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if mgr == nil {
			writeError(w, http.StatusServiceUnavailable, "MCP not configured")
			return
		}
		name := chi.URLParam(r, "name")
		if _, ok := mgr.Connection(name); !ok {
			writeError(w, http.StatusNotFound, fmt.Sprintf("MCP client %q not connected", name))
			return
		}
		tools, err := mgr.RefreshTools(r.Context(), name)
		if err != nil {
			writeError(w, http.StatusInternalServerError, fmt.Sprintf("tool discovery failed: %v", err))
			return
		}
		syncMCPToolSurface(r.Context(), surface, mgr)
		toolRows := make([]map[string]any, 0, len(tools))
		for _, t := range tools {
			toolRows = append(toolRows, map[string]any{
				"name":        t.Name,
				"description": t.Description,
			})
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"name":       name,
			"tools":      toolRows,
			"tool_count": len(toolRows),
		})
	}
}

// DisconnectMCPClient disconnects a specific MCP client by name (runtime path).
func DisconnectMCPClient(mgr *mcp.ConnectionManager, surface MCPToolSurface) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if mgr == nil {
			writeError(w, http.StatusServiceUnavailable, "MCP not configured")
			return
		}
		name := chi.URLParam(r, "name")
		if err := mgr.Disconnect(name); err != nil {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		syncMCPToolSurface(r.Context(), surface, mgr)
		writeJSON(w, http.StatusOK, map[string]string{"status": "disconnected", "name": name})
	}
}

// GetMCPRuntime summarizes configured and active MCP runtime state.
func GetMCPRuntime(cfg *core.Config, mgr *mcp.ConnectionManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		statuses := mcpStatusMap(mgr)
		configured := 0
		enabled := 0
		for _, server := range cfg.MCP.Servers {
			configured++
			if server.Enabled {
				enabled++
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"configured_servers": configured,
			"enabled_servers":    enabled,
			"connected_servers":  len(statuses),
			"clients":            statuses,
		})
	}
}

// ListMCPServers returns configured MCP servers enriched with live connection state.
func ListMCPServers(cfg *core.Config, mgr *mcp.ConnectionManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		statuses := mcpStatusMap(mgr)
		servers := make([]map[string]any, 0, len(cfg.MCP.Servers))
		for _, server := range cfg.MCP.Servers {
			entry := map[string]any{
				"name":       server.Name,
				"transport":  server.Transport,
				"command":    server.Command,
				"args":       server.Args,
				"url":        server.URL,
				"enabled":    server.Enabled,
				"connected":  false,
				"tool_count": 0,
			}
			if status, ok := statuses[server.Name]; ok {
				entry["connected"] = status.Connected
				entry["tool_count"] = status.ToolCount
				entry["server_name"] = status.ServerName
				entry["server_version"] = status.ServerVersion
			}
			servers = append(servers, entry)
		}
		writeJSON(w, http.StatusOK, map[string]any{"servers": servers})
	}
}

// GetMCPServer returns details for a configured MCP server.
func GetMCPServer(cfg *core.Config, mgr *mcp.ConnectionManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := chi.URLParam(r, "name")
		server, ok := findMCPServerConfig(cfg, name)
		if !ok {
			writeError(w, http.StatusNotFound, "MCP server not found")
			return
		}
		statuses := mcpStatusMap(mgr)
		body := map[string]any{
			"name":       server.Name,
			"transport":  server.Transport,
			"command":    server.Command,
			"args":       server.Args,
			"url":        server.URL,
			"enabled":    server.Enabled,
			"connected":  false,
			"tool_count": 0,
		}
		if status, ok := statuses[server.Name]; ok {
			body["connected"] = status.Connected
			body["tool_count"] = status.ToolCount
			body["server_name"] = status.ServerName
			body["server_version"] = status.ServerVersion
		}
		if mgr != nil {
			tools := toolsForConnection(mgr, server.Name)
			if len(tools) > 0 {
				body["tools"] = tools
			}
		}
		writeJSON(w, http.StatusOK, body)
	}
}

// TestMCPServer performs a live connectivity test against a configured MCP server.
func TestMCPServer(cfg *core.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := chi.URLParam(r, "name")
		server, ok := findMCPServerConfig(cfg, name)
		if !ok {
			writeError(w, http.StatusNotFound, "MCP server not found")
			return
		}
		status, err := testMCPServer(r.Context(), server)
		if err != nil {
			writeJSON(w, http.StatusOK, map[string]any{
				"ok":        false,
				"name":      server.Name,
				"transport": server.Transport,
				"error":     err.Error(),
			})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":             true,
			"name":           server.Name,
			"transport":      server.Transport,
			"tool_count":     status.ToolCount,
			"server_name":    status.ServerName,
			"server_version": status.ServerVersion,
		})
	}
}

func findMCPServerConfig(cfg *core.Config, name string) (mcp.McpServerConfig, bool) {
	for _, server := range cfg.MCP.Servers {
		if server.Name == name {
			return mcp.McpServerConfig{
				Name:      server.Name,
				Transport: server.Transport,
				Command:   server.Command,
				Args:      server.Args,
				URL:       server.URL,
				Env:       server.Env,
				Enabled:   server.Enabled,
			}, true
		}
	}
	return mcp.McpServerConfig{}, false
}

func mcpStatusMap(mgr *mcp.ConnectionManager) map[string]mcp.ServerStatus {
	if mgr == nil {
		return map[string]mcp.ServerStatus{}
	}
	statuses := mgr.Statuses()
	out := make(map[string]mcp.ServerStatus, len(statuses))
	for _, status := range statuses {
		out[status.Name] = status
	}
	return out
}

func toolsForConnection(mgr *mcp.ConnectionManager, name string) []mcp.ToolDescriptor {
	if mgr == nil {
		return nil
	}
	conn, ok := mgr.Connection(name)
	if !ok {
		return nil
	}
	return conn.Tools
}

func syncMCPToolSurface(ctx context.Context, surface MCPToolSurface, mgr *mcp.ConnectionManager) {
	if surface == nil || mgr == nil {
		return
	}
	surface.SyncMCPToolSurface(ctx, mgr)
}

func testMCPServer(ctx context.Context, cfg mcp.McpServerConfig) (mcp.ServerStatus, error) {
	switch cfg.Transport {
	case "stdio":
		conn, err := mcp.ConnectStdio(ctx, cfg.Name, cfg.Command, cfg.Args, cfg.Env)
		if err != nil {
			return mcp.ServerStatus{}, err
		}
		defer func() { _ = conn.Close() }()
		return mcp.ServerStatus{
			Name:          cfg.Name,
			Connected:     true,
			ToolCount:     len(conn.Tools),
			ServerName:    conn.ServerName,
			ServerVersion: conn.ServerVersion,
		}, nil
	case "sse":
		if cfg.URL == "" {
			return mcp.ServerStatus{}, fmt.Errorf("mcp: SSE transport requires a URL")
		}
		conn, err := mcp.ConnectSSE(ctx, cfg.Name, cfg.URL)
		if err != nil {
			return mcp.ServerStatus{}, err
		}
		defer func() { _ = conn.Close() }()
		return mcp.ServerStatus{
			Name:          cfg.Name,
			Connected:     true,
			ToolCount:     len(conn.Tools),
			ServerName:    conn.ServerName,
			ServerVersion: conn.ServerVersion,
		}, nil
	default:
		return mcp.ServerStatus{}, fmt.Errorf("mcp: unsupported transport %q", cfg.Transport)
	}
}
