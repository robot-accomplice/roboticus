package routes

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/go-chi/chi/v5"

	"roboticus/internal/plugin"
)

// ListPlugins returns all registered plugins.
func ListPlugins(reg *plugin.Registry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if reg == nil {
			writeJSON(w, http.StatusOK, map[string]any{"plugins": []any{}})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"plugins": reg.List()})
	}
}

// ListPluginTools returns all tools from all plugins.
func ListPluginTools(reg *plugin.Registry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if reg == nil {
			writeJSON(w, http.StatusOK, map[string]any{"tools": []any{}})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"tools": reg.AllTools()})
	}
}

// EnablePlugin enables a plugin by name.
func EnablePlugin(reg *plugin.Registry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if reg == nil {
			writeError(w, http.StatusServiceUnavailable, "plugin registry not configured")
			return
		}
		name := chi.URLParam(r, "name")
		if err := reg.Enable(name); err != nil {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "enabled"})
	}
}

// DisablePlugin disables a plugin by name.
func DisablePlugin(reg *plugin.Registry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if reg == nil {
			writeError(w, http.StatusServiceUnavailable, "plugin registry not configured")
			return
		}
		name := chi.URLParam(r, "name")
		if err := reg.Disable(name); err != nil {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "disabled"})
	}
}

// ExecutePluginTool executes a specific tool from a plugin.
func ExecutePluginTool(reg *plugin.Registry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if reg == nil {
			writeError(w, http.StatusServiceUnavailable, "plugin registry not configured")
			return
		}
		toolName := chi.URLParam(r, "tool")

		body, err := io.ReadAll(r.Body)
		if err != nil {
			writeError(w, http.StatusBadRequest, "failed to read request body")
			return
		}
		if len(body) == 0 {
			body = []byte("{}")
		}

		result, err := reg.ExecuteTool(r.Context(), toolName, json.RawMessage(body))
		if err != nil {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, result)
	}
}
