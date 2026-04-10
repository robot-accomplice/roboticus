package routes

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/rs/zerolog/log"

	"roboticus/internal/db"
	"roboticus/internal/llm"
)

const maxRoutingDatasetLimit = 50000

// GetRoutingDataset exports joined routing decisions and inference outcomes.
func GetRoutingDataset(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		filter, includeUserExcerpt, format, err := parseRoutingDatasetQuery(r)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}

		repo := db.NewRoutingDatasetRepo(store)
		rows, err := repo.ExtractRoutingDataset(r.Context(), filter)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to query routing dataset")
			return
		}
		summary, err := repo.SummarizeRoutingDataset(r.Context(), filter)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to summarize routing dataset")
			return
		}

		if !includeUserExcerpt {
			for i := range rows {
				rows[i].UserExcerpt = "[redacted]"
			}
		}

		if format == "tsv" {
			if !includeUserExcerpt {
				writeError(w, http.StatusBadRequest, "TSV export includes user excerpts; pass include_user_excerpt=true to confirm.")
				return
			}
			w.Header().Set("Content-Type", "text/tab-separated-values; charset=utf-8")
			if _, err := fmt.Fprint(w, routingDatasetTSV(rows)); err != nil {
				log.Trace().Err(err).Msg("routing_admin: TSV response write failed")
			}
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"rows":    rows,
			"summary": summary,
		})
	}
}

// ExerciseModel runs the exercise matrix against a specific model and returns
// per-prompt quality scores. This bypasses the pipeline (no session, no guards,
// no memory) — it's a direct LLM quality measurement for baselining.
func ExerciseModel(llmSvc *llm.Service, store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Model string `json:"model"`
			RunID string `json:"run_id,omitempty"` // Caller-provided run ID for grouping results.
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
		if req.Model == "" {
			writeError(w, http.StatusBadRequest, "model is required")
			return
		}
		runID := req.RunID
		if runID == "" {
			runID = db.NewID()
		}

		// Persist each result as it completes — survives client disconnects.
		// Uses background context so DB writes succeed even if the HTTP
		// connection dies mid-exercise.
		persistResult := llm.ExerciseCallback(func(index int, result llm.ExerciseResult) {
			if store == nil {
				return
			}
			_ = db.InsertExerciseResult(context.Background(), store, db.ExerciseResultRow{
				ID:          db.NewID(),
				RunID:       runID,
				Model:       req.Model,
				IntentClass: result.Prompt.Intent.String(),
				Complexity:  result.Prompt.Complexity.String(),
				Prompt:      result.Prompt.Prompt,
				Content:     result.Content,
				Quality:     result.Quality,
				LatencyMs:   result.LatencyMs,
				Passed:      result.Passed,
				ErrorMsg:    result.Error,
			})
		})

		results := llm.RunExercise(r.Context(), llmSvc, req.Model, llm.ExerciseMatrix, persistResult)

		type promptResult struct {
			Intent     string  `json:"intent"`
			Complexity string  `json:"complexity"`
			Prompt     string  `json:"prompt"`
			Content    string  `json:"content,omitempty"`
			Quality    float64 `json:"quality"`
			LatencyMs  int64   `json:"latency_ms"`
			Passed     bool    `json:"passed"`
			Error      string  `json:"error,omitempty"`
		}

		var pass, fail int
		var totalQuality float64
		intentQuality := make(map[string][]float64)
		promptResults := make([]promptResult, len(results))

		for i, res := range results {
			intentStr := res.Prompt.Intent.String()
			promptResults[i] = promptResult{
				Intent:     intentStr,
				Complexity: res.Prompt.Complexity.String(),
				Prompt:     res.Prompt.Prompt,
				Content:    res.Content,
				Quality:    res.Quality,
				LatencyMs:  res.LatencyMs,
				Passed:     res.Passed,
				Error:      res.Error,
			}
			if res.Passed {
				pass++
			} else {
				fail++
			}
			totalQuality += res.Quality
			intentQuality[intentStr] = append(intentQuality[intentStr], res.Quality)
		}

		// Compute per-intent averages.
		intentAvg := make(map[string]float64)
		for intent, scores := range intentQuality {
			var sum float64
			for _, s := range scores {
				sum += s
			}
			intentAvg[intent] = sum / float64(len(scores))
		}

		avgQuality := 0.0
		if len(results) > 0 {
			avgQuality = totalQuality / float64(len(results))
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"model":          req.Model,
			"run_id":         runID,
			"total":          len(results),
			"pass":           pass,
			"fail":           fail,
			"avg_quality":    avgQuality,
			"intent_quality": intentAvg,
			"results":        promptResults,
		})
	}
}

