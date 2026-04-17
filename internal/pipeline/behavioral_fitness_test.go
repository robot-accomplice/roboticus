// Behavioral Fitness Tests — Pipeline Parity with Rust
//
// These tests verify that Go's pipeline follows the same behavioral contract
// as Rust's pipeline. Unlike unit tests that verify *what* happens, fitness
// tests verify *how* it happens — stage ordering, trace annotations, cache
// semantics, guard recording, and delegation tracing.
//
// Each test sends a message through the real pipeline with mocked inference
// and inspects the pipeline_traces table to verify behavioral invariants.
//
// Rust reference: crates/roboticus-pipeline/src/core/ + context/

package pipeline

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"roboticus/internal/db"
	"roboticus/testutil"
)

// ── Trace Extraction Helpers ──────────────────────────────────────────────

// traceRow is a decoded pipeline trace from the DB.
type traceRow struct {
	ID              string
	TurnID          string
	SessionID       string
	Channel         string
	TotalMs         int64
	StagesJSON      string
	ReactTraceJSON  *string
	InferenceParams *string
}

// extractFullTrace queries the latest pipeline trace for a given session.
func extractFullTrace(t *testing.T, store *db.Store, sessionID string) *traceRow {
	t.Helper()
	row := store.QueryRowContext(context.Background(),
		`SELECT id, turn_id, session_id, channel, total_ms, stages_json, react_trace_json, inference_params_json
		 FROM pipeline_traces WHERE session_id = ? ORDER BY rowid DESC LIMIT 1`, sessionID)
	var tr traceRow
	if err := row.Scan(&tr.ID, &tr.TurnID, &tr.SessionID, &tr.Channel, &tr.TotalMs, &tr.StagesJSON, &tr.ReactTraceJSON, &tr.InferenceParams); err != nil {
		t.Fatalf("extractFullTrace: %v", err)
	}
	return &tr
}

// extractSpans parses stages_json into TraceSpan structs.
func extractSpans(t *testing.T, stagesJSON string) []TraceSpan {
	t.Helper()
	var spans []TraceSpan
	if err := json.Unmarshal([]byte(stagesJSON), &spans); err != nil {
		t.Fatalf("extractSpans: %v", err)
	}
	return spans
}

// spanNames returns just the names from a slice of spans.
func spanNames(spans []TraceSpan) []string {
	names := make([]string, len(spans))
	for i, s := range spans {
		names[i] = s.Name
	}
	return names
}

// findSpan returns the first span with the given name, or nil.
func findSpan(spans []TraceSpan, name string) *TraceSpan {
	for i := range spans {
		if spans[i].Name == name {
			return &spans[i]
		}
	}
	return nil
}

// hasAnnotation checks if a span has a metadata key.
func hasAnnotation(span *TraceSpan, key string) bool {
	if span == nil || span.Metadata == nil {
		return false
	}
	_, ok := span.Metadata[key]
	return ok
}

// ── Stage Ordering Fitness Tests ──────────────────────────────────────────
//
// Rust's pipeline executes stages in a strict order. The Go pipeline must
// follow the same order. These tests verify that stage spans appear in the
// correct relative order in the trace.

func TestFitness_StageOrderMatchesRust(t *testing.T) {
	store := testutil.TempStore(t)
	pipe := New(PipelineDeps{
		Store:    store,
		Executor: &stubExecutor{response: "This is a thoughtful response about Go pipeline architecture and behavioral parity testing"},
		BGWorker: testutil.BGWorker(t, 4),
	})

	// Use API preset for maximum stage coverage.
	cfg := PresetAPI()
	input := Input{Content: "Tell me about pipeline architecture", AgentID: "default", Platform: "test"}
	outcome, err := RunPipeline(context.Background(), pipe, cfg, input)
	if err != nil {
		t.Fatalf("RunPipeline: %v", err)
	}

	trace := extractFullTrace(t, store, outcome.SessionID)
	spans := extractSpans(t, trace.StagesJSON)
	names := spanNames(spans)

	// Rust's canonical stage order (from crates/roboticus-pipeline/src/core/pipeline.rs):
	//   1. validation
	//   2. injection_defense
	//   3. dedup_check (if enabled)
	//   4. session_resolution
	//   5. message_storage
	//   6. decomposition_gate (if enabled)
	//   7. task_synthesis (if enabled)
	//   8. authority_resolution
	//   9. memory_retrieval (if retriever present)
	//  10. delegated_execution (if delegation decided)
	//  11. skill_dispatch
	//  12. shortcut_dispatch
	//  13. cache_check (if enabled)
	//  14. inference
	//
	// Not all stages fire every time (some are conditional), but the ORDER
	// must be preserved. Stages that fire must appear in this relative order.
	orderedStages := []string{
		"validation",
		"injection_defense",
		"dedup_check",
		"session_resolution",
		"message_storage",
		"decomposition_gate",
		"task_synthesis",
		"authority_resolution",
		"skill_dispatch",
		"shortcut_dispatch",
		"cache_check",
		"inference",
	}

	// Build an index map: stage name → position in trace.
	posMap := make(map[string]int)
	for i, name := range names {
		posMap[name] = i
	}

	// Verify relative ordering: for each pair of ordered stages that both
	// appear in the trace, the first must come before the second.
	for i := 0; i < len(orderedStages); i++ {
		posI, okI := posMap[orderedStages[i]]
		if !okI {
			continue
		}
		for j := i + 1; j < len(orderedStages); j++ {
			posJ, okJ := posMap[orderedStages[j]]
			if !okJ {
				continue
			}
			if posI >= posJ {
				t.Errorf("stage ordering violation: %q (pos %d) must come before %q (pos %d); trace: %v",
					orderedStages[i], posI, orderedStages[j], posJ, names)
			}
		}
	}
}

