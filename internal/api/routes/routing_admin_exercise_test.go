package routes

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"roboticus/internal/core"
	"roboticus/internal/db"
	"roboticus/internal/hostresources"
	"roboticus/internal/llm"
	"roboticus/internal/modelstate"
	"roboticus/internal/pipeline"
	"roboticus/testutil"
)

func stubResourceSamplerForTest(t *testing.T) {
	t.Helper()
	restore := hostresources.SetSamplerForTests(func(context.Context) hostresources.Snapshot {
		return hostresources.Snapshot{
			CollectedAt:          "2026-04-20T18:20:00Z",
			CPUPercent:           55.5,
			MemoryAvailableBytes: 9_000_000_000,
			OllamaRSSBytes:       3_000_000_000,
			RoboticusRSSBytes:    200_000_000,
		}
	})
	t.Cleanup(restore)
}

func stubModelStateSamplerForTest(t *testing.T) {
	t.Helper()
	restore := modelstate.SetSamplerForTests(func(_ context.Context, _ *core.Config, model string) modelstate.Snapshot {
		return modelstate.Snapshot{
			CollectedAt:        "2026-04-20T18:20:00Z",
			Model:              model,
			Provider:           "ollama",
			IsLocal:            true,
			ProviderConfigured: true,
			ProviderReachable:  true,
			ModelAvailable:     true,
			ModelLoaded:        true,
			StateClass:         "ready",
		}
	})
	t.Cleanup(restore)
}

type stubExerciseRunner struct {
	inputs []pipeline.Input
}

func (s *stubExerciseRunner) Run(_ context.Context, _ pipeline.Config, input pipeline.Input) (*pipeline.Outcome, error) {
	s.inputs = append(s.inputs, input)
	content := "ok"
	if input.Content == llm.WarmupPrompt {
		content = "ready"
	}
	return &pipeline.Outcome{
		SessionID: "sess",
		MessageID: "msg",
		Content:   content,
		Model:     input.ModelOverride,
	}, nil
}

