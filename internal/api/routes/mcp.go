package routes

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"roboticus/internal/mcp"
)

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
func ConnectMCPServer(mgr *mcp.ConnectionManager) http.HandlerFunc {
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
		writeJSON(w, http.StatusOK, map[string]string{"status": "connected"})
	}
}

// DisconnectMCPServer disconnects an MCP server by name.
func DisconnectMCPServer(mgr *mcp.ConnectionManager) http.HandlerFunc {
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
		writeJSON(w, http.StatusOK, map[string]string{"status": "disconnected"})
	}
}
