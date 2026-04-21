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

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog/log"

	"roboticus/internal/core"
	"roboticus/internal/db"
	"roboticus/internal/hostresources"
	"roboticus/internal/llm"
	"roboticus/internal/modelstate"
	"roboticus/internal/pipeline"
)

const maxRoutingDatasetLimit = 50000

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

type modelPolicyUpsertRequest struct {
	Model             string   `json:"model"`
	State             string   `json:"state"`
	PrimaryReasonCode string   `json:"primary_reason_code,omitempty"`
	ReasonCodes       []string `json:"reason_codes,omitempty"`
	HumanReason       string   `json:"human_reason,omitempty"`
	EvidenceRefs      []string `json:"evidence_refs,omitempty"`
	Source            string   `json:"source,omitempty"`
}

func snapshotPtr(s hostresources.Snapshot) *hostresources.Snapshot {
	if s.Empty() {
		return nil
	}
	out := s
	return &out
}

func effectiveModelPolicies(ctx context.Context, store *db.Store, cfg *core.Config) map[string]llm.ModelPolicy {
	if cfg == nil {
		return nil
	}
	return llm.EffectiveModelPolicies(ctx, store, cfg.Models.Policy)
}

func benchmarkBlockedModels(models []string, policies map[string]llm.ModelPolicy) []string {
	blocked := make([]string, 0)
	for _, model := range models {
		policy := llm.EffectiveModelPolicy([]string{model}, policies)
		if len(policy) == 0 {
			continue
		}
		if !policy[0].BenchmarkEligible {
			blocked = append(blocked, model)
		}
	}
	return blocked
}

// ListModelPolicies returns persisted and effective model lifecycle policy.
func ListModelPolicies(store *db.Store, cfg *core.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		persisted := db.ListModelPolicies(r.Context(), store)
		effective := effectiveModelPolicies(r.Context(), store, cfg)
		writeJSON(w, http.StatusOK, map[string]any{
			"persisted": persisted,
			"effective": effective,
		})
	}
}

// UpsertModelPolicy persists a model lifecycle policy and returns the merged effective map.
func UpsertModelPolicy(store *db.Store, cfg *core.Config, llmSvc *llm.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req modelPolicyUpsertRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
		if strings.TrimSpace(req.Model) == "" {
			writeError(w, http.StatusBadRequest, "model is required")
			return
		}
		state := strings.TrimSpace(req.State)
		switch state {
		case llm.ModelStateEnabled, llm.ModelStateNiche, llm.ModelStateDisabled, llm.ModelStateBenchmarkOnly:
		default:
			writeError(w, http.StatusBadRequest, "state must be enabled, niche, disabled, or benchmark_only")
			return
		}
		if err := db.UpsertModelPolicy(r.Context(), store, db.ModelPolicyRow{
			Model:             req.Model,
			State:             state,
			PrimaryReasonCode: req.PrimaryReasonCode,
			ReasonCodes:       append([]string(nil), req.ReasonCodes...),
			HumanReason:       req.HumanReason,
			EvidenceRefs:      append([]string(nil), req.EvidenceRefs...),
			Source:            req.Source,
		}); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to persist model policy")
			return
		}
		effective := effectiveModelPolicies(r.Context(), store, cfg)
		if llmSvc != nil {
			llmSvc.ApplyModelPolicies(effective)
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":        true,
			"effective": effective,
		})
	}
}

// DeleteModelPolicy removes a persisted model lifecycle policy override.
func DeleteModelPolicy(store *db.Store, cfg *core.Config, llmSvc *llm.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		model := strings.TrimSpace(r.URL.Query().Get("model"))
		if model == "" {
			writeError(w, http.StatusBadRequest, "model is required")
			return
		}
		if err := db.DeleteModelPolicy(r.Context(), store, model); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to delete model policy")
			return
		}
		effective := effectiveModelPolicies(r.Context(), store, cfg)
		if llmSvc != nil {
			llmSvc.ApplyModelPolicies(effective)
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":        true,
			"effective": effective,
		})
	}
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
			Model       string `json:"model"`
			RunID       string `json:"run_id,omitempty"`       // Caller-provided run ID for grouping results.
			Iterations  int    `json:"iterations,omitempty"`   // Optional parity with CLI exercise.
			IntentClass string `json:"intent_class,omitempty"` // Optional canonical matrix slice.
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