func TestExerciseModel_UsesPipelineOwnedPath(t *testing.T) {
	stubResourceSamplerForTest(t)
	stubModelStateSamplerForTest(t)
	store := testutil.TempStore(t)
	runner := &stubExerciseRunner{}
	cfg := &core.Config{
		Providers: map[string]core.ProviderConfig{
			"ollama": {IsLocal: true},
		},
	}

	handler := ExerciseModel(runner, store, cfg, "ExerciseBot")
	req := httptest.NewRequest(http.MethodPost, "/api/models/exercise", strings.NewReader(`{"model":"ollama/test-model"}`))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got, want := len(runner.inputs), len(llm.ExerciseMatrix)+2; got != want {
		t.Fatalf("pipeline calls = %d, want %d", got, want)
	}
	for _, in := range runner.inputs {
		if !in.NoCache {
			t.Fatalf("expected NoCache on every exercise request")
		}
		if !in.NoEscalate {
			t.Fatalf("expected NoEscalate on every exercise request")
		}
		if in.ModelOverride != "ollama/test-model" {
			t.Fatalf("model override = %q, want ollama/test-model", in.ModelOverride)
		}
		if in.AgentName != "ExerciseBot" {
			t.Fatalf("agent name = %q, want ExerciseBot", in.AgentName)
		}
	}
	body := jsonBody(t, rec)
	if int(body["total"].(float64)) != len(llm.ExerciseMatrix) {
		t.Fatalf("total = %v, want %d", body["total"], len(llm.ExerciseMatrix))
	}
	warmup := body["warmup"].(map[string]any)
	if skipped, _ := warmup["Skipped"].(bool); skipped {
		t.Fatalf("warmup unexpectedly skipped for local model")
	}

	var persisted int
	row := store.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM exercise_results WHERE model = ?`, "ollama/test-model")
	if err := row.Scan(&persisted); err != nil {
		t.Fatalf("scan exercise_results: %v", err)
	}
	if persisted != len(llm.ExerciseMatrix) {
		t.Fatalf("persisted rows = %d, want %d", persisted, len(llm.ExerciseMatrix))
	}

	var runStatus string
	row = store.QueryRowContext(context.Background(), `SELECT status FROM baseline_runs WHERE run_id = ?`, body["run_id"].(string))
	if err := row.Scan(&runStatus); err != nil {
		t.Fatalf("scan baseline_runs: %v", err)
	}
	if runStatus != "completed" {
		t.Fatalf("baseline run status = %q, want completed", runStatus)
	}
	var (
		startResources   string
		endResources     string
		startModelStates string
		endModelStates   string
		resultStart      string
		resultEnd        string
		resultStateStart string
		resultStateEnd   string
	)
	row = store.QueryRowContext(context.Background(), `SELECT COALESCE(start_resources_json, ''), COALESCE(end_resources_json, ''), COALESCE(start_model_states_json, ''), COALESCE(end_model_states_json, '') FROM baseline_runs WHERE run_id = ?`, body["run_id"].(string))
	if err := row.Scan(&startResources, &endResources, &startModelStates, &endModelStates); err != nil {
		t.Fatalf("scan baseline resource snapshots: %v", err)
	}
	if startResources == "" || endResources == "" || startModelStates == "" || endModelStates == "" {
		t.Fatalf("expected baseline run to persist start/end resource and model-state snapshots")
	}
	row = store.QueryRowContext(context.Background(), `SELECT COALESCE(resource_start_json, ''), COALESCE(resource_end_json, ''), COALESCE(model_state_start_json, ''), COALESCE(model_state_end_json, '') FROM exercise_results WHERE model = ? LIMIT 1`, "ollama/test-model")
	if err := row.Scan(&resultStart, &resultEnd, &resultStateStart, &resultStateEnd); err != nil {
		t.Fatalf("scan exercise snapshots: %v", err)
	}
	if resultStart == "" || resultEnd == "" || resultStateStart == "" || resultStateEnd == "" {
		t.Fatalf("expected exercise results to persist prompt-level resource and model-state snapshots")
	}
}

func TestExerciseModel_SkipsWarmupForCloudModels(t *testing.T) {
	store := testutil.TempStore(t)
	runner := &stubExerciseRunner{}
	cfg := &core.Config{
		Providers: map[string]core.ProviderConfig{
			"openai": {IsLocal: false},
		},
	}

	handler := ExerciseModel(runner, store, cfg, "ExerciseBot")
	req := httptest.NewRequest(http.MethodPost, "/api/models/exercise", strings.NewReader(`{"model":"openai/gpt-4o-mini"}`))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got, want := len(runner.inputs), len(llm.ExerciseMatrix); got != want {
		t.Fatalf("pipeline calls = %d, want %d", got, want)
	}
	body := jsonBody(t, rec)
	warmup := body["warmup"].(map[string]any)
	if skipped, _ := warmup["Skipped"].(bool); !skipped {
		t.Fatalf("warmup should be skipped for cloud model")
	}
}

func TestExerciseModel_FiltersSingleIntent(t *testing.T) {
	store := testutil.TempStore(t)
	runner := &stubExerciseRunner{}
	cfg := &core.Config{
		Providers: map[string]core.ProviderConfig{
			"ollama": {IsLocal: true},
		},
	}

	handler := ExerciseModel(runner, store, cfg, "ExerciseBot")
	req := httptest.NewRequest(http.MethodPost, "/api/models/exercise", strings.NewReader(`{"model":"ollama/test-model","intent_class":"tool_use"}`))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	filteredCount := 0
	for _, prompt := range llm.ExerciseMatrix {
		if prompt.Intent == llm.IntentToolUse {
			filteredCount++
		}
	}
	if got, want := len(runner.inputs), filteredCount+2; got != want {
		t.Fatalf("pipeline calls = %d, want %d", got, want)
	}
	body := jsonBody(t, rec)
	if got := body["intent_class"]; got != "TOOL_USE" {
		t.Fatalf("intent_class = %v, want TOOL_USE", got)
	}
	if int(body["total"].(float64)) != filteredCount {
		t.Fatalf("total = %v, want %d", body["total"], filteredCount)
	}
	var persisted int
	row := store.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM exercise_results WHERE model = ? AND intent_class = ?`, "ollama/test-model", "TOOL_USE")
	if err := row.Scan(&persisted); err != nil {
		t.Fatalf("scan exercise_results: %v", err)
	}
	if persisted != filteredCount {
		t.Fatalf("persisted rows = %d, want %d", persisted, filteredCount)
	}
}

