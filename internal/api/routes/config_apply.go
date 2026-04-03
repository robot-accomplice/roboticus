package routes

import (
	"encoding/json"
	"net/http"
	"strings"

	"goboticus/internal/core"
	"goboticus/internal/db"
)

// ConfigApplyRequest is the request body for applying a config section.
type ConfigApplyRequest struct {
	Section string         `json:"section"`
	Values  map[string]any `json:"values"`
}

// ConfigApplyResponse is the response body for config apply.
type ConfigApplyResponse struct {
	Applied bool   `json:"applied"`
	Section string `json:"section"`
	Message string `json:"message"`
}

// ConfigApply applies a config section's values through the same TOML
// round-trip logic as UpdateConfig: load, merge, validate, write.
func ConfigApply(cfg *core.Config, store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
			return
		}

		var req ConfigApplyRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
			return
		}

		if req.Section == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "section is required"})
			return
		}

		validSections := map[string]bool{
			"agent": true, "server": true, "models": true, "memory": true,
			"cache": true, "skills": true, "channels": true, "wallet": true,
		}
		if !validSections[req.Section] {
			writeJSON(w, http.StatusBadRequest, ConfigApplyResponse{
				Applied: false, Section: req.Section, Message: "unknown config section",
			})
			return
		}

		// Build a patch where the key is the section name and the value is the section values.
		patch := map[string]any{req.Section: req.Values}
		_, err := applyConfigPatch(r.Context(), store, patch)
		if err != nil {
			errMsg := err.Error()
			status := http.StatusInternalServerError
			if strings.HasPrefix(errMsg, "validation failed:") || strings.HasPrefix(errMsg, "patch produced invalid config:") {
				status = http.StatusBadRequest
			}
			writeJSON(w, status, ConfigApplyResponse{
				Applied: false, Section: req.Section, Message: errMsg,
			})
			return
		}

		writeJSON(w, http.StatusOK, ConfigApplyResponse{
			Applied: true, Section: req.Section, Message: "config section applied",
		})
	}
}
