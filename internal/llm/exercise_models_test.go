package llm

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"roboticus/internal/modelstate"
)

// fakeSender returns the same canned response every time. Records all
// dispatched prompts in order so tests can inspect call patterns.
type fakeSender struct {
	response  string
	latencyMs int64
	err       error
	prompts   []string // captured, in dispatch order
	model     string   // captured: should stay consistent per call
}

func (f *fakeSender) promptSender(ctx context.Context, model, content string, timeout time.Duration) (string, int64, error) {
	f.prompts = append(f.prompts, content)
	f.model = model
	return f.response, f.latencyMs, f.err
}

func (f *fakeSender) warmupSender(ctx context.Context, model string, timeout time.Duration) WarmupResult {
	return WarmupResult{LatencyMs: f.latencyMs}
}

// TestExerciseModels_Rejects_EmptyInputs pins the validation contract.
// A usage mistake (nil sender, empty list) MUST error — not silently
// produce an empty report or nil-deref. These are caller bugs, not
// data conditions.
func TestExerciseModels_Rejects_EmptyInputs(t *testing.T) {
	f := &fakeSender{response: "ok", latencyMs: 100}
	base := ExerciseRequest{
		Models:       []string{"m"},
		SendPrompt:   f.promptSender,
		SendWarmup:   f.warmupSender,
		IsLocal:      func(string) bool { return true },
		ModelTimeout: func(string) time.Duration { return time.Second },
	}

	cases := []struct {
		name   string
		mutate func(*ExerciseRequest)
		wantIn string
	}{
		{"empty models", func(r *ExerciseRequest) { r.Models = nil }, "Models list is empty"},
		{"nil SendPrompt", func(r *ExerciseRequest) { r.SendPrompt = nil }, "SendPrompt is required"},
		{"nil SendWarmup", func(r *ExerciseRequest) { r.SendWarmup = nil }, "SendWarmup is required"},
		{"nil IsLocal", func(r *ExerciseRequest) { r.IsLocal = nil }, "IsLocal is required"},
		{"nil ModelTimeout", func(r *ExerciseRequest) { r.ModelTimeout = nil }, "ModelTimeout is required"},
		{"invalid intent filter", func(r *ExerciseRequest) {
			invalid := IntentClass(999)
			r.IntentFilter = &invalid
		}, "invalid IntentFilter"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := base
			tc.mutate(&req)
			_, err := ExerciseModels(context.Background(), req)
			if err == nil {
				t.Fatalf("expected error containing %q; got nil", tc.wantIn)
			}
			if !strings.Contains(err.Error(), tc.wantIn) {
				t.Fatalf("error = %q; want substring %q", err.Error(), tc.wantIn)
			}
		})
	}
}

func TestExerciseModels_FiltersToSingleIntent(t *testing.T) {
	calls := 0
	promptSender := func(ctx context.Context, model, content string, timeout time.Duration) (string, int64, error) {
		calls++
		return "ok", 1, nil
	}
	warmupSender := func(ctx context.Context, model string, timeout time.Duration) WarmupResult {
		return WarmupResult{LatencyMs: 1}
	}
	intent := IntentToolUse

	req := ExerciseRequest{
		Models:       []string{"m"},
		IntentFilter: &intent,
		Iterations:   2,
		SendPrompt:   promptSender,
		SendWarmup:   warmupSender,
		IsLocal:      func(string) bool { return true },
		ModelTimeout: func(string) time.Duration { return time.Second },
	}

	report, err := ExerciseModels(context.Background(), req)
	if err != nil {
		t.Fatalf("ExerciseModels: %v", err)
	}

	expectedPrompts := len(filterExerciseMatrix(intent)) * req.Iterations
	if calls != expectedPrompts {
		t.Fatalf("prompt calls = %d, want %d", calls, expectedPrompts)
	}
	if len(report.Models) != 1 {
		t.Fatalf("model results = %d, want 1", len(report.Models))
	}
	if len(report.Models[0].IntentQuality) != 1 {
		t.Fatalf("intent quality entries = %d, want 1", len(report.Models[0].IntentQuality))
	}
	if _, ok := report.Models[0].IntentQuality[intent.String()]; !ok {
		t.Fatalf("missing intent quality for %s", intent.String())
	}
}

