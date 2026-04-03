package routes

import (
	"encoding/json"
	"net/http"

	"goboticus/internal/core"
	"goboticus/internal/db"
)

// ConfigApplyRequest is the request body for applying a config section.
type ConfigApplyRequest struct {
	Section string          `json:"section"`
	Values  json.RawMessage `json:"values"`
}

// ConfigApplyResponse is the response body for config apply.
type ConfigApplyResponse struct {
	Applied bool   `json:"applied"`
	Section string `json:"section"`
	Message string `json:"message"`
}

// ConfigApply persists a config section's values to the identity table and returns success.
// The config is stored as JSON keyed by section name, enabling retrieval on next restart.
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

		// Persist the section values to the identity table.
		key := "config_section:" + req.Section
		_, err := store.ExecContext(r.Context(),
			`INSERT INTO identity (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
			key, string(req.Values))
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, ConfigApplyResponse{
				Applied: false, Section: req.Section, Message: "failed to persist: " + err.Error(),
			})
			return
		}

		writeJSON(w, http.StatusOK, ConfigApplyResponse{
			Applied: true, Section: req.Section, Message: "config section persisted — restart to activate",
		})
	}
}
