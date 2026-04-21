package db

import (
	"context"
	"encoding/json"

	"github.com/rs/zerolog/log"

	"roboticus/internal/hostresources"
)

// BaselineRunRow is the durable metadata record for one exercise/baseline run.
// A single run may cover multiple models; prompt-level rows link back via RunID.
type BaselineRunRow struct {
	RunID             string                  `json:"run_id"`
	Initiator         string                  `json:"initiator"`
	Status            string                  `json:"status"`
	ModelCount        int                     `json:"model_count"`
	Models            []string                `json:"models,omitempty"`
	Iterations        int                     `json:"iterations"`
	ConfigFingerprint string                  `json:"config_fingerprint,omitempty"`
	GitRevision       string                  `json:"git_revision,omitempty"`
	Notes             string                  `json:"notes,omitempty"`
	StartedAt         string                  `json:"started_at"`
	FinishedAt        string                  `json:"finished_at,omitempty"`
	StartResources    *hostresources.Snapshot `json:"start_resources,omitempty"`
	EndResources      *hostresources.Snapshot `json:"end_resources,omitempty"`
}

// ExerciseResultRow is a single persisted exercise prompt result.
type ExerciseResultRow struct {
	ID            string                  `json:"id"`
	RunID         string                  `json:"run_id"`
	Model         string                  `json:"model"`
	IntentClass   string                  `json:"intent_class"`
	Complexity    string                  `json:"complexity"`
	Prompt        string                  `json:"prompt"`
	Content       string                  `json:"content,omitempty"`
	Quality       float64                 `json:"quality"`
	LatencyMs     int64                   `json:"latency_ms"`
	Passed        bool                    `json:"passed"`
	ErrorMsg      string                  `json:"error_msg,omitempty"`
	CreatedAt     string                  `json:"created_at"`
	ResourceStart *hostresources.Snapshot `json:"resource_start,omitempty"`
	ResourceEnd   *hostresources.Snapshot `json:"resource_end,omitempty"`
}

// InsertBaselineRun persists or updates the metadata record for a baseline run.
func InsertBaselineRun(ctx context.Context, store *Store, row BaselineRunRow) error {
	modelsJSON, err := json.Marshal(row.Models)
	if err != nil {
		return err
	}
	startResourcesJSON := hostresources.Marshal(row.StartResources)
	endResourcesJSON := hostresources.Marshal(row.EndResources)
	_, err = store.ExecContext(ctx,
		`INSERT INTO baseline_runs (
			run_id, initiator, status, model_count, models_json, iterations,
			config_fingerprint, git_revision, notes, started_at, finished_at,
			start_resources_json, end_resources_json
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, COALESCE(NULLIF(?, ''), datetime('now')), NULLIF(?, ''), NULLIF(?, ''), NULLIF(?, ''))
		ON CONFLICT(run_id) DO UPDATE SET
			initiator = excluded.initiator,
			status = excluded.status,
			model_count = excluded.model_count,
			models_json = excluded.models_json,
			iterations = excluded.iterations,
			config_fingerprint = excluded.config_fingerprint,
			git_revision = excluded.git_revision,
			notes = excluded.notes,
			start_resources_json = COALESCE(excluded.start_resources_json, baseline_runs.start_resources_json),
			end_resources_json = COALESCE(excluded.end_resources_json, baseline_runs.end_resources_json)`,
		row.RunID, row.Initiator, row.Status, row.ModelCount, string(modelsJSON), row.Iterations,
		row.ConfigFingerprint, row.GitRevision, row.Notes, row.StartedAt, row.FinishedAt,
		startResourcesJSON, endResourcesJSON,
	)
	if err != nil {
		log.Warn().Err(err).Str("run_id", row.RunID).Msg("exercise: failed to persist baseline run metadata")
	}
	return err
}

