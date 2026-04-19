package routes

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/rs/zerolog/log"

	"roboticus/internal/core"
	"roboticus/internal/db"
	"roboticus/internal/llm"
	"roboticus/internal/pipeline"
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

// ExerciseModel runs the exercise matrix against a specific model through the
// same pipeline-owned request path the CLI uses. Baselines must reflect real
// runtime behavior, not a stripped direct-LLM bypass.
func ExerciseModel(p pipeline.Runner, store *db.Store, cfg *core.Config, agentName string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Model      string `json:"model"`
			RunID      string `json:"run_id,omitempty"`     // Caller-provided run ID for grouping results.
			Iterations int    `json:"iterations,omitempty"` // Optional parity with CLI exercise.
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
		if req.Iterations < 1 {
			req.Iterations = 1
		}

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
		promptResults := make([]promptResult, 0, len(llm.ExerciseMatrix)*req.Iterations)

		onPrompt := func(o llm.PromptOutcome) {
			pr := promptResult{
				Intent:     o.Prompt.Intent.String(),
				Complexity: o.Prompt.Complexity.String(),
				Prompt:     o.Prompt.Prompt,
				Content:    o.Content,
				Quality:    o.Quality,
				LatencyMs:  o.LatencyMs,
				Passed:     o.Passed,
			}
			if o.Err != nil {
				pr.Error = o.Err.Error()
			}
			promptResults = append(promptResults, pr)
			if store == nil {
				return
			}
			errMsg := ""
			if o.Err != nil {
				errMsg = o.Err.Error()
			}
			_ = db.InsertExerciseResult(context.Background(), store, db.ExerciseResultRow{
				ID:          db.NewID(),
				RunID:       runID,
				Model:       req.Model,
				IntentClass: o.Prompt.Intent.String(),
				Complexity:  o.Prompt.Complexity.String(),
				Prompt:      o.Prompt.Prompt,
				Content:     o.Content,
				Quality:     o.Quality,
				LatencyMs:   o.LatencyMs,
				Passed:      o.Passed,
				ErrorMsg:    errMsg,
			})
		}

		report, err := llm.ExerciseModels(r.Context(), llm.ExerciseRequest{
			Models:       []string{req.Model},
			Iterations:   req.Iterations,
			SendPrompt:   pipelineExercisePromptSender(p, agentName),
			SendWarmup:   pipelineExerciseWarmupSender(p, agentName),
			OnPrompt:     onPrompt,
			IsLocal:      func(model string) bool { return llm.ExerciseModelIsLocal(cfg, model) },
			ModelTimeout: func(model string) time.Duration { return llm.ExerciseModelTimeout(cfg, model) },
		})
		if err != nil {
			status := http.StatusInternalServerError
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				status = http.StatusRequestTimeout
			}
			writeError(w, status, err.Error())
			return
		}
		modelResult := report.Models[0]

		writeJSON(w, http.StatusOK, map[string]any{
			"model":          req.Model,
			"run_id":         runID,
			"iterations":     req.Iterations,
			"total":          len(promptResults),
			"pass":           modelResult.Pass,
			"fail":           modelResult.Fail,
			"avg_quality":    modelResult.AvgQuality,
			"intent_quality": modelResult.IntentQuality,
			"warmup":         modelResult.Warmup,
			"results":        promptResults,
		})
	}
}

func pipelineExercisePromptSender(p pipeline.Runner, agentName string) llm.ModelSender {
	return func(ctx context.Context, model, content string, timeout time.Duration) (string, int64, error) {
		callCtx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		start := time.Now()
		outcome, err := pipeline.RunPipeline(callCtx, p, pipeline.PresetAPI(), pipeline.Input{
			Content:       content,
			AgentID:       "default",
			AgentName:     agentName,
			Platform:      "api",
			ModelOverride: model,
			NoCache:       true,
			NoEscalate:    true,
		})
		latencyMs := time.Since(start).Milliseconds()
		if err != nil {
			return "", latencyMs, err
		}
		return outcome.Content, latencyMs, nil
	}
}

func pipelineExerciseWarmupSender(p pipeline.Runner, agentName string) llm.WarmupSender {
	sendPrompt := pipelineExercisePromptSender(p, agentName)
	return func(ctx context.Context, model string, timeout time.Duration) llm.WarmupResult {
		_, latencyMs, err := sendPrompt(ctx, model, llm.WarmupPrompt, timeout)
		res := llm.WarmupResult{LatencyMs: latencyMs}
		if err != nil {
			if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
				res.TimedOut = true
			} else {
				res.Err = err
			}
		}
		return res
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

// GetExerciseScorecard returns per-model per-intent quality averages from
// the latest exercise run for each model. The frontend uses this to render a
// quality matrix in the Model Scorecard card.
func GetExerciseScorecard(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		entries := db.ExerciseScorecard(r.Context(), store)
		if entries == nil {
			entries = []db.ExerciseScorecardEntry{}
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"entries": entries,
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
