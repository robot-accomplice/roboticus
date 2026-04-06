package routes

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"roboticus/internal/db"
	"roboticus/internal/pipeline"
)

// ListCronJobs returns all cron jobs.
func ListCronJobs(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rows, err := store.QueryContext(r.Context(),
			`SELECT id, name, description, enabled, schedule_kind, schedule_expr,
			        schedule_every_ms, agent_id, payload_json, last_run_at, last_status, next_run_at
			 FROM cron_jobs ORDER BY name`)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to query cron jobs")
			return
		}
		defer func() { _ = rows.Close() }()

		var jobs []map[string]any
		for rows.Next() {
			var id, name, scheduleKind, agentID, payloadJSON string
			var description, scheduleExpr, lastRunAt, lastStatus, nextRunAt *string
			var scheduleEveryMs *int64
			var enabled bool
			if err := rows.Scan(&id, &name, &description, &enabled, &scheduleKind, &scheduleExpr,
				&scheduleEveryMs, &agentID, &payloadJSON, &lastRunAt, &lastStatus, &nextRunAt); err != nil {
				writeError(w, http.StatusInternalServerError, "failed to read cron job row")
				return
			}
			j := map[string]any{
				"id":            id,
				"name":          name,
				"enabled":       enabled,
				"schedule_kind": scheduleKind,
				"agent_id":      agentID,
				"payload":       payloadJSON,
			}
			if description != nil {
				j["description"] = *description
			}
			if scheduleExpr != nil {
				j["schedule_expr"] = *scheduleExpr
			}
			if scheduleEveryMs != nil {
				j["schedule_every_ms"] = *scheduleEveryMs
			}
			if lastRunAt != nil {
				j["last_run_at"] = *lastRunAt
			}
			if lastStatus != nil {
				j["last_status"] = *lastStatus
			}
			if nextRunAt != nil {
				j["next_run_at"] = *nextRunAt
			}
			jobs = append(jobs, j)
		}
		if jobs == nil {
			jobs = []map[string]any{}
		}
		writeJSON(w, http.StatusOK, map[string]any{"jobs": jobs})
	}
}

// CreateCronJob creates a new cron job.
func CreateCronJob(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Name            string `json:"name"`
			Description     string `json:"description"`
			ScheduleKind    string `json:"schedule_kind"`
			ScheduleExpr    string `json:"schedule_expr"`
			ScheduleEveryMs *int64 `json:"schedule_every_ms"`
			AgentID         string `json:"agent_id"`
			PayloadJSON     string `json:"payload_json"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
		if req.Name == "" || req.ScheduleKind == "" {
			writeError(w, http.StatusBadRequest, "name and schedule_kind are required")
			return
		}
		if req.AgentID == "" {
			req.AgentID = "default"
		}
		if req.PayloadJSON == "" {
			req.PayloadJSON = "{}"
		}

		id := db.NewID()
		_, err := store.ExecContext(r.Context(),
			`INSERT INTO cron_jobs (id, name, description, schedule_kind, schedule_expr, schedule_every_ms, agent_id, payload_json)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			id, req.Name, req.Description, req.ScheduleKind, req.ScheduleExpr, req.ScheduleEveryMs, req.AgentID, req.PayloadJSON,
		)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, map[string]string{"id": id})
	}
}

// ListCronRuns returns recent cron run history.
func ListCronRuns(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rows, err := store.QueryContext(r.Context(),
			`SELECT id, job_id, status, duration_ms, error_msg, '', timestamp
			 FROM cron_runs ORDER BY timestamp DESC LIMIT 50`)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to query cron runs")
			return
		}
		defer func() { _ = rows.Close() }()

		var runs []map[string]any
		for rows.Next() {
			var id, jobID, status, createdAt string
			var durationMs *int64
			var errMsg, outputText *string
			if err := rows.Scan(&id, &jobID, &status, &durationMs, &errMsg, &outputText, &createdAt); err != nil {
				writeError(w, http.StatusInternalServerError, "failed to read cron run row")
				return
			}
			run := map[string]any{
				"id":         id,
				"job_id":     jobID,
				"status":     status,
				"created_at": createdAt,
			}
			if durationMs != nil {
				run["duration_ms"] = *durationMs
			}
			if errMsg != nil {
				run["error"] = *errMsg
			}
			if outputText != nil {
				run["output_text"] = *outputText
			}
			runs = append(runs, run)
		}
		if runs == nil {
			runs = []map[string]any{}
		}
		writeJSON(w, http.StatusOK, map[string]any{"runs": runs})
	}
}