func TestExerciseModel_FiltersExactPromptRowAndSkipsWarmup(t *testing.T) {
	store := testutil.TempStore(t)
	runner := &stubExerciseRunner{}
	cfg := &core.Config{
		Providers: map[string]core.ProviderConfig{
			"ollama": {IsLocal: true},
		},
	}

	handler := ExerciseModel(runner, store, cfg, "ExerciseBot")
	req := httptest.NewRequest(http.MethodPost, "/api/models/exercise", strings.NewReader(`{"model":"ollama/test-model","prompt_row":"TOOL_USE:C2","warmup_mode":"skip"}`))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got, want := len(runner.inputs), 1; got != want {
		t.Fatalf("pipeline calls = %d, want %d", got, want)
	}
	wantPrompt := "Look up how many sessions were created today by querying the database."
	if runner.inputs[0].Content != wantPrompt {
		t.Fatalf("prompt = %q, want %q", runner.inputs[0].Content, wantPrompt)
	}
	body := jsonBody(t, rec)
	if int(body["total"].(float64)) != 1 {
		t.Fatalf("total = %v, want 1", body["total"])
	}
	warmup := body["warmup"].(map[string]any)
	if skipped, _ := warmup["Skipped"].(bool); !skipped {
		t.Fatalf("warmup should be skipped by request")
	}
	var persisted int
	row := store.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM exercise_results WHERE model = ? AND intent_class = ? AND complexity = ?`, "ollama/test-model", "TOOL_USE", "moderate")
	if err := row.Scan(&persisted); err != nil {
		t.Fatalf("scan exercise_results: %v", err)
	}
	if persisted != 1 {
		t.Fatalf("persisted rows = %d, want 1", persisted)
	}
}

func TestExerciseModel_RejectsUnknownIntent(t *testing.T) {
	store := testutil.TempStore(t)
	runner := &stubExerciseRunner{}
	cfg := &core.Config{}

	handler := ExerciseModel(runner, store, cfg, "ExerciseBot")
	req := httptest.NewRequest(http.MethodPost, "/api/models/exercise", strings.NewReader(`{"model":"ollama/test-model","intent_class":"not_real"}`))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if len(runner.inputs) != 0 {
		t.Fatalf("pipeline calls = %d, want 0", len(runner.inputs))
	}
}

func TestExerciseRunLifecycleRoutes_PersistHistory(t *testing.T) {
	stubResourceSamplerForTest(t)
	stubModelStateSamplerForTest(t)
	store := testutil.TempStore(t)

	start := StartExerciseRun(store, &core.Config{Models: core.ModelsConfig{Policy: map[string]core.ModelPolicyConfig{}}})
	req := httptest.NewRequest(http.MethodPost, "/api/models/exercise/runs", strings.NewReader(`{"initiator":"cli","models":["ollama/gemma4","ollama/phi4-mini:latest"],"iterations":2,"config_fingerprint":"abc","git_revision":"deadbeef"}`))
	rec := httptest.NewRecorder()
	start.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("start status = %d, want 200", rec.Code)
	}
	body := jsonBody(t, rec)
	runID, _ := body["run_id"].(string)
	if runID == "" {
		t.Fatal("missing run_id")
	}

	appendResult := AppendExerciseRunResult(store)
	req = httptest.NewRequest(http.MethodPost, "/api/models/exercise/runs/"+runID+"/results", strings.NewReader(`{"turn_id":"turn-hello","model":"ollama/gemma4","intent_class":"CONVERSATION","complexity":"simple","prompt":"Say hello.","content":"hello","quality":0.9,"latency_ms":1234,"passed":true,"result_class":"clean_pass"}`))
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, func() *chi.Context {
		routeCtx := chi.NewRouteContext()
		routeCtx.URLParams.Add("runID", runID)
		return routeCtx
	}()))
	rec = httptest.NewRecorder()
	appendResult.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("append status = %d, want 200", rec.Code)
	}
	var persistedTurnID, persistedResultClass string
	row := store.QueryRowContext(context.Background(), `SELECT COALESCE(turn_id, ''), COALESCE(result_class, '') FROM exercise_results WHERE run_id = ? LIMIT 1`, runID)
	if err := row.Scan(&persistedTurnID, &persistedResultClass); err != nil {
		t.Fatalf("scan persisted result identity: %v", err)
	}
	if persistedTurnID != "turn-hello" {
		t.Fatalf("exercise_results.turn_id = %q, want turn-hello", persistedTurnID)
	}
	if persistedResultClass != "clean_pass" {
		t.Fatalf("exercise_results.result_class = %q, want clean_pass", persistedResultClass)
	}

	complete := CompleteExerciseRun(store, &core.Config{Providers: map[string]core.ProviderConfig{
		"ollama": {URL: "http://localhost:11434", Format: "ollama", IsLocal: true},
	}})
	req = httptest.NewRequest(http.MethodPost, "/api/models/exercise/runs/"+runID+"/complete", strings.NewReader(`{"status":"completed","models":["ollama/gemma4","ollama/phi4-mini:latest"]}`))
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, func() *chi.Context {
		routeCtx := chi.NewRouteContext()
		routeCtx.URLParams.Add("runID", runID)
		return routeCtx
	}()))
	rec = httptest.NewRecorder()
	complete.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("complete status = %d, want 200", rec.Code)
	}

	var (
		status           string
		results          int
		startResources   string
		endResources     string
		startModelStates string
		endModelStates   string
	)
	if err := store.QueryRowContext(context.Background(), `SELECT status, COALESCE(start_resources_json, ''), COALESCE(end_resources_json, ''), COALESCE(start_model_states_json, ''), COALESCE(end_model_states_json, '') FROM baseline_runs WHERE run_id = ?`, runID).Scan(&status, &startResources, &endResources, &startModelStates, &endModelStates); err != nil {
		t.Fatalf("query baseline_runs: %v", err)
	}
	if status != "completed" {
		t.Fatalf("status = %q, want completed", status)
	}
	if startResources == "" || endResources == "" || startModelStates == "" || endModelStates == "" {
		t.Fatalf("expected baseline run lifecycle routes to persist start/end resource and model-state snapshots")
	}
	if err := store.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM exercise_results WHERE run_id = ?`, runID).Scan(&results); err != nil {
		t.Fatalf("query exercise_results: %v", err)
	}
	if results != 1 {
		t.Fatalf("results = %d, want 1", results)
	}
}

