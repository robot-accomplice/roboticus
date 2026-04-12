package db

import (
	"context"

	"github.com/rs/zerolog/log"
)

// ExerciseResultRow is a single persisted exercise prompt result.
type ExerciseResultRow struct {
	ID          string  `json:"id"`
	RunID       string  `json:"run_id"`
	Model       string  `json:"model"`
	IntentClass string  `json:"intent_class"`
	Complexity  string  `json:"complexity"`
	Prompt      string  `json:"prompt"`
	Content     string  `json:"content,omitempty"`
	Quality     float64 `json:"quality"`
	LatencyMs   int64   `json:"latency_ms"`
	Passed      bool    `json:"passed"`
	ErrorMsg    string  `json:"error_msg,omitempty"`
	CreatedAt   string  `json:"created_at"`
}

// InsertExerciseResult persists a single exercise prompt result.
func InsertExerciseResult(ctx context.Context, store *Store, row ExerciseResultRow) error {
	passed := 0
	if row.Passed {
		passed = 1
	}
	_, err := store.ExecContext(ctx,
		`INSERT INTO exercise_results (id, run_id, model, intent_class, complexity, prompt, content, quality, latency_ms, passed, error_msg)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		row.ID, row.RunID, row.Model, row.IntentClass, row.Complexity,
		row.Prompt, row.Content, row.Quality, row.LatencyMs, passed, row.ErrorMsg,
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
		`SELECT id, run_id, model, intent_class, complexity, prompt, content, quality, latency_ms, passed, COALESCE(error_msg, ''), created_at
		 FROM exercise_results WHERE run_id = ? ORDER BY rowid`, runID)
	if err != nil {
		return nil
	}
	defer func() { _ = rows.Close() }()

	var results []ExerciseResultRow
	for rows.Next() {
		var r ExerciseResultRow
		var passed int
		if err := rows.Scan(&r.ID, &r.RunID, &r.Model, &r.IntentClass, &r.Complexity,
			&r.Prompt, &r.Content, &r.Quality, &r.LatencyMs, &passed, &r.ErrorMsg, &r.CreatedAt); err == nil {
			r.Passed = passed == 1
			results = append(results, r)
		}
	}
	return results
}