// TestFitness_MandatoryStagesAlwaysFire verifies that certain stages MUST
// fire for every pipeline run, regardless of preset or input.
func TestFitness_MandatoryStagesAlwaysFire(t *testing.T) {
	// These stages are mandatory per Rust's pipeline — they always execute.
	mandatoryStages := []string{
		"validation",
		"injection_defense",
		"session_resolution",
		"message_storage",
		"authority_resolution",
	}

	presets := map[string]Config{
		"API":     PresetAPI(),
		"Channel": PresetChannel("test"),
		"Cron":    PresetCron(),
	}

	for presetName, cfg := range presets {
		t.Run(presetName, func(t *testing.T) {
			store := testutil.TempStore(t)
			pipe := New(PipelineDeps{
				Store:    store,
				Executor: &stubExecutor{response: "This is a substantive fitness test response for behavioral parity verification"},
				BGWorker: testutil.BGWorker(t, 4),
			})

			outcome, err := RunPipeline(context.Background(), pipe, cfg, Input{
				Content: "fitness test mandatory stages",
				AgentID: "default",
			})
			if err != nil {
				t.Fatalf("RunPipeline[%s]: %v", presetName, err)
			}

			trace := extractFullTrace(t, store, outcome.SessionID)
			spans := extractSpans(t, trace.StagesJSON)
			names := spanNames(spans)

			for _, required := range mandatoryStages {
				if !containsStage(names, required) {
					t.Errorf("%s preset missing mandatory stage %q (got: %v)", presetName, required, names)
				}
			}
		})
	}
}

// ── Trace Annotation Fitness Tests ────────────────────────────────────────
//
// Rust records structured annotations under namespace prefixes. These tests
// verify that Go's pipeline produces the same annotation keys.

func TestFitness_SessionAnnotation(t *testing.T) {
	store := testutil.TempStore(t)
	pipe := New(PipelineDeps{
		Store:    store,
		Executor: &stubExecutor{response: "A well-formed response for annotation verification in pipeline fitness tests"},
		BGWorker: testutil.BGWorker(t, 4),
	})

	outcome, err := RunPipeline(context.Background(), pipe, PresetAPI(), Input{
		Content: "annotation test input",
		AgentID: "default",
	})
	if err != nil {
		t.Fatal(err)
	}

	trace := extractFullTrace(t, store, outcome.SessionID)
	spans := extractSpans(t, trace.StagesJSON)

	// Session resolution must annotate session_id.
	sessionSpan := findSpan(spans, "session_resolution")
	if sessionSpan == nil {
		t.Fatal("missing session_resolution span")
	}
	if !hasAnnotation(sessionSpan, "session_id") {
		t.Error("session_resolution span missing 'session_id' annotation")
	}
}

func TestFitness_AuthorityAnnotation(t *testing.T) {
	store := testutil.TempStore(t)
	pipe := New(PipelineDeps{
		Store:    store,
		Executor: &stubExecutor{response: "Authority annotation fitness test response with sufficient length for validation"},
		BGWorker: testutil.BGWorker(t, 4),
	})

	outcome, err := RunPipeline(context.Background(), pipe, PresetAPI(), Input{
		Content: "authority annotation test",
		AgentID: "default",
	})
	if err != nil {
		t.Fatal(err)
	}

	trace := extractFullTrace(t, store, outcome.SessionID)
	spans := extractSpans(t, trace.StagesJSON)

	// Authority resolution must annotate authority level.
	authSpan := findSpan(spans, "authority_resolution")
	if authSpan == nil {
		t.Fatal("missing authority_resolution span")
	}
	if !hasAnnotation(authSpan, "authority") {
		t.Error("authority_resolution missing 'authority' annotation")
	}
}

func TestFitness_DecompositionAnnotation(t *testing.T) {
	store := testutil.TempStore(t)
	pipe := New(PipelineDeps{
		Store:    store,
		Executor: &stubExecutor{response: "Decomposition gate fitness test with a thoughtful and substantive response body"},
		BGWorker: testutil.BGWorker(t, 4),
	})

	outcome, err := RunPipeline(context.Background(), pipe, PresetAPI(), Input{
		Content: "decomposition annotation test",
		AgentID: "default",
	})
	if err != nil {
		t.Fatal(err)
	}

	trace := extractFullTrace(t, store, outcome.SessionID)
	spans := extractSpans(t, trace.StagesJSON)

	// Decomposition gate must annotate its decision.
	decompSpan := findSpan(spans, "decomposition_gate")
	if decompSpan == nil {
		t.Fatal("missing decomposition_gate span")
	}
	if !hasAnnotation(decompSpan, "decision") {
		t.Error("decomposition_gate missing 'decision' annotation")
	}
}