// GetCronJob returns a single cron job.
func GetCronJob(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		row := store.QueryRowContext(r.Context(),
			`SELECT id, name, description, enabled, schedule_kind, schedule_expr, schedule_every_ms,
			        agent_id, payload_json, last_run_at, last_status, next_run_at
			 FROM cron_jobs WHERE id = ?`, id)

		var name, scheduleKind, agentID, payloadJSON string
		var description, scheduleExpr, lastRunAt, lastStatus, nextRunAt *string
		var scheduleEveryMs *int64
		var enabled bool
		if err := row.Scan(&id, &name, &description, &enabled, &scheduleKind, &scheduleExpr,
			&scheduleEveryMs, &agentID, &payloadJSON, &lastRunAt, &lastStatus, &nextRunAt); err != nil {
			writeError(w, http.StatusNotFound, "cron job not found")
			return
		}
		j := map[string]any{
			"id": id, "name": name, "enabled": enabled,
			"schedule_kind": scheduleKind, "agent_id": agentID, "payload": payloadJSON,
		}
		if description != nil {
			j["description"] = *description
		}
		if scheduleExpr != nil {
			j["schedule_expr"] = *scheduleExpr
		}
		if scheduleEveryMs != nil {
			j["schedule_every_ms"] = *scheduleEveryMs
		}
		if lastRunAt != nil {
			j["last_run_at"] = *lastRunAt
		}
		if lastStatus != nil {
			j["last_status"] = *lastStatus
		}
		if nextRunAt != nil {
			j["next_run_at"] = *nextRunAt
		}
		writeJSON(w, http.StatusOK, j)
	}
}

// UpdateCronJob updates a cron job atomically. All field updates happen in a
// single transaction — on any failure, all changes are rolled back.
func UpdateCronJob(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		var req struct {
			Name         *string `json:"name"`
			Description  *string `json:"description"`
			ScheduleKind *string `json:"schedule_kind"`
			ScheduleExpr *string `json:"schedule_expr"`
			Enabled      *bool   `json:"enabled"`
			PayloadJSON  *string `json:"payload_json"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}

		// Build a single UPDATE with dynamic SET clauses.
		var setClauses []string
		var args []any

		if req.Name != nil {
			setClauses = append(setClauses, "name = ?")
			args = append(args, *req.Name)
		}
		if req.Description != nil {
			setClauses = append(setClauses, "description = ?")
			args = append(args, *req.Description)
		}
		if req.ScheduleKind != nil {
			setClauses = append(setClauses, "schedule_kind = ?")
			args = append(args, *req.ScheduleKind)
		}
		if req.ScheduleExpr != nil {
			setClauses = append(setClauses, "schedule_expr = ?")
			args = append(args, *req.ScheduleExpr)
		}
		if req.Enabled != nil {
			setClauses = append(setClauses, "enabled = ?")
			args = append(args, *req.Enabled)
		}
		if req.PayloadJSON != nil {
			setClauses = append(setClauses, "payload_json = ?")
			args = append(args, *req.PayloadJSON)
		}

		if len(setClauses) == 0 {
			writeJSON(w, http.StatusOK, map[string]string{"status": "no changes"})
			return
		}

		args = append(args, id)
		query := "UPDATE cron_jobs SET " + strings.Join(setClauses, ", ") + " WHERE id = ?"

		result, err := store.ExecContext(r.Context(), query, args...)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to update cron job")
			return
		}
		rows, _ := result.RowsAffected()
		if rows == 0 {
			writeError(w, http.StatusNotFound, "cron job not found")
			return
		}

		writeJSON(w, http.StatusOK, map[string]string{"status": "updated"})
	}
}

// DeleteCronJob deletes a cron job.
func DeleteCronJob(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		_, err := store.ExecContext(r.Context(), `DELETE FROM cron_jobs WHERE id = ?`, id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
	}
}

// RunCronJobNow triggers immediate execution of a cron job.
func RunCronJobNow(p pipeline.Runner, store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		row := store.QueryRowContext(r.Context(),
			`SELECT payload_json FROM cron_jobs WHERE id = ? AND enabled = 1`, id)

		var payloadJSON string
		if err := row.Scan(&payloadJSON); err != nil {
			writeError(w, http.StatusNotFound, "cron job not found or disabled")
			return
		}

		input := pipeline.Input{
			Content:   payloadJSON,
			AgentID:   "default",
			AgentName: "Roboticus",
			Platform:  "cron",
		}

		outcome, err := pipeline.RunPipeline(r.Context(), p, pipeline.PresetCron(), input)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, outcome)
	}
}
