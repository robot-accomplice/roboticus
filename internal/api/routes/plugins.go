package routes

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"goboticus/internal/plugin"
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
