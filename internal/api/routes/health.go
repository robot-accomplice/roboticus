package routes

import (
	"encoding/json"
	"net/http"
	"runtime"
	"time"

	"github.com/rs/zerolog/log"

	"goboticus/internal/db"
	"goboticus/internal/llm"
)

var startedAt = time.Now()

// Health returns the health check endpoint handler.
func Health(store *db.Store, llmSvc *llm.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		providers := []map[string]any{}
		if llmSvc != nil {
			for _, p := range llmSvc.Status() {
				providers = append(providers, map[string]any{
					"name":     p.Name,
					"state":    p.State,
					"format":   p.Format,
					"is_local": p.IsLocal,
				})
			}
		}

		resp := map[string]any{
			"status":    "ok",
			"uptime":    time.Since(startedAt).String(),
			"go":        runtime.Version(),
			"providers": providers,
		}

		writeJSON(w, http.StatusOK, resp)
	}
}

// AgentCard returns the A2A agent discovery card.
func AgentCard() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		card := map[string]any{
			"name":        "goboticus",
			"description": "Autonomous AI agent runtime",
			"url":         "https://github.com/goboticus",
			"version":     "0.1.0",
			"capabilities": []string{
				"chat",
				"tool-use",
				"multi-model",
				"memory",
			},
		}
		writeJSON(w, http.StatusOK, card)
	}
}

// GetLogs returns recent log entries.
func GetLogs() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// TODO: implement ring buffer log capture
		writeJSON(w, http.StatusOK, map[string]any{
			"logs": []string{},
			"note": "log capture not yet implemented",
		})
	}
}

// writeJSON is a helper to write JSON responses.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// writeError is a helper to write JSON error responses.
// For client-facing errors (400s) the message is passed through.
// For internal errors (500s) the raw error is logged and a generic message is returned.
func writeError(w http.ResponseWriter, status int, msg string) {
	if status >= 500 {
		log.Error().Str("error", msg).Msg("internal error")
		writeJSON(w, status, map[string]string{"error": "internal error"})
		return
	}
	writeJSON(w, status, map[string]string{"error": msg})
}