// CompleteBaselineRun marks a baseline run finished and records its terminal status.
func CompleteBaselineRun(ctx context.Context, store *Store, runID, status, notes string, endResources *hostresources.Snapshot) error {
	endResourcesJSON := hostresources.Marshal(endResources)
	_, err := store.ExecContext(ctx,
		`UPDATE baseline_runs
		    SET status = ?,
		        notes = CASE WHEN ? = '' THEN notes ELSE ? END,
		        finished_at = datetime('now'),
		        end_resources_json = CASE WHEN ? = '' THEN end_resources_json ELSE ? END
		  WHERE run_id = ?`,
		status, notes, notes, endResourcesJSON, endResourcesJSON, runID,
	)
	if err != nil {
		log.Warn().Err(err).Str("run_id", runID).Msg("exercise: failed to finalize baseline run")
	}
	return err
}

// ListBaselineRuns returns recent baseline runs, newest first.
func ListBaselineRuns(ctx context.Context, store *Store, limit int) []BaselineRunRow {
	if limit <= 0 {
		limit = 20
	}
	rows, err := store.QueryContext(ctx,
		`SELECT run_id, initiator, status, model_count, COALESCE(models_json, '[]'),
		        iterations, COALESCE(config_fingerprint, ''), COALESCE(git_revision, ''),
		        COALESCE(notes, ''), started_at, COALESCE(finished_at, ''),
		        COALESCE(start_resources_json, ''), COALESCE(end_resources_json, '')
		   FROM baseline_runs
		  ORDER BY started_at DESC
		  LIMIT ?`, limit)
	if err != nil {
		log.Warn().Err(err).Msg("exercise: baseline run list query failed")
		return nil
	}
	defer func() { _ = rows.Close() }()

	var out []BaselineRunRow
	for rows.Next() {
		var (
			entry             BaselineRunRow
			modelsRaw         string
			startResourcesRaw string
			endResourcesRaw   string
		)
		if err := rows.Scan(
			&entry.RunID, &entry.Initiator, &entry.Status, &entry.ModelCount, &modelsRaw,
			&entry.Iterations, &entry.ConfigFingerprint, &entry.GitRevision,
			&entry.Notes, &entry.StartedAt, &entry.FinishedAt, &startResourcesRaw, &endResourcesRaw,
		); err != nil {
			continue
		}
		_ = json.Unmarshal([]byte(modelsRaw), &entry.Models)
		entry.StartResources = hostresources.FromJSON(startResourcesRaw)
		entry.EndResources = hostresources.FromJSON(endResourcesRaw)
		out = append(out, entry)
	}
	return out
}

// InsertExerciseResult persists a single exercise prompt result.
func InsertExerciseResult(ctx context.Context, store *Store, row ExerciseResultRow) error {
	passed := 0
	if row.Passed {
		passed = 1
	}
	resourceStartJSON := hostresources.Marshal(row.ResourceStart)
	resourceEndJSON := hostresources.Marshal(row.ResourceEnd)
	_, err := store.ExecContext(ctx,
		`INSERT INTO exercise_results (id, run_id, model, intent_class, complexity, prompt, content, quality, latency_ms, passed, error_msg, resource_start_json, resource_end_json)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, NULLIF(?, ''), NULLIF(?, ''))`,
		row.ID, row.RunID, row.Model, row.IntentClass, row.Complexity,
		row.Prompt, row.Content, row.Quality, row.LatencyMs, passed, row.ErrorMsg, resourceStartJSON, resourceEndJSON,
	)
	if err != nil {
		log.Warn().Err(err).Str("model", row.Model).Msg("exercise: failed to persist result")
	}
	return err
}

// ModelsWithExerciseResults returns model names that have at least one exercise result.
func ModelsWithExerciseResults(ctx context.Context, store *Store) []string {
	rows, err := store.QueryContext(ctx,
		`SELECT DISTINCT model FROM exercise_results ORDER BY model`)
	if err != nil {
		return nil
	}
	defer func() { _ = rows.Close() }()

	var models []string
	for rows.Next() {
		var m string
		if err := rows.Scan(&m); err == nil {
			models = append(models, m)
		}
	}
	return models
}

