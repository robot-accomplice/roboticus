package routes

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"

	"roboticus/internal/core"
	"roboticus/internal/plugin"
)

// InstallPlugin installs a plugin into the configured plugin directory and, when
// a live registry is available, loads it into the running daemon immediately.
func InstallPlugin(cfg *core.Config, reg *plugin.Registry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Name       string `json:"name"`
			Content    string `json:"content"`
			SourcePath string `json:"source_path"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
		if req.Name == "" {
			writeError(w, http.StatusBadRequest, "name is required")
			return
		}
		if req.Content == "" && req.SourcePath == "" {
			writeError(w, http.StatusBadRequest, "content or source_path is required")
			return
		}

		pluginsDir := cfg.Plugins.Dir
		if pluginsDir == "" {
			pluginsDir = filepath.Join(core.ConfigDir(), "plugins")
		}
		pluginDir := filepath.Join(pluginsDir, req.Name)
		if _, err := os.Stat(pluginDir); err == nil {
			writeError(w, http.StatusConflict, "plugin already installed")
			return
		}

		var err error
		if req.SourcePath != "" {
			err = plugin.InstallFromSource(req.SourcePath, pluginDir)
		} else {
			err = plugin.InstallCompat(pluginDir, req.Name, req.Content)
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		if reg != nil {
			if _, err := reg.LoadDirectory(pluginDir); err != nil {
				_ = plugin.RemoveInstall(pluginDir)
				writeError(w, http.StatusBadRequest, err.Error())
				return
			}
		}

		writeJSON(w, http.StatusCreated, map[string]string{
			"status": "installed",
			"name":   req.Name,
			"path":   pluginDir,
		})
	}
}