// TestExerciseModels_DispatchesWarmupThenMatrix is the core orchestration
// invariant: for each model, warm-up runs first, then the scored matrix
// runs N iterations × matrix-length times. No interleaving, no
// warm-up-in-the-middle, no skipping warm-up.
func TestExerciseModels_DispatchesWarmupThenMatrix(t *testing.T) {
	// Track call ordering via a composite log so we can see interleaving.
	var callLog []string

	warmupSender := func(ctx context.Context, model string, timeout time.Duration) WarmupResult {
		callLog = append(callLog, "warmup:"+model)
		return WarmupResult{LatencyMs: 50}
	}
	promptSender := func(ctx context.Context, model, content string, timeout time.Duration) (string, int64, error) {
		snippet := content
		if len(snippet) > 10 {
			snippet = snippet[:10]
		}
		callLog = append(callLog, "prompt:"+model+":"+snippet)
		return "ok response", 100, nil
	}

	req := ExerciseRequest{
		Models:       []string{"m1", "m2"},
		Iterations:   1,
		SendPrompt:   promptSender,
		SendWarmup:   warmupSender,
		IsLocal:      func(string) bool { return true },
		ModelTimeout: func(string) time.Duration { return time.Second },
		Progress:     &bytes.Buffer{},
	}

	_, err := ExerciseModels(context.Background(), req)
	if err != nil {
		t.Fatalf("ExerciseModels: %v", err)
	}

	// Expected structure: for each model, exactly 2 warmup calls (cold +
	// warm-transition) before any prompt call, and then len(ExerciseMatrix)
	// prompt calls, all with the same model tag.
	matrixLen := len(ExerciseMatrix)
	if matrixLen == 0 {
		t.Skip("ExerciseMatrix is empty in this build; skipping dispatch ordering check")
	}

	i := 0
	for _, model := range []string{"m1", "m2"} {
		// Two warm-up calls first.
		for w := 0; w < 2; w++ {
			if i >= len(callLog) || callLog[i] != "warmup:"+model {
				t.Fatalf("at index %d: expected warmup:%s; got %q (full log: %v)", i, model, callLog[i], callLog)
			}
			i++
		}
		// Then matrixLen prompt calls, all with this model.
		for p := 0; p < matrixLen; p++ {
			if i >= len(callLog) {
				t.Fatalf("call log ended early at index %d; expected %d prompt calls for %s", i, matrixLen, model)
			}
			if !strings.HasPrefix(callLog[i], "prompt:"+model+":") {
				t.Fatalf("at index %d: expected prompt:%s:*; got %q", i, model, callLog[i])
			}
			i++
		}
	}
}

// TestExerciseModels_IterationsMultiplier confirms -n N produces
// N × matrix-length scored calls, not N + matrix-length or other
// off-by-one shapes.
func TestExerciseModels_IterationsMultiplier(t *testing.T) {
	calls := 0
	promptSender := func(ctx context.Context, model, content string, timeout time.Duration) (string, int64, error) {
		calls++
		return "ok", 1, nil
	}
	warmupSender := func(ctx context.Context, model string, timeout time.Duration) WarmupResult {
		return WarmupResult{LatencyMs: 1}
	}

	const iterations = 3
	req := ExerciseRequest{
		Models:       []string{"m"},
		Iterations:   iterations,
		SendPrompt:   promptSender,
		SendWarmup:   warmupSender,
		IsLocal:      func(string) bool { return true },
		ModelTimeout: func(string) time.Duration { return time.Second },
	}
	_, err := ExerciseModels(context.Background(), req)
	if err != nil {
		t.Fatalf("ExerciseModels: %v", err)
	}

	want := iterations * len(ExerciseMatrix)
	if calls != want {
		t.Fatalf("prompt calls = %d; want iterations(%d) × matrix(%d) = %d", calls, iterations, len(ExerciseMatrix), want)
	}
}

// TestExerciseModels_SkipsWarmupForCloudModels pins the IsLocal-driven
// warm-up skip: cloud models get zero warm-up calls, local models get
// two.
func TestExerciseModels_SkipsWarmupForCloudModels(t *testing.T) {
	warmupCalls := 0
	warmupSender := func(ctx context.Context, model string, timeout time.Duration) WarmupResult {
		warmupCalls++
		return WarmupResult{LatencyMs: 1}
	}
	promptSender := func(ctx context.Context, model, content string, timeout time.Duration) (string, int64, error) {
		return "ok", 1, nil
	}

	req := ExerciseRequest{
		Models:       []string{"cloud-model", "local-model"},
		Iterations:   1,
		SendPrompt:   promptSender,
		SendWarmup:   warmupSender,
		IsLocal:      func(m string) bool { return m == "local-model" },
		ModelTimeout: func(string) time.Duration { return time.Second },
	}

	_, err := ExerciseModels(context.Background(), req)
	if err != nil {
		t.Fatalf("ExerciseModels: %v", err)
	}
	// Only local-model should have triggered warm-up (2 calls).
	if warmupCalls != 2 {
		t.Fatalf("warmup calls = %d; want 2 (cloud model must skip warmup)", warmupCalls)
	}
}

