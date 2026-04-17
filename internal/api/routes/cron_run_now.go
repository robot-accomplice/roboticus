package routes

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"roboticus/internal/core"
	"roboticus/internal/db"
	"roboticus/internal/pipeline"
	"roboticus/internal/schedule"
)

// RunCronJobNow triggers immediate execution of a cron job through the same
// lease/run-history lifecycle used by the durable cron worker.
func RunCronJobNow(p pipeline.Runner, store *db.Store, agentName ...string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		cronAgentName := "Roboticus"
		if len(agentName) > 0 && agentName[0] != "" {
			cronAgentName = agentName[0]
		}

		errBus := core.NewErrorBus(r.Context(), 1)
		defer errBus.Drain(100 * time.Millisecond)

		var outcome *pipeline.Outcome
		worker := schedule.NewCronWorker(store, "manual-route", 0,
			schedule.CronExecutorFunc(func(ctx context.Context, job *schedule.CronJob) error {
				input := pipeline.Input{
					Content:   job.PayloadJSON,
					AgentID:   job.AgentID,
					AgentName: cronAgentName,
					Platform:  "cron",
				}
				if input.AgentID == "" {
					input.AgentID = "default"
				}
				if input.Content == "" {
					input.Content = "Execute scheduled job: " + job.Name
				}
				var err error
				outcome, err = pipeline.RunPipeline(ctx, p, pipeline.PresetCron(), input)
				return err
			}),
			errBus,
		)

		if err := worker.RunJobNow(r.Context(), id); err != nil {
			var leaseErr *schedule.LeaseError
			switch {
			case errors.Is(err, sql.ErrNoRows):
				writeError(w, http.StatusNotFound, "cron job not found or disabled")
			case errors.As(err, &leaseErr):
				writeError(w, http.StatusConflict, err.Error())
			default:
				writeError(w, http.StatusInternalServerError, err.Error())
			}
			return
		}
		if outcome == nil {
			writeError(w, http.StatusInternalServerError, "cron job produced no outcome")
			return
		}
		writeJSON(w, http.StatusOK, outcome)
	}
}