// GetExerciseStatus returns which models have existing exercise data and how many results.
func GetExerciseStatus(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		counts := db.ExerciseResultCountByModel(r.Context(), store)
		writeJSON(w, http.StatusOK, map[string]any{
			"models": counts,
		})
	}
}

// ResetModelScores clears metascore quality observations for one model or all models.
func ResetModelScores(llmSvc *llm.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		model := strings.TrimSpace(r.URL.Query().Get("model"))
		cleared := 0
		if llmSvc != nil {
			cleared = llmSvc.ResetQualityScores(model)
		}

		message := fmt.Sprintf("cleared %d observation entries for all models", cleared)
		modelValue := any(nil)
		if model != "" {
			modelValue = model
			message = fmt.Sprintf("cleared %d observation entries for %s", cleared, model)
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"cleared": cleared,
			"model":   modelValue,
			"message": message,
		})
	}
}

func parseRoutingDatasetQuery(r *http.Request) (db.DatasetFilter, bool, string, error) {
	query := r.URL.Query()
	filter := db.DatasetFilter{
		Since: query.Get("since"),
		Until: query.Get("until"),
		Limit: parseIntParam(r, "limit", 10000),
	}
	if filter.Limit > maxRoutingDatasetLimit {
		filter.Limit = maxRoutingDatasetLimit
	}
	if filter.Since != "" && !validTimeFilter(filter.Since) {
		return db.DatasetFilter{}, false, "", fmt.Errorf("since must be RFC3339 or YYYY-MM-DD")
	}
	if filter.Until != "" && !validTimeFilter(filter.Until) {
		return db.DatasetFilter{}, false, "", fmt.Errorf("until must be RFC3339 or YYYY-MM-DD")
	}
	if schemaVersionRaw := strings.TrimSpace(query.Get("schema_version")); schemaVersionRaw != "" {
		schemaVersion, err := strconv.Atoi(schemaVersionRaw)
		if err != nil {
			return db.DatasetFilter{}, false, "", fmt.Errorf("schema_version must be an integer")
		}
		filter.SchemaVersion = &schemaVersion
	}

	includeUserExcerpt, _ := strconv.ParseBool(query.Get("include_user_excerpt"))
	return filter, includeUserExcerpt, strings.TrimSpace(query.Get("format")), nil
}

func validTimeFilter(value string) bool {
	if _, err := time.Parse(time.RFC3339, value); err == nil {
		return true
	}
	_, err := time.Parse("2006-01-02", value)
	return err == nil
}

func routingDatasetTSV(rows []db.RoutingDatasetRow) string {
	records := make([][]string, 0, len(rows)+1)
	records = append(records, []string{
		"event_id", "turn_id", "session_id", "agent_id", "channel", "selected_model",
		"strategy", "primary_model", "override_model", "complexity", "user_excerpt",
		"candidates_json", "attribution", "metascore_json", "features_json", "schema_version",
		"decision_at", "total_tokens_in", "total_tokens_out", "total_cost", "inference_count",
		"any_cached", "avg_latency_ms", "avg_quality_score", "any_escalation",
	})
	for _, row := range rows {
		records = append(records, []string{
			row.EventID,
			row.TurnID,
			row.SessionID,
			row.AgentID,
			row.Channel,
			row.SelectedModel,
			row.Strategy,
			row.PrimaryModel,
			derefString(row.OverrideModel),
			derefString(row.Complexity),
			row.UserExcerpt,
			row.CandidatesJSON,
			derefString(row.Attribution),
			derefString(row.MetascoreJSON),
			derefString(row.FeaturesJSON),
			strconv.Itoa(row.SchemaVersion),
			row.DecisionAt,
			strconv.FormatInt(row.TotalTokensIn, 10),
			strconv.FormatInt(row.TotalTokensOut, 10),
			strconv.FormatFloat(row.TotalCost, 'f', -1, 64),
			strconv.FormatInt(row.InferenceCount, 10),
			strconv.FormatBool(row.AnyCached),
			formatOptionalFloat(row.AvgLatencyMS),
			formatOptionalFloat(row.AvgQualityScore),
			strconv.FormatBool(row.AnyEscalation),
		})
	}

	var b strings.Builder
	for _, record := range records {
		for i, field := range record {
			if i > 0 {
				b.WriteByte('\t')
			}
			b.WriteString(strings.ReplaceAll(field, "\t", " "))
		}
		b.WriteByte('\n')
	}
	return b.String()
}

func derefString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func formatOptionalFloat(value *float64) string {
	if value == nil {
		return ""
	}
	return strconv.FormatFloat(*value, 'f', -1, 64)
}
