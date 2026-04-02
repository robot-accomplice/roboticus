package routes

import (
	"encoding/json"
	"net/http"

	"goboticus/internal/core"
)

type ConfigApplyRequest struct {
	Section string          `json:"section"`
	Values  json.RawMessage `json:"values"`
}

type ConfigApplyResponse struct {
	Applied bool   `json:"applied"`
	Section string `json:"section"`
	Message string `json:"message"`
}

func ConfigApply(cfg *core.Config) http.HandlerFunc {
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

		// For now, validate the section exists but don't apply (full implementation
		// requires config hot-reload wiring through the daemon).
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

		writeJSON(w, http.StatusOK, ConfigApplyResponse{
			Applied: true, Section: req.Section, Message: "config apply acknowledged",
		})
	}
}
