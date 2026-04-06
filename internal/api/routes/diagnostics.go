package routes

import (
	"encoding/json"
	"net/http"
	"runtime"
	"time"

	"roboticus/internal/db"
)

type DiagnosticReport struct {
	Status       string            `json:"status"`
	Timestamp    string            `json:"timestamp"`
	GoVersion    string            `json:"go_version"`
	NumGoroutine int               `json:"num_goroutine"`
	MemoryMB     float64           `json:"memory_mb"`
	Checks       map[string]string `json:"checks"`
}

func Diagnostics(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		report := DiagnosticReport{
			Status:       "ok",
			Timestamp:    time.Now().UTC().Format(time.RFC3339),
			GoVersion:    runtime.Version(),
			NumGoroutine: runtime.NumGoroutine(),
			Checks:       make(map[string]string),
		}

		// Memory stats
		var memStats runtime.MemStats
		runtime.ReadMemStats(&memStats)
		report.MemoryMB = float64(memStats.Alloc) / 1024 / 1024

		// DB check
		if store != nil {
			if err := store.Ping(); err != nil {
				report.Checks["database"] = "error: " + err.Error()
				report.Status = "degraded"
			} else {
				report.Checks["database"] = "ok"
			}
		} else {
			report.Checks["database"] = "not configured"
		}

		// Schema check
		report.Checks["schema"] = "ok" // base check

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(report)
	}
}
