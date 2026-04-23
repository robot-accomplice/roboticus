package routes

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog/log"

	"roboticus/internal/core"
	"roboticus/internal/db"
	"roboticus/internal/hostresources"
	"roboticus/internal/llm"
	"roboticus/internal/modelstate"
	"roboticus/internal/pipeline"
)

type baselineRunStartRequest struct {
	RunID             string   `json:"run_id,omitempty"`
	Initiator         string   `json:"initiator,omitempty"`
	Models            []string `json:"models"`
	Iterations        int      `json:"iterations,omitempty"`
	ConfigFingerprint string   `json:"config_fingerprint,omitempty"`
	GitRevision       string   `json:"git_revision,omitempty"`
	Notes             string   `json:"notes,omitempty"`
	Force             bool     `json:"force,omitempty"`
}

type baselineRunResultRequest struct {
	Model           string          `json:"model"`
	IntentClass     string          `json:"intent_class"`
	Complexity      string          `json:"complexity"`
	Prompt          string          `json:"prompt"`
	Content         string          `json:"content,omitempty"`
	Quality         float64         `json:"quality"`
	LatencyMs       int64           `json:"latency_ms"`
	Passed          bool            `json:"passed"`
	ErrorMsg        string          `json:"error_msg,omitempty"`
	ResourceStart   json.RawMessage `json:"resource_start,omitempty"`
	ResourceEnd     json.RawMessage `json:"resource_end,omitempty"`
	ModelStateStart json.RawMessage `json:"model_state_start,omitempty"`
	ModelStateEnd   json.RawMessage `json:"model_state_end,omitempty"`
}

type baselineRunCompleteRequest struct {
	Status string   `json:"status,omitempty"`
	Notes  string   `json:"notes,omitempty"`
	Models []string `json:"models,omitempty"`
}

// StartExerciseRun records metadata for a new baseline/exercise run.
func StartExerciseRun(store *db.Store, cfg *core.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req baselineRunStartRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
		if len(req.Models) == 0 {
			writeError(w, http.StatusBadRequest, "models are required")
			return
		}
		if req.RunID == "" {
			req.RunID = db.NewID()
		}
		if req.Initiator == "" {
			req.Initiator = "api"
		}
		if req.Iterations < 1 {
			req.Iterations = 1
		}
		if !req.Force {
			blocked := benchmarkBlockedModels(req.Models, effectiveModelPolicies(r.Context(), store, cfg))
			if len(blocked) > 0 {
				writeError(w, http.StatusConflict, "benchmark blocked by model policy: "+strings.Join(blocked, ", "))
				return
			}
		}
		if err := db.InsertBaselineRun(r.Context(), store, db.BaselineRunRow{
			RunID:             req.RunID,
			Initiator:         req.Initiator,
			Status:            "running",
			ModelCount:        len(req.Models),
			Models:            req.Models,
			Iterations:        req.Iterations,
			ConfigFingerprint: req.ConfigFingerprint,
			GitRevision:       req.GitRevision,
			Notes:             req.Notes,
			StartResources:    snapshotPtr(hostresources.Sample(r.Context())),
			StartModelStates:  modelstate.SampleMany(r.Context(), cfg, req.Models),
		}); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to start exercise run")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"run_id": req.RunID})
	}
}

// AppendExerciseRunResult persists one prompt-level outcome under a baseline run.
func AppendExerciseRunResult(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		runID := chi.URLParam(r, "runID")
		if runID == "" {
			writeError(w, http.StatusBadRequest, "run_id is required")
			return
		}
		var req baselineRunResultRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
		if req.Model == "" || req.IntentClass == "" || req.Complexity == "" || req.Prompt == "" {
			writeError(w, http.StatusBadRequest, "model, intent_class, complexity, and prompt are required")
			return
		}
		if err := db.InsertExerciseResult(r.Context(), store, db.ExerciseResultRow{
			ID:              db.NewID(),
			RunID:           runID,
			Model:           req.Model,
			IntentClass:     req.IntentClass,
			Complexity:      req.Complexity,
			Prompt:          req.Prompt,
			Content:         req.Content,
			Quality:         req.Quality,
			LatencyMs:       req.LatencyMs,
			Passed:          req.Passed,
			ErrorMsg:        req.ErrorMsg,
			ResourceStart:   hostresources.FromJSON(string(req.ResourceStart)),
			ResourceEnd:     hostresources.FromJSON(string(req.ResourceEnd)),
			ModelStateStart: modelstate.FromJSON(string(req.ModelStateStart)),
			ModelStateEnd:   modelstate.FromJSON(string(req.ModelStateEnd)),
		}); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to persist exercise result")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	}
}