func TestRescoreExerciseResults_ClassifiesLegacyTransportErrors(t *testing.T) {
	store := testutil.TempStore(t)
	ctx := context.Background()
	if err := db.InsertBaselineRun(ctx, store, db.BaselineRunRow{
		RunID:      "run-legacy",
		Initiator:  "test",
		Status:     "completed",
		ModelCount: 1,
		Models:     []string{"ollama/broken"},
		Iterations: 1,
	}); err != nil {
		t.Fatalf("InsertBaselineRun: %v", err)
	}
	if err := db.InsertExerciseResult(ctx, store, db.ExerciseResultRow{
		ID:          "row-legacy-error",
		RunID:       "run-legacy",
		Model:       "ollama/broken",
		IntentClass: "TOOL_USE",
		Complexity:  "trivial",
		Prompt:      "What is 2 + 2?",
		Quality:     0,
		LatencyMs:   12,
		Passed:      false,
		ErrorMsg:    "API error: internal error",
	}); err != nil {
		t.Fatalf("InsertExerciseResult: %v", err)
	}

	handler := RescoreExerciseResults(store)
	req := httptest.NewRequest(http.MethodPost, "/api/models/exercise/rescore", strings.NewReader(`{"models":["ollama/broken"]}`))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("rescore status = %d, want 200", rec.Code)
	}

	var quality float64
	var passed int
	var resultClass string
	if err := store.QueryRowContext(ctx, `SELECT quality, passed, COALESCE(result_class, '') FROM exercise_results WHERE id = ?`, "row-legacy-error").Scan(&quality, &passed, &resultClass); err != nil {
		t.Fatalf("query rescored row: %v", err)
	}
	if quality != 0 || passed != 0 || resultClass != string(llm.ExerciseOutcomeTransportError) {
		t.Fatalf("rescored row = quality %.2f passed %d class %q, want 0/0/transport_error", quality, passed, resultClass)
	}

	entries := db.ExerciseScorecard(ctx, store)
	if len(entries) != 0 {
		t.Fatalf("legacy transport error should be validity-only scorecard evidence, got %#v", entries)
	}
}