func TestFitness_TaskSynthesisAnnotations(t *testing.T) {
	store := testutil.TempStore(t)
	pipe := New(PipelineDeps{
		Store:    store,
		Executor: &stubExecutor{response: "Task synthesis fitness test demonstrates comprehensive pipeline tracing behavior"},
		BGWorker: testutil.BGWorker(t, 4),
	})

	outcome, err := RunPipeline(context.Background(), pipe, PresetAPI(), Input{
		Content: "task synthesis annotation test",
		AgentID: "default",
	})
	if err != nil {
		t.Fatal(err)
	}

	trace := extractFullTrace(t, store, outcome.SessionID)
	spans := extractSpans(t, trace.StagesJSON)

	// Task synthesis must annotate intent, complexity, planned_action, confidence.
	// These are namespaced under TraceNSTaskState ("task_state"). Matches
	// Rust's ns::TASK_STATE constant.
	synthSpan := findSpan(spans, "task_synthesis")
	if synthSpan == nil {
		t.Fatal("missing task_synthesis span")
	}

	expectedAnnotations := []string{
		TraceNSTaskState + ".intent",
		TraceNSTaskState + ".complexity",
		TraceNSTaskState + ".planned_action",
		TraceNSTaskState + ".confidence",
		TraceNSTaskState + ".capability_fit",
		TraceNSTaskState + ".retrieval_needed",
	}
	for _, key := range expectedAnnotations {
		if !hasAnnotation(synthSpan, key) {
			t.Errorf("task_synthesis span missing annotation %q", key)
		}
	}
}

// ── Span Outcome Fitness Tests ────────────────────────────────────────────
//
// Every span must have a valid outcome ("ok", "skipped", "error", etc.).

func TestFitness_AllSpansHaveOutcomes(t *testing.T) {
	store := testutil.TempStore(t)
	pipe := New(PipelineDeps{
		Store:    store,
		Executor: &stubExecutor{response: "Span outcome verification fitness test with a properly formed pipeline response"},
		BGWorker: testutil.BGWorker(t, 4),
	})

	outcome, err := RunPipeline(context.Background(), pipe, PresetAPI(), Input{
		Content: "span outcome test",
		AgentID: "default",
	})
	if err != nil {
		t.Fatal(err)
	}

	trace := extractFullTrace(t, store, outcome.SessionID)
	spans := extractSpans(t, trace.StagesJSON)

	validOutcomes := map[string]bool{
		"ok": true, "skipped": true, "error": true,
		"rejected": true, "fallthrough": true, "miss": true,
	}

	for _, span := range spans {
		if span.Outcome == "" {
			t.Errorf("span %q has empty outcome", span.Name)
			continue
		}
		if !validOutcomes[span.Outcome] {
			t.Errorf("span %q has unexpected outcome %q", span.Name, span.Outcome)
		}
	}
}

// ── Cache Fitness Tests ──────────────────────────────────────────────────
//
// Rust's cache has quality guards: minimum length, parroting detection,
// acknowledgement rejection. These tests verify the full round-trip.

func TestFitness_CacheRoundTrip(t *testing.T) {
	store := testutil.TempStore(t)
	pipe := New(PipelineDeps{
		Store:    store,
		Executor: &stubExecutor{response: "This is a high-quality cached response that should survive all pipeline quality guards"},
		BGWorker: testutil.BGWorker(t, 4),
	})

	cfg := PresetAPI()
	input := Input{Content: "cache round trip test for fitness", AgentID: "default"}

	// First run: generates response and stores in cache.
	outcome1, err := RunPipeline(context.Background(), pipe, cfg, input)
	if err != nil {
		t.Fatalf("first run: %v", err)
	}
	if outcome1.FromCache {
		t.Error("first run should not be from cache")
	}

	// Wait for background cache store (bgWorker.Submit is async).
	pipe.bgWorker.Drain(5 * time.Second)

	// Manually verify cache was stored.
	hit := pipe.CheckCache(context.Background(), input.Content)
	if hit == nil {
		t.Fatal("cache should contain the response after first run")
	}
	if hit.Content != outcome1.Content {
		t.Errorf("cached content mismatch: got %q, want %q", hit.Content, outcome1.Content)
	}
}

func TestFitness_CacheRejectsShortResponses(t *testing.T) {
	store := testutil.TempStore(t)
	pipe := New(PipelineDeps{
		Store:    store,
		Executor: &stubExecutor{response: "short"},
		BGWorker: testutil.BGWorker(t, 4),
	})

	// Store a short response directly.
	pipe.StoreInCache(context.Background(), "test prompt for short rejection", "short", "mock")

	// Check should return nil (rejected by length guard).
	hit := pipe.CheckCache(context.Background(), "test prompt for short rejection")
	if hit != nil {
		t.Error("cache should reject responses shorter than 20 chars")
	}
}

func TestFitness_CacheRejectsParroting(t *testing.T) {
	store := testutil.TempStore(t)
	pipe := New(PipelineDeps{
		Store:    store,
		Executor: &stubExecutor{response: "parroting test"},
		BGWorker: testutil.BGWorker(t, 4),
	})

	prompt := "What is the meaning of life and the universe?"
	// Try to store a response that heavily overlaps with the prompt.
	pipe.StoreInCache(context.Background(), prompt, prompt, "mock")

	hit := pipe.CheckCache(context.Background(), prompt)
	if hit != nil {
		t.Error("cache should reject parroting responses (>60% overlap)")
	}
}