// TestExerciseModels_CapturesTransportErrors pins the failure-counting
// contract: a transport error surfaces on the per-model Fail counter
// and is passed through to the OnPrompt callback, NOT returned as an
// orchestrator-level error. Individual call failures are DATA.
func TestExerciseModels_CapturesTransportErrors(t *testing.T) {
	promptSender := func(ctx context.Context, model, content string, timeout time.Duration) (string, int64, error) {
		return "", 0, errString("simulated transport failure")
	}
	warmupSender := func(ctx context.Context, model string, timeout time.Duration) WarmupResult {
		return WarmupResult{LatencyMs: 1}
	}

	var seenErrs int
	onPrompt := func(o PromptOutcome) {
		if o.Err != nil {
			seenErrs++
		}
	}

	req := ExerciseRequest{
		Models:       []string{"broken-model"},
		Iterations:   1,
		SendPrompt:   promptSender,
		SendWarmup:   warmupSender,
		OnPrompt:     onPrompt,
		IsLocal:      func(string) bool { return true },
		ModelTimeout: func(string) time.Duration { return time.Second },
	}

	report, err := ExerciseModels(context.Background(), req)
	if err != nil {
		t.Fatalf("ExerciseModels should NOT return an error for per-prompt failures; got %v", err)
	}
	if len(report.Models) != 1 {
		t.Fatalf("want 1 model result; got %d", len(report.Models))
	}
	mr := report.Models[0]
	if mr.Pass != 0 {
		t.Fatalf("pass = %d; want 0 (every call failed)", mr.Pass)
	}
	if mr.Fail != len(ExerciseMatrix) {
		t.Fatalf("fail = %d; want %d (every prompt should have failed)", mr.Fail, len(ExerciseMatrix))
	}
	if seenErrs != len(ExerciseMatrix) {
		t.Fatalf("OnPrompt saw %d errors; want %d", seenErrs, len(ExerciseMatrix))
	}
}

func TestExerciseModels_CapturesModelStateSnapshots(t *testing.T) {
	promptSender := func(ctx context.Context, model, content string, timeout time.Duration) (string, int64, error) {
		return "ok", 1, nil
	}
	warmupSender := func(ctx context.Context, model string, timeout time.Duration) WarmupResult {
		return WarmupResult{LatencyMs: 1}
	}

	var seen PromptOutcome
	req := ExerciseRequest{
		Models:       []string{"ollama/test-model"},
		IntentFilter: func() *IntentClass { v := IntentToolUse; return &v }(),
		Iterations:   1,
		SendPrompt:   promptSender,
		SendWarmup:   warmupSender,
		OnPrompt: func(o PromptOutcome) {
			seen = o
		},
		SampleModelState: func(ctx context.Context, model string) *modelstate.Snapshot {
			state := modelstate.Snapshot{
				CollectedAt:        "2026-04-21T12:00:00Z",
				Model:              model,
				Provider:           "ollama",
				ProviderConfigured: true,
				ProviderReachable:  true,
				ModelAvailable:     true,
				ModelLoaded:        true,
				StateClass:         "ready",
			}
			return &state
		},
		IsLocal:      func(string) bool { return true },
		ModelTimeout: func(string) time.Duration { return time.Second },
	}

	if _, err := ExerciseModels(context.Background(), req); err != nil {
		t.Fatalf("ExerciseModels: %v", err)
	}
	if seen.ModelStateStart == nil || seen.ModelStateEnd == nil {
		t.Fatalf("expected prompt outcomes to carry model-state snapshots: %+v", seen)
	}
	if seen.ModelStateEnd.StateClass != "ready" {
		t.Fatalf("end state = %q, want ready", seen.ModelStateEnd.StateClass)
	}
}

// TestExerciseModels_ContextCancellation confirms ctx cancel propagates
// through the orchestrator and returns whatever partial data accrued.
// Callers can use this for Ctrl-C behavior during long baseline runs.
func TestExerciseModels_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	callCount := 0
	promptSender := func(ctx context.Context, model, content string, timeout time.Duration) (string, int64, error) {
		callCount++
		if callCount == 2 {
			cancel()
		}
		return "ok", 1, nil
	}
	warmupSender := func(ctx context.Context, model string, timeout time.Duration) WarmupResult {
		return WarmupResult{LatencyMs: 1}
	}

	req := ExerciseRequest{
		Models:       []string{"m"},
		Iterations:   1,
		SendPrompt:   promptSender,
		SendWarmup:   warmupSender,
		IsLocal:      func(string) bool { return true },
		ModelTimeout: func(string) time.Duration { return time.Second },
	}

	report, err := ExerciseModels(ctx, req)
	if err != context.Canceled {
		t.Fatalf("err = %v; want context.Canceled", err)
	}
	// Partial progress: at least one prompt should have landed on the report.
	if len(report.Models) != 1 {
		t.Fatalf("want 1 model in partial report; got %d", len(report.Models))
	}
	if report.Models[0].Pass < 1 {
		t.Fatalf("want at least 1 passed prompt in partial report; got %d", report.Models[0].Pass)
	}
}