func TestRescoreExerciseResults_ClassifiesLegacyBlankZeroFailuresAsAmbiguous(t *testing.T) {
	store := testutil.TempStore(t)
	ctx := context.Background()
	if err := db.InsertBaselineRun(ctx, store, db.BaselineRunRow{
		RunID:      "run-legacy-blank",
		Initiator:  "test",
		Status:     "completed",
		ModelCount: 1,
		Models:     []string{"ollama/broken"},
		Iterations: 1,
	}); err != nil {
		t.Fatalf("InsertBaselineRun: %v", err)
	}
	if err := db.InsertExerciseResult(ctx, store, db.ExerciseResultRow{
		ID:          "row-legacy-blank",
		RunID:       "run-legacy-blank",
		Model:       "ollama/broken",
		IntentClass: "TOOL_USE",
		Complexity:  "trivial",
		Prompt:      "What is 2 + 2?",
		Quality:     0,
		LatencyMs:   12,
		Passed:      false,
	}); err != nil {
		t.Fatalf("InsertExerciseResult: %v", err)
	}

	handler := RescoreExerciseResults(store)
	req := httptest.NewRequest(http.MethodPost, "/api/models/exercise/rescore", strings.NewReader(`{"models":["ollama/broken"]}`))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("rescore status = %d, want 200", rec.Code)
	}

	var quality float64
	var passed int
	var resultClass string
	if err := store.QueryRowContext(ctx, `SELECT quality, passed, COALESCE(result_class, '') FROM exercise_results WHERE id = ?`, "row-legacy-blank").Scan(&quality, &passed, &resultClass); err != nil {
		t.Fatalf("query rescored row: %v", err)
	}
	if quality != 0 || passed != 0 || resultClass != string(llm.ExerciseOutcomeValidityAmbiguous) {
		t.Fatalf("rescored row = quality %.2f passed %d class %q, want 0/0/validity_ambiguous", quality, passed, resultClass)
	}

	entries := db.ExerciseScorecard(ctx, store)
	if len(entries) != 0 {
		t.Fatalf("legacy blank-zero failure should be validity-only scorecard evidence, got %#v", entries)
	}
}

func TestRescoreExerciseResults_AppliesDefaultQualityFloor(t *testing.T) {
	store := testutil.TempStore(t)
	ctx := context.Background()
	if err := db.InsertBaselineRun(ctx, store, db.BaselineRunRow{
		RunID:      "run-low-quality",
		Initiator:  "test",
		Status:     "completed",
		ModelCount: 1,
		Models:     []string{"model-low"},
		Iterations: 1,
	}); err != nil {
		t.Fatalf("InsertBaselineRun: %v", err)
	}
	lowQualityResponse := "tool_call tool_call tool_call: this response intentionally avoids the requested answer and keeps expanding with irrelevant procedural language instead of returning the fact. It is verbose enough to violate the concise direct-fact contract and contains prohibited tool-call wording."
	if err := db.InsertExerciseResult(ctx, store, db.ExerciseResultRow{
		ID:          "row-low-quality",
		RunID:       "run-low-quality",
		Model:       "model-low",
		IntentClass: "TOOL_USE",
		Complexity:  "trivial",
		Prompt:      "What is 2 + 2?",
		Content:     lowQualityResponse,
		Quality:     0.90,
		LatencyMs:   12,
		Passed:      true,
		ResultClass: string(llm.ExerciseOutcomeCleanPass),
	}); err != nil {
		t.Fatalf("InsertExerciseResult: %v", err)
	}

	handler := RescoreExerciseResults(store)
	req := httptest.NewRequest(http.MethodPost, "/api/models/exercise/rescore", strings.NewReader(`{"models":["model-low"]}`))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("rescore status = %d, want 200", rec.Code)
	}

	var quality float64
	var passed int
	var resultClass string
	if err := store.QueryRowContext(ctx, `SELECT quality, passed, COALESCE(result_class, '') FROM exercise_results WHERE id = ?`, "row-low-quality").Scan(&quality, &passed, &resultClass); err != nil {
		t.Fatalf("query rescored row: %v", err)
	}
	wantQuality := llm.ScoreExerciseResponse(llm.ResolveExercisePrompt("What is 2 + 2?", llm.IntentToolUse, llm.ComplexityTrivial), lowQualityResponse)
	if wantQuality >= llm.DefaultExercisePassQualityFloor {
		t.Fatalf("test fixture scored %.2f; need below %.2f", wantQuality, llm.DefaultExercisePassQualityFloor)
	}
	if quality != wantQuality || passed != 0 || resultClass != string(llm.ExerciseOutcomeQualityGateFailure) {
		t.Fatalf("rescored row = quality %.2f passed %d class %q, want %.2f/0/quality_gate_failure", quality, passed, resultClass, wantQuality)
	}

	entries := db.ExerciseScorecard(ctx, store)
	if len(entries) != 1 {
		t.Fatalf("scorecard entries = %d, want 1", len(entries))
	}
	if entries[0].AvgQuality != wantQuality {
		t.Fatalf("scorecard quality = %.2f, want preserved low-quality evidence %.2f", entries[0].AvgQuality, wantQuality)
	}
}