// CompleteExerciseRun marks a baseline run completed, failed, or canceled.
func CompleteExerciseRun(store *db.Store, cfg *core.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		runID := chi.URLParam(r, "runID")
		if runID == "" {
			writeError(w, http.StatusBadRequest, "run_id is required")
			return
		}
		var req baselineRunCompleteRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
		if req.Status == "" {
			req.Status = "completed"
		}
		switch req.Status {
		case "completed", "failed", "canceled":
		default:
			writeError(w, http.StatusBadRequest, "status must be completed, failed, or canceled")
			return
		}
		if err := db.CompleteBaselineRun(r.Context(), store, runID, req.Status, req.Notes, snapshotPtr(hostresources.Sample(r.Context())), modelstate.SampleMany(r.Context(), cfg, req.Models)); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to finalize exercise run")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	}
}

// ListExerciseRuns returns recent baseline/exercise runs.
func ListExerciseRuns(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		limit := 20
		if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
			if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
				limit = parsed
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"runs": db.ListBaselineRuns(r.Context(), store, limit),
		})
	}
}

// ExerciseModel runs the exercise matrix against a specific model through the
// same pipeline-owned request path the CLI uses. Baselines must reflect real
// runtime behavior, not a stripped direct-LLM bypass.
func ExerciseModel(p pipeline.Runner, store *db.Store, cfg *core.Config, agentName string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Model       string `json:"model"`
			RunID       string `json:"run_id,omitempty"`
			Iterations  int    `json:"iterations,omitempty"`
			IntentClass string `json:"intent_class,omitempty"`
			Force       bool   `json:"force,omitempty"`
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
		var intentFilter *llm.IntentClass
		if strings.TrimSpace(req.IntentClass) != "" {
			intent, err := llm.ParseIntentClassStrict(req.IntentClass)
			if err != nil {
				writeError(w, http.StatusBadRequest, err.Error())
				return
			}
			intentFilter = &intent
			req.IntentClass = intent.String()
		}
		if !req.Force {
			blocked := benchmarkBlockedModels([]string{req.Model}, effectiveModelPolicies(r.Context(), store, cfg))
			if len(blocked) > 0 {
				writeError(w, http.StatusConflict, "benchmark blocked by model policy: "+strings.Join(blocked, ", "))
				return
			}
		}
		notes := ""
		if req.IntentClass != "" {
			notes = "intent filter: " + req.IntentClass
		}
		if err := db.InsertBaselineRun(r.Context(), store, db.BaselineRunRow{
			RunID:            runID,
			Initiator:        "api",
			Status:           "running",
			ModelCount:       1,
			Models:           []string{req.Model},
			Iterations:       req.Iterations,
			Notes:            notes,
			StartResources:   snapshotPtr(hostresources.Sample(r.Context())),
			StartModelStates: modelstate.SampleMany(r.Context(), cfg, []string{req.Model}),
		}); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to start exercise run")
			return
		}
		defer func() {
			status := "completed"
			notes := ""
			if r.Context().Err() != nil {
				status = "canceled"
				notes = r.Context().Err().Error()
			}
			if err := db.CompleteBaselineRun(context.Background(), store, runID, status, notes, snapshotPtr(hostresources.Sample(context.Background())), modelstate.SampleMany(context.Background(), cfg, []string{req.Model})); err != nil {
				log.Warn().Err(err).Str("run_id", runID).Msg("exercise: failed to finalize API exercise run")
			}
		}()

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
		promptCapacity := len(llm.ExerciseMatrix)
		if intentFilter != nil {
			promptCapacity = 0
			for _, prompt := range llm.ExerciseMatrix {
				if prompt.Intent == *intentFilter {
					promptCapacity++
				}
			}
		}
		promptResults := make([]promptResult, 0, promptCapacity*req.Iterations)

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
				ID:              db.NewID(),
				RunID:           runID,
				Model:           req.Model,
				IntentClass:     o.Prompt.Intent.String(),
				Complexity:      o.Prompt.Complexity.String(),
				Prompt:          o.Prompt.Prompt,
				Content:         o.Content,
				Quality:         o.Quality,
				LatencyMs:       o.LatencyMs,
				Passed:          o.Passed,
				ErrorMsg:        errMsg,
				ResourceStart:   o.ResourceStart,
				ResourceEnd:     o.ResourceEnd,
				ModelStateStart: o.ModelStateStart,
				ModelStateEnd:   o.ModelStateEnd,
			})
		}

		report, err := llm.ExerciseModels(r.Context(), llm.ExerciseRequest{
			Models:       []string{req.Model},
			IntentFilter: intentFilter,
			Iterations:   req.Iterations,
			SendPrompt:   pipelineExercisePromptSender(p, agentName),
			SendWarmup:   pipelineExerciseWarmupSender(p, agentName),
			OnPrompt:     onPrompt,
			SampleModelState: func(ctx context.Context, model string) *modelstate.Snapshot {
				snapshot := modelstate.Sample(ctx, cfg, model)
				if snapshot.Empty() {
					return nil
				}
				return &snapshot
			},
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
			"intent_class":   req.IntentClass,
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