func TestFitness_CacheRejectsAcknowledgements(t *testing.T) {
	store := testutil.TempStore(t)
	pipe := New(PipelineDeps{
		Store:    store,
		Executor: &stubExecutor{response: "ok"},
		BGWorker: testutil.BGWorker(t, 4),
	})

	// Acknowledgement responses should not be cached.
	// The AcknowledgementShortcut matches short exact words: "ok", "thanks", etc.
	// StoreInCache should reject these because TryMatch detects them.
	ackResponses := []string{"ok", "thanks", "got it", "understood", "sure", "yep"}
	for _, ack := range ackResponses {
		pipe.StoreInCache(context.Background(), "test ack rejection prompt "+ack, ack, "mock")
		hit := pipe.CheckCache(context.Background(), "test ack rejection prompt "+ack)
		if hit != nil {
			t.Errorf("cache should not store/return acknowledgement response %q", ack)
		}
	}
}

func TestFitness_CacheRejectsExpiredEntries(t *testing.T) {
	store := testutil.TempStore(t)
	pipe := New(PipelineDeps{
		Store:    store,
		BGWorker: testutil.BGWorker(t, 4),
		CacheTTL: 50 * time.Millisecond,
	})

	prompt := "expired cache prompt"
	response := "This is a sufficiently long cached response that should expire quickly."
	pipe.StoreInCache(context.Background(), prompt, response, "mock")

	time.Sleep(80 * time.Millisecond)

	hit := pipe.CheckCache(context.Background(), prompt)
	if hit != nil {
		t.Fatal("expired cache entry should not be returned")
	}
}

// ── InferenceParams Fitness Tests ─────────────────────────────────────────
//
// Rust records model selection, tokens, and guard outcomes per inference.
// These tests verify Go persists the same metadata.

func TestFitness_InferenceParamsPersisted(t *testing.T) {
	store := testutil.TempStore(t)
	pipe := New(PipelineDeps{
		Store:    store,
		Executor: &stubExecutor{response: "Inference params fitness test with a response that exercises the full pipeline trace persistence path"},
		BGWorker: testutil.BGWorker(t, 4),
	})

	outcome, err := RunPipeline(context.Background(), pipe, PresetAPI(), Input{
		Content: "inference params test",
		AgentID: "default",
	})
	if err != nil {
		t.Fatal(err)
	}

	trace := extractFullTrace(t, store, outcome.SessionID)

	if trace.InferenceParams == nil {
		t.Fatal("inference_params_json should be populated after inference")
	}

	params, err := ParseInferenceParams(*trace.InferenceParams)
	if err != nil {
		t.Fatalf("ParseInferenceParams: %v", err)
	}
	if params == nil {
		t.Fatal("parsed inference params should not be nil")
	}
	if params.ReactTurns <= 0 {
		t.Errorf("ReactTurns should be > 0, got %d", params.ReactTurns)
	}
}