// ExerciseResultCountByModel returns a map of model → number of exercise results.
func ExerciseResultCountByModel(ctx context.Context, store *Store) map[string]int {
	rows, err := store.QueryContext(ctx,
		`SELECT model, COUNT(*) FROM exercise_results GROUP BY model`)
	if err != nil {
		return nil
	}
	defer func() { _ = rows.Close() }()

	counts := make(map[string]int)
	for rows.Next() {
		var m string
		var c int
		if err := rows.Scan(&m, &c); err == nil {
			counts[m] = c
		}
	}
	return counts
}

// ExerciseScorecardEntry holds per-model per-intent average quality.
type ExerciseScorecardEntry struct {
	Model       string  `json:"model"`
	IntentClass string  `json:"intent_class"`
	AvgQuality  float64 `json:"avg_quality"`
	Count       int     `json:"count"`
}

// ExerciseScorecard returns per-model per-intent average quality from the
// latest run for each model. Results are suitable for rendering a quality
// matrix (models x intent classes).
func ExerciseScorecard(ctx context.Context, store *Store) []ExerciseScorecardEntry {
	// For each model, pick the latest run_id then aggregate by intent.
	rows, err := store.QueryContext(ctx,
		`WITH latest_runs AS (
		   SELECT model, run_id
		   FROM exercise_results
		   GROUP BY model
		   HAVING created_at = MAX(created_at)
		 )
		 SELECT e.model, e.intent_class, AVG(e.quality), COUNT(*)
		 FROM exercise_results e
		 INNER JOIN latest_runs lr ON e.model = lr.model AND e.run_id = lr.run_id
		 GROUP BY e.model, e.intent_class
		 ORDER BY e.model, e.intent_class`)
	if err != nil {
		log.Warn().Err(err).Msg("exercise: scorecard query failed")
		return nil
	}
	defer func() { _ = rows.Close() }()

	var entries []ExerciseScorecardEntry
	for rows.Next() {
		var e ExerciseScorecardEntry
		if err := rows.Scan(&e.Model, &e.IntentClass, &e.AvgQuality, &e.Count); err == nil {
			entries = append(entries, e)
		}
	}
	return entries
}

// LatestExerciseResults returns the most recent exercise results for a model.
func LatestExerciseResults(ctx context.Context, store *Store, model string) []ExerciseResultRow {
	// Find the latest run_id for this model.
	var runID string
	row := store.QueryRowContext(ctx,
		`SELECT run_id FROM exercise_results WHERE model = ? ORDER BY created_at DESC LIMIT 1`, model)
	if err := row.Scan(&runID); err != nil {
		return nil
	}

	rows, err := store.QueryContext(ctx,
		`SELECT id, run_id, model, intent_class, complexity, prompt, content, quality, latency_ms, passed, COALESCE(error_msg, ''), created_at,
		        COALESCE(resource_start_json, ''), COALESCE(resource_end_json, '')
		 FROM exercise_results WHERE run_id = ? ORDER BY rowid`, runID)
	if err != nil {
		return nil
	}
	defer func() { _ = rows.Close() }()

	var results []ExerciseResultRow
	for rows.Next() {
		var r ExerciseResultRow
		var passed int
		var resourceStartRaw, resourceEndRaw string
		if err := rows.Scan(&r.ID, &r.RunID, &r.Model, &r.IntentClass, &r.Complexity,
			&r.Prompt, &r.Content, &r.Quality, &r.LatencyMs, &passed, &r.ErrorMsg, &r.CreatedAt,
			&resourceStartRaw, &resourceEndRaw); err == nil {
			r.Passed = passed == 1
			r.ResourceStart = hostresources.FromJSON(resourceStartRaw)
			r.ResourceEnd = hostresources.FromJSON(resourceEndRaw)
			results = append(results, r)
		}
	}
	return results
}