func TestStartExerciseRun_RejectsDisabledModelByPolicy(t *testing.T) {
	store := testutil.TempStore(t)
	if err := db.UpsertModelPolicy(context.Background(), store, db.ModelPolicyRow{
		Model:             "ollama/qwen2.5:32b",
		State:             llm.ModelStateDisabled,
		PrimaryReasonCode: "latency_nonviable",
		HumanReason:       "Disabled on this hardware.",
		Source:            "operator_policy",
	}); err != nil {
		t.Fatalf("UpsertModelPolicy: %v", err)
	}

	start := StartExerciseRun(store, &core.Config{Models: core.ModelsConfig{Policy: map[string]core.ModelPolicyConfig{}}})
	req := httptest.NewRequest(http.MethodPost, "/api/models/exercise/runs", strings.NewReader(`{"initiator":"cli","models":["ollama/qwen2.5:32b"]}`))
	rec := httptest.NewRecorder()
	start.ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", rec.Code)
	}
}

func TestModelPolicyCRUDRoutes_PersistAndDelete(t *testing.T) {
	store := testutil.TempStore(t)
	cfg := &core.Config{Models: core.ModelsConfig{Policy: map[string]core.ModelPolicyConfig{}}}

	put := UpsertModelPolicy(store, cfg, nil)
	req := httptest.NewRequest(http.MethodPut, "/api/models/policies", strings.NewReader(`{"model":"ollama/qwen2.5:32b","state":"disabled","primary_reason_code":"latency_nonviable","reason_codes":["latency_nonviable","provider_instability"],"human_reason":"Disabled on this hardware.","evidence_refs":["baseline:run-1"],"source":"operator_policy"}`))
	rec := httptest.NewRecorder()
	put.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("put status = %d, want 200", rec.Code)
	}

	list := ListModelPolicies(store, cfg)
	req = httptest.NewRequest(http.MethodGet, "/api/models/policies", nil)
	rec = httptest.NewRecorder()
	list.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list status = %d, want 200", rec.Code)
	}
	body := jsonBody(t, rec)
	persisted, _ := body["persisted"].([]any)
	if len(persisted) != 1 {
		t.Fatalf("persisted len = %d, want 1", len(persisted))
	}

	del := DeleteModelPolicy(store, cfg, nil)
	req = httptest.NewRequest(http.MethodDelete, "/api/models/policies?model=ollama/qwen2.5:32b", nil)
	rec = httptest.NewRecorder()
	del.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete status = %d, want 200", rec.Code)
	}

	if got := db.ListModelPolicies(context.Background(), store); len(got) != 0 {
		t.Fatalf("remaining policies = %d, want 0", len(got))
	}
}