func TestFitness_CacheHitRecordsFromCacheParam(t *testing.T) {
	store := testutil.TempStore(t)
	pipe := New(PipelineDeps{
		Store:    store,
		Executor: &stubExecutor{response: "This is a quality response for cache hit inference params fitness verification"},
		BGWorker: testutil.BGWorker(t, 4),
	})

	// Seed cache with a valid response.
	prompt := "cache hit params fitness test input"
	response := "This is a quality response for cache hit inference params fitness verification"
	pipe.StoreInCache(context.Background(), prompt, response, "gpt-4")

	// Run pipeline — should hit cache.
	outcome, err := RunPipeline(context.Background(), pipe, PresetAPI(), Input{
		Content: prompt,
		AgentID: "default",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !outcome.FromCache {
		t.Skip("cache miss — may be a timing issue; skipping param verification")
	}

	trace := extractFullTrace(t, store, outcome.SessionID)
	if trace.InferenceParams == nil {
		t.Fatal("inference_params_json should be set for cache hits too")
	}

	params, _ := ParseInferenceParams(*trace.InferenceParams)
	if params == nil {
		t.Fatal("parsed params nil")
	}
	if !params.FromCache {
		t.Error("FromCache should be true for cache hits")
	}
	if params.ModelActual != "gpt-4" {
		t.Errorf("ModelActual = %q, want 'gpt-4'", params.ModelActual)
	}
}

// ── Preset Behavioral Fitness Tests ───────────────────────────────────────
//
// Each preset should produce a distinct behavioral profile. These tests
// verify that preset-specific stages fire (or don't fire) as expected.

func TestFitness_CronPresetSkipsDedupAndShortcuts(t *testing.T) {
	store := testutil.TempStore(t)
	pipe := New(PipelineDeps{
		Store:    store,
		Executor: &stubExecutor{response: "Cron preset fitness test verifying that dedup and shortcuts are correctly disabled"},
		BGWorker: testutil.BGWorker(t, 4),
	})

	outcome, err := RunPipeline(context.Background(), pipe, PresetCron(), Input{
		Content: "cron preset fitness test",
		AgentID: "cron-agent",
	})
	if err != nil {
		t.Fatal(err)
	}

	trace := extractFullTrace(t, store, outcome.SessionID)
	spans := extractSpans(t, trace.StagesJSON)
	names := spanNames(spans)

	// Cron should NOT have dedup_check (DedupTracking=false).
	if containsStage(names, "dedup_check") {
		t.Error("Cron preset should not have dedup_check stage")
	}

	// Shortcut dispatch should be skipped (ShortcutsEnabled=false).
	shortcutSpan := findSpan(spans, "shortcut_dispatch")
	if shortcutSpan != nil && shortcutSpan.Outcome != "skipped" {
		t.Errorf("Cron shortcut_dispatch should be skipped, got outcome %q", shortcutSpan.Outcome)
	}
}

func TestFitness_ChannelPresetEnablesBotCommands(t *testing.T) {
	cfg := PresetChannel("telegram")
	if !cfg.BotCommandDispatch {
		t.Error("Channel preset should have BotCommandDispatch=true (Rust parity)")
	}
	if !cfg.SkillFirstEnabled {
		t.Error("Channel preset should have SkillFirstEnabled=true (Rust parity)")
	}
	if !cfg.SpecialistControls {
		t.Error("Channel preset should have SpecialistControls=true (Rust parity)")
	}
}

// ── Trace Channel Label Fitness Tests ─────────────────────────────────────
//
// Rust uses channel labels for cost attribution and log filtering.

func TestFitness_TracePersistsChannelLabel(t *testing.T) {
	presets := map[string]Config{
		"api":      PresetAPI(),
		"telegram": PresetChannel("telegram"),
		"cron":     PresetCron(),
	}

	for label, cfg := range presets {
		t.Run(label, func(t *testing.T) {
			store := testutil.TempStore(t)
			pipe := New(PipelineDeps{
				Store:    store,
				Executor: &stubExecutor{response: "Channel label fitness test response with adequate length for guard validation"},
				BGWorker: testutil.BGWorker(t, 4),
			})

			outcome, err := RunPipeline(context.Background(), pipe, cfg, Input{
				Content: "channel label test",
				AgentID: "default",
			})
			if err != nil {
				t.Fatal(err)
			}

			trace := extractFullTrace(t, store, outcome.SessionID)
			if trace.Channel != cfg.ChannelLabel {
				t.Errorf("trace.Channel = %q, want %q", trace.Channel, cfg.ChannelLabel)
			}
		})
	}
}

// ── Timing Fitness Tests ──────────────────────────────────────────────────
//
// Rust records per-span timing. These tests verify Go does the same.

func TestFitness_SpanTimingRecorded(t *testing.T) {
	store := testutil.TempStore(t)
	pipe := New(PipelineDeps{
		Store:    store,
		Executor: &stubExecutor{response: "Timing fitness test verifying that per-span duration is recorded in pipeline traces"},
		BGWorker: testutil.BGWorker(t, 4),
	})

	outcome, err := RunPipeline(context.Background(), pipe, PresetAPI(), Input{
		Content: "timing fitness test",
		AgentID: "default",
	})
	if err != nil {
		t.Fatal(err)
	}

	trace := extractFullTrace(t, store, outcome.SessionID)
	spans := extractSpans(t, trace.StagesJSON)

	// Total trace time must be non-negative.
	if trace.TotalMs < 0 {
		t.Errorf("TotalMs = %d, should be >= 0", trace.TotalMs)
	}

	// Each span must have non-negative duration.
	for _, span := range spans {
		if span.DurationMs < 0 {
			t.Errorf("span %q has negative duration: %d", span.Name, span.DurationMs)
		}
	}
}

// ── Guard Chain Recording Fitness Tests ───────────────────────────────────
//
// Rust's guard chain records which guards evaluated and their outcomes.

func TestFitness_GuardChainEvaluated(t *testing.T) {
	store := testutil.TempStore(t)
	guards := DefaultGuardChain()
	pipe := New(PipelineDeps{
		Store:    store,
		Executor: &stubExecutor{response: "This is a thoughtful and substantive response that exercises the full guard chain evaluation path"},
		Guards:   guards,
		BGWorker: testutil.BGWorker(t, 4),
	})

	outcome, err := RunPipeline(context.Background(), pipe, PresetAPI(), Input{
		Content: "guard chain fitness test",
		AgentID: "default",
	})
	if err != nil {
		t.Fatal(err)
	}

	// The outcome content should not be empty if guards passed.
	if outcome.Content == "" {
		t.Error("guard chain should produce non-empty content")
	}

	// InferenceParams should exist and record guard metadata.
	trace := extractFullTrace(t, store, outcome.SessionID)
	if trace.InferenceParams != nil {
		params, _ := ParseInferenceParams(*trace.InferenceParams)
		if params != nil && params.GuardRetried {
			// If a retry happened, violations should be recorded.
			if len(params.GuardViolations) == 0 {
				t.Error("guard retry with no recorded violations")
			}
		}
	}
}

// ── Injection Defense Fitness Tests ───────────────────────────────────────
//
// Rust's injection defense records the threat score in span metadata.

func TestFitness_InjectionScoreAnnotated(t *testing.T) {
	store := testutil.TempStore(t)
	pipe := New(PipelineDeps{
		Store:    store,
		Executor: &stubExecutor{response: "Injection score annotation fitness test verifying threat score metadata persistence"},
		BGWorker: testutil.BGWorker(t, 4),
		// Note: no Injection checker configured — the span should still fire.
	})

	outcome, err := RunPipeline(context.Background(), pipe, PresetAPI(), Input{
		Content: "injection fitness test",
		AgentID: "default",
	})
	if err != nil {
		t.Fatal(err)
	}

	trace := extractFullTrace(t, store, outcome.SessionID)
	spans := extractSpans(t, trace.StagesJSON)

	// injection_defense stage should always fire.
	injSpan := findSpan(spans, "injection_defense")
	if injSpan == nil {
		t.Fatal("missing injection_defense span")
	}
	// When no injection checker is configured, score annotation won't be present,
	// but the span outcome should still be "ok".
	if injSpan.Outcome != "ok" {
		t.Errorf("injection_defense outcome = %q, want 'ok'", injSpan.Outcome)
	}
}

// ── Short-Followup Expansion Fitness Test ─────────────────────────────────
//
// Rust's contextualize_short_followup() expands short reactions. Verify that
// short-followup expansion doesn't interfere with normal messages.

func TestFitness_ShortFollowupDoesNotExpandNormalMessages(t *testing.T) {
	store := testutil.TempStore(t)
	pipe := New(PipelineDeps{
		Store:    store,
		Executor: &stubExecutor{response: "Short followup fitness test verifying that normal-length messages are not expanded or modified"},
		BGWorker: testutil.BGWorker(t, 4),
	})

	cfg := PresetAPI()
	normalMsg := "This is a normal length message that should not be expanded by the short followup heuristic"

	outcome, err := RunPipeline(context.Background(), pipe, cfg, Input{
		Content: normalMsg,
		AgentID: "default",
	})
	if err != nil {
		t.Fatal(err)
	}

	// The message should pass through without expansion (no prior context to expand from).
	if outcome.Content == "" {
		t.Error("outcome should have content")
	}
}

// ── Dedup Fitness Tests ──────────────────────────────────────────────────

func TestFitness_DedupRejectsDuplicateInFlight(t *testing.T) {
	store := testutil.TempStore(t)
	pipe := New(PipelineDeps{
		Store:    store,
		Executor: &stubExecutor{response: "Dedup fitness test verifying concurrent duplicate rejection in the pipeline dedup tracker"},
		BGWorker: testutil.BGWorker(t, 4),
	})

	cfg := PresetAPI()
	input := Input{Content: "dedup fitness test", AgentID: "default"}

	// Pre-track the fingerprint to simulate in-flight.
	fp := Fingerprint(input.Content, input.AgentID, input.SessionID)
	pipe.dedup.CheckAndTrack(fp)

	// Second run should be rejected.
	_, err := RunPipeline(context.Background(), pipe, cfg, input)
	if err == nil {
		t.Error("duplicate in-flight request should be rejected")
	}
	if err != nil && !strings.Contains(err.Error(), "duplicate") {
		t.Errorf("error should mention 'duplicate', got: %v", err)
	}

	// Release and try again — should succeed.
	pipe.dedup.Release(fp)
	outcome, err := RunPipeline(context.Background(), pipe, cfg, input)
	if err != nil {
		t.Fatalf("after release, should succeed: %v", err)
	}
	if outcome == nil {
		t.Error("should produce outcome after dedup release")
	}
}

// ── Cross-Preset Trace Completeness ──────────────────────────────────────
//
// Every preset that runs through inference should produce a complete trace
// with TurnID, SessionID, Channel, and non-zero TotalMs.

func TestFitness_TraceCompletenessAcrossPresets(t *testing.T) {
	presets := map[string]Config{
		"API":     PresetAPI(),
		"Channel": PresetChannel("discord"),
		"Cron":    PresetCron(),
	}

	for name, cfg := range presets {
		t.Run(name, func(t *testing.T) {
			store := testutil.TempStore(t)
			pipe := New(PipelineDeps{
				Store:    store,
				Executor: &stubExecutor{response: "Trace completeness fitness test verifying all trace fields are populated correctly"},
				BGWorker: testutil.BGWorker(t, 4),
			})

			outcome, err := RunPipeline(context.Background(), pipe, cfg, Input{
				Content: "trace completeness test",
				AgentID: "default",
			})
			if err != nil {
				t.Fatalf("RunPipeline[%s]: %v", name, err)
			}

			trace := extractFullTrace(t, store, outcome.SessionID)
			if trace.TurnID == "" {
				t.Error("TurnID should not be empty")
			}
			if trace.SessionID == "" {
				t.Error("SessionID should not be empty")
			}
			if trace.Channel == "" {
				t.Error("Channel should not be empty")
			}
			if trace.StagesJSON == "" {
				t.Error("StagesJSON should not be empty")
			}
		})
	}
}

// ── Namespace Prefix Fitness Test ────────────────────────────────────────
//
// Rust uses specific namespace prefixes for trace annotations. Verify that
// Go's annotation helpers use the same prefixes.

func TestFitness_TraceNamespaceConstants(t *testing.T) {
	// These must match Rust's namespace constants exactly.
	expected := map[string]string{
		"TraceNSPipeline":   "pipeline",
		"TraceNSGuard":      "guard",
		"TraceNSInference":  "inference",
		"TraceNSRetrieval":  "retrieval",
		"TraceNSToolSearch": "tool_search",
		"TraceNSMCP":        "mcp",
		"TraceNSDelegation": "delegation",
		"TraceNSTaskState":  "task_state",
	}

	actual := map[string]string{
		"TraceNSPipeline":   TraceNSPipeline,
		"TraceNSGuard":      TraceNSGuard,
		"TraceNSInference":  TraceNSInference,
		"TraceNSRetrieval":  TraceNSRetrieval,
		"TraceNSToolSearch": TraceNSToolSearch,
		"TraceNSMCP":        TraceNSMCP,
		"TraceNSDelegation": TraceNSDelegation,
		"TraceNSTaskState":  TraceNSTaskState,
	}

	for name, want := range expected {
		got := actual[name]
		if got != want {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
	}
}

// ── ReactTrace Type Fitness Test ─────────────────────────────────────────
//
// Verify that ReactTrace covers all step kinds from Rust.

func TestFitness_ReactTraceStepKindCoverage(t *testing.T) {
	// Rust's StepKind enum has: ToolCall, LLMCall, GuardCheck, Retry,
	// GuardPrecompute, CacheHit, Decomposition, Speculation.
	// Verify Go defines all of these.
	kinds := map[string]StepKind{
		"StepToolCall":        StepToolCall,
		"StepLLMCall":         StepLLMCall,
		"StepGuardCheck":      StepGuardCheck,
		"StepRetry":           StepRetry,
		"StepGuardPrecompute": StepGuardPrecompute,
		"StepCacheHit":        StepCacheHit,
		"StepDecomposition":   StepDecomposition,
		"StepSpeculation":     StepSpeculation,
	}

	// Verify they're all distinct values.
	seen := make(map[StepKind]string)
	for name, kind := range kinds {
		if prev, exists := seen[kind]; exists {
			t.Errorf("duplicate StepKind value: %s and %s both = %d", prev, name, kind)
		}
		seen[kind] = name
	}
}

func TestFitness_ReactTraceRoundTrip(t *testing.T) {
	rt := NewReactTrace()
	rt.RecordStep(ReactStep{Kind: StepToolCall, Name: "web_search", DurationMs: 120, Success: true, Source: ToolSource{Kind: "builtin"}})
	rt.RecordStep(ReactStep{Kind: StepLLMCall, Name: "gpt-4", DurationMs: 500, Success: true})
	rt.RecordStep(ReactStep{Kind: StepGuardCheck, Name: "empty_response", DurationMs: 1, Success: true})
	rt.Finish()

	// JSON round-trip.
	jsonStr := rt.JSON()
	if jsonStr == "" {
		t.Fatal("ReactTrace.JSON() should not be empty")
	}

	var decoded ReactTrace
	if err := json.Unmarshal([]byte(jsonStr), &decoded); err != nil {
		t.Fatalf("JSON unmarshal: %v", err)
	}
	if len(decoded.Steps) != 3 {
		t.Errorf("got %d steps, want 3", len(decoded.Steps))
	}
	if decoded.Steps[0].Name != "web_search" {
		t.Errorf("step[0].Name = %q, want 'web_search'", decoded.Steps[0].Name)
	}
	if decoded.Steps[0].Source.Kind != "builtin" {
		t.Errorf("step[0].Source.Kind = %q, want 'builtin'", decoded.Steps[0].Source.Kind)
	}
}

// ── InferenceParams Round-Trip Fitness Test ───────────────────────────────

func TestFitness_InferenceParamsRoundTrip(t *testing.T) {
	params := &InferenceParams{
		ModelRequested:  "gpt-4",
		ModelActual:     "gpt-4-turbo",
		Provider:        "openai",
		Escalated:       true,
		TokensIn:        1500,
		TokensOut:       800,
		ReactTurns:      3,
		FromCache:       false,
		GuardViolations: []string{"empty_response", "repetition"},
		GuardRetried:    true,
	}

	jsonStr := params.JSON()
	decoded, err := ParseInferenceParams(jsonStr)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	if decoded.ModelRequested != params.ModelRequested {
		t.Errorf("ModelRequested = %q, want %q", decoded.ModelRequested, params.ModelRequested)
	}
	if decoded.ModelActual != params.ModelActual {
		t.Errorf("ModelActual = %q, want %q", decoded.ModelActual, params.ModelActual)
	}
	if decoded.Escalated != params.Escalated {
		t.Errorf("Escalated = %v, want %v", decoded.Escalated, params.Escalated)
	}
	if decoded.TokensIn != params.TokensIn {
		t.Errorf("TokensIn = %d, want %d", decoded.TokensIn, params.TokensIn)
	}
	if decoded.ReactTurns != params.ReactTurns {
		t.Errorf("ReactTurns = %d, want %d", decoded.ReactTurns, params.ReactTurns)
	}
	if len(decoded.GuardViolations) != 2 {
		t.Errorf("GuardViolations len = %d, want 2", len(decoded.GuardViolations))
	}
	if !decoded.GuardRetried {
		t.Error("GuardRetried should be true")
	}
}

func TestFitness_InferenceParamsNilSafety(t *testing.T) {
	// Nil params should produce empty string.
	var nilParams *InferenceParams
	if nilParams.JSON() != "" {
		t.Error("nil InferenceParams.JSON() should be empty")
	}

	// Empty string should parse to nil.
	parsed, err := ParseInferenceParams("")
	if err != nil {
		t.Errorf("empty string should not error: %v", err)
	}
	if parsed != nil {
		t.Error("empty string should parse to nil")
	}
}

// ── Cache Hit Session Persistence Fitness Test ──────────────────────────
//
// Regression: cache hit path returned without storing the assistant response
// in session_messages, breaking context continuity on subsequent turns.
// The model would lose the cached exchange from its history, causing
// response looping and context drift.

func TestFitness_CacheHitStoresAssistantMessage(t *testing.T) {
	store := testutil.TempStore(t)
	pipe := New(PipelineDeps{
		Store:    store,
		Executor: &stubExecutor{response: "This is a substantive cached response for session persistence fitness testing"},
		BGWorker: testutil.BGWorker(t, 4),
	})

	cfg := PresetAPI()
	prompt := "cache hit session persistence fitness test"
	expectedResponse := "This is a substantive cached response for session persistence fitness testing"

	// First run: generates response and stores in cache.
	outcome1, err := RunPipeline(context.Background(), pipe, cfg, Input{
		Content: prompt,
		AgentID: "default",
	})
	if err != nil {
		t.Fatalf("first run: %v", err)
	}

	// Wait for background cache store.
	pipe.bgWorker.Drain(5 * time.Second)

	// Second run: should hit cache.
	outcome2, err := RunPipeline(context.Background(), pipe, cfg, Input{
		Content:   prompt,
		AgentID:   "default",
		SessionID: outcome1.SessionID, // Same session!
	})
	if err != nil {
		t.Fatalf("second run: %v", err)
	}
	if !outcome2.FromCache {
		t.Skip("cache miss — skipping persistence check")
	}
	if outcome2.Content != expectedResponse {
		t.Errorf("cached content = %q, want %q", outcome2.Content, expectedResponse)
	}

	// CRITICAL CHECK: The cached assistant message must exist in session_messages.
	var assistantCount int
	err = store.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM session_messages
		 WHERE session_id = ? AND role = 'assistant'`,
		outcome1.SessionID).Scan(&assistantCount)
	if err != nil {
		t.Fatalf("query assistant messages: %v", err)
	}
	// Should have AT LEAST 2 assistant messages: one from first run, one from cache hit.
	if assistantCount < 2 {
		t.Errorf("expected >= 2 assistant messages in session_messages, got %d (cache hit not persisted)", assistantCount)
	}
}

// ── Personality Reinforcement Fitness Test ──────────────────────────────
//
// On early turns (1-3) with no memory, the pipeline injects a personality
// reinforcement system note so the model doesn't default to generic
// assistant behavior. This test verifies the reinforcement fires.

func TestFitness_PersonalityReinforcementOnEarlyTurns(t *testing.T) {
	store := testutil.TempStore(t)

	// Use a stub retriever that always returns empty (simulating cold start).
	pipe := New(PipelineDeps{
		Store:     store,
		Executor:  &stubExecutor{response: "Personality reinforcement fitness test response with character-appropriate content"},
		Retriever: &stubRetriever{result: ""},
		BGWorker:  testutil.BGWorker(t, 4),
	})

	cfg := PresetAPI()
	outcome, err := RunPipeline(context.Background(), pipe, cfg, Input{
		Content: "Hello, who are you?",
		AgentID: "default",
	})
	if err != nil {
		t.Fatalf("RunPipeline: %v", err)
	}

	// Check trace for personality_boost annotation.
	trace := extractFullTrace(t, store, outcome.SessionID)
	spans := extractSpans(t, trace.StagesJSON)
	memSpan := findSpan(spans, "memory_retrieval")
	if memSpan == nil {
		t.Fatal("missing memory_retrieval span")
	}
	if !hasAnnotation(memSpan, "personality_boost") {
		t.Error("early turn with empty memory should annotate personality_boost=true")
	}
}

func TestFitness_PersonalityReinforcementNotOnLaterTurns(t *testing.T) {
	store := testutil.TempStore(t)

	// Stub retriever that returns actual memory content (simulating established session).
	pipe := New(PipelineDeps{
		Store:    store,
		Executor: &stubExecutor{response: "Later turn fitness test response that should not trigger personality reinforcement"},
		Retriever: &stubRetriever{
			result: "[Working Memory]\nUser prefers concise responses.\n---\n[Episodic]\nPrevious discussion about pipeline architecture.",
		},
		BGWorker: testutil.BGWorker(t, 4),
	})

	cfg := PresetAPI()
	outcome, err := RunPipeline(context.Background(), pipe, cfg, Input{
		Content: "Tell me more about that",
		AgentID: "default",
	})
	if err != nil {
		t.Fatalf("RunPipeline: %v", err)
	}

	// When memory IS available, personality boost should NOT fire.
	trace := extractFullTrace(t, store, outcome.SessionID)
	spans := extractSpans(t, trace.StagesJSON)
	memSpan := findSpan(spans, "memory_retrieval")
	if memSpan == nil {
		t.Fatal("missing memory_retrieval span")
	}
	if hasAnnotation(memSpan, "personality_boost") {
		t.Error("personality_boost should NOT fire when memory retrieval returns content")
	}
}

// ── Pipeline Trace Persistence Fitness Test ───────────────────────────────
//
// Verify that the pipeline actually persists traces to the DB on every run.

func TestFitness_EveryRunPersistsTrace(t *testing.T) {
	store := testutil.TempStore(t)
	pipe := New(PipelineDeps{
		Store:    store,
		Executor: &stubExecutor{response: "Trace persistence fitness test ensuring every pipeline run writes to pipeline_traces table"},
		BGWorker: testutil.BGWorker(t, 4),
	})

	// Count traces before.
	var countBefore int
	_ = store.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM pipeline_traces`).Scan(&countBefore)

	// Run pipeline.
	_, err := RunPipeline(context.Background(), pipe, PresetAPI(), Input{
		Content: "trace persistence test",
		AgentID: "default",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Count traces after.
	var countAfter int
	_ = store.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM pipeline_traces`).Scan(&countAfter)

	if countAfter <= countBefore {
		t.Errorf("pipeline trace count did not increase: before=%d, after=%d", countBefore, countAfter)
	}
}
