package routes

import (
	"encoding/json"
	"net/http"
	"runtime"
	"strconv"
	"sync"
	"time"

	"github.com/rs/zerolog/log"

	"roboticus/internal/core"
	"roboticus/internal/db"
	"roboticus/internal/llm"
)

// Health returns the health check endpoint handler.
func Health(store *db.Store, llmSvc *llm.Service, cfg ...*core.Config) http.HandlerFunc {
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

		agentName := "roboticus"
		if len(cfg) > 0 && cfg[0] != nil && cfg[0].Agent.Name != "" {
			agentName = cfg[0].Agent.Name
		}

		resp := map[string]any{
			"status":         "ok",
			"uptime":         time.Since(processStartTime).String(),
			"uptime_seconds": time.Since(processStartTime).Seconds(),
			"version":        "0.1.0",
			"agent":          agentName,
			"go":             runtime.Version(),
			"providers":      providers,
		}

		writeJSON(w, http.StatusOK, resp)
	}
}

// AgentCard returns the A2A agent discovery card.
func AgentCard() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		card := map[string]any{
			"name":        "roboticus",
			"description": "Autonomous AI agent runtime",
			"url":         "https://github.com/roboticus",
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

// LogTailer provides read access to the log ring buffer.
type LogTailer interface {
	Tail(n int, level string) []any
}

// GetLogs returns recent log entries from the ring buffer.
// Accepts ?lines=100&level=error query parameters.
func GetLogs() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Log buffer is set via SetLogBuffer; if not set, return empty.
		bufMu.RLock()
		buf := logBuf
		bufMu.RUnlock()

		if buf == nil {
			writeJSON(w, http.StatusOK, map[string]any{
				"logs": []any{},
				"note": "log capture not configured",
			})
			return
		}

		lines := 100
		if v := r.URL.Query().Get("lines"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				lines = n
			}
		}
		level := r.URL.Query().Get("level")
		entries := buf(lines, level)
		writeJSON(w, http.StatusOK, map[string]any{"logs": entries})
	}
}

// logBuf holds a function reference to the log ring buffer's Tail method.
// This avoids importing the api package into routes (circular import).
var (
	bufMu  sync.RWMutex
	logBuf func(int, string) []any
)

// SetLogBuffer registers the log buffer's tail function for the GetLogs handler.
func SetLogBuffer(tailFn func(int, string) []any) {
	bufMu.Lock()
	logBuf = tailFn
	bufMu.Unlock()
}

// writeJSON is a helper to write JSON responses.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// writeError writes an RFC 9457 Problem Details response for client errors (4xx)
// and a masked generic response for server errors (5xx).
func writeError(w http.ResponseWriter, status int, msg string) {
	if status >= 500 {
		log.Error().Str("error", msg).Msg("internal error")
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"type":   "about:blank",
			"title":  http.StatusText(status),
			"status": status,
			"detail": "internal error",
		})
		return
	}
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"type":   "about:blank",
		"title":  http.StatusText(status),
		"status": status,
		"detail": msg,
	})
}
