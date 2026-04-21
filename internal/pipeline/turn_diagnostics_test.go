package pipeline

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"roboticus/testutil"
)

func TestStoreTurnDiagnostics_PersistsRecorderOnce(t *testing.T) {
	store := testutil.TempStore(t)
	pipe := New(PipelineDeps{Store: store})
	dr := NewTurnDiagnosticsRecorder("sess-1", "turn-1", "telegram")
	dr.RecordEvent("fallback_triggered", "error",
		"primary model failed and fallback was used",
		"The system had to switch models for this turn.",
		map[string]any{"reason_code": "provider_timeout"},
	)

	pipe.storeTurnDiagnostics(context.Background(), dr)
	pipe.storeTurnDiagnostics(context.Background(), dr)

	var summaryCount int
	if err := store.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM turn_diagnostics WHERE turn_id = ?`, "turn-1",
	).Scan(&summaryCount); err != nil {
		t.Fatalf("count turn_diagnostics: %v", err)
	}
	if summaryCount != 1 {
		t.Fatalf("turn_diagnostics rows = %d, want 1", summaryCount)
	}

	var eventCount int
	if err := store.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM turn_diagnostic_events WHERE turn_id = ?`, "turn-1",
	).Scan(&eventCount); err != nil {
		t.Fatalf("count turn_diagnostic_events: %v", err)
	}
	if eventCount != 1 {
		t.Fatalf("turn_diagnostic_events rows = %d, want 1", eventCount)
	}

	pipe.storeTurnDiagnostics(context.Background(), dr)

	if err := store.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM turn_diagnostics WHERE turn_id = ?`, "turn-1",
	).Scan(&summaryCount); err != nil {
		t.Fatalf("recount turn_diagnostics: %v", err)
	}
	if summaryCount != 1 {
		t.Fatalf("turn_diagnostics rows after redundant flush = %d, want 1", summaryCount)
	}
}

func TestPipelineRun_PersistsTaskSynthesisDecisionFacts(t *testing.T) {
	store := testutil.TempStore(t)
	exec := &sequencedExecutor{responses: []string{
		"Here is a short answer.",
	}}

	pipe := New(PipelineDeps{
		Store:    store,
		Executor: exec,
		Guards:   DefaultGuardChain(),
	})

	cfg := PresetAPI()
	cfg.DecompositionGate = false
	cfg.DelegatedExecution = false
	cfg.TaskOperatingState = "test"
	cfg.GuardSet = GuardSetNone

	outcome, err := pipe.Run(context.Background(), cfg, Input{
		Content: "Why do Go closures capture variables?",
		AgentID: "test-agent",
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	var turnID string
	if err := store.QueryRowContext(context.Background(),
		`SELECT id FROM turns WHERE session_id = ? ORDER BY created_at DESC LIMIT 1`, outcome.SessionID,
	).Scan(&turnID); err != nil {
		t.Fatalf("query turn id: %v", err)
	}

	var detailsJSON string
	if err := store.QueryRowContext(context.Background(),
		`SELECT details_json FROM turn_diagnostic_events WHERE turn_id = ? AND event_type = ?`,
		turnID, "task_synthesis_completed",
	).Scan(&detailsJSON); err != nil {
		if err == sql.ErrNoRows {
			t.Fatal("expected task_synthesis_completed event")
		}
		t.Fatalf("query task synthesis event: %v", err)
	}

	var details map[string]any
	if err := json.Unmarshal([]byte(detailsJSON), &details); err != nil {
		t.Fatalf("decode details_json: %v", err)
	}
	if got := details["intent"]; got != "question" {
		t.Fatalf("intent = %v, want question", got)
	}
	if got := details["complexity"]; got != "simple" {
		t.Fatalf("complexity = %v, want simple", got)
	}
	if got := details["turn_weight"]; got != "standard" {
		t.Fatalf("turn_weight = %v, want standard", got)
	}
}

func TestStoreTurnDiagnostics_UpdatesSummaryAndAppendsEventsAcrossFlushes(t *testing.T) {
	store := testutil.TempStore(t)
	pipe := New(PipelineDeps{Store: store})
	dr := NewTurnDiagnosticsRecorder("sess-1", "turn-2", "telegram")
	dr.RecordEvent("context_pressure_assessed", "ok",
		"context pressure assessed before inference",
		"Checked request size before inference.",
		map[string]any{"context_pressure": "high"},
	)
	pipe.storeTurnDiagnostics(context.Background(), dr)

	dr.SetSummaryField("final_model", "openai/gpt-4o-mini")
	dr.SetSummaryField("final_provider", "openrouter")
	dr.IncrementSummaryCounter("fallback_count", 1)
	dr.RecordEvent("fallback_triggered", "error",
		"primary model failed and fallback was used",
		"The system had to switch models for this turn.",
		map[string]any{"reason_code": "provider_timeout"},
	)
	pipe.storeTurnDiagnostics(context.Background(), dr)

	var summaryCount int
	var fallbackCount int
	var finalModel string
	if err := store.QueryRowContext(context.Background(),
		`SELECT COUNT(*), COALESCE(MAX(fallback_count), 0), COALESCE(MAX(final_model), '') FROM turn_diagnostics WHERE turn_id = ?`,
		"turn-2",
	).Scan(&summaryCount, &fallbackCount, &finalModel); err != nil {
		t.Fatalf("query updated summary: %v", err)
	}
	if summaryCount != 1 {
		t.Fatalf("turn_diagnostics rows = %d, want 1", summaryCount)
	}
	if fallbackCount != 1 {
		t.Fatalf("fallback_count = %d, want 1", fallbackCount)
	}
	if finalModel != "openai/gpt-4o-mini" {
		t.Fatalf("final_model = %q, want openai/gpt-4o-mini", finalModel)
	}

	var eventCount int
	if err := store.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM turn_diagnostic_events WHERE turn_id = ?`, "turn-2",
	).Scan(&eventCount); err != nil {
		t.Fatalf("query updated event count: %v", err)
	}
	if eventCount != 2 {
		t.Fatalf("turn_diagnostic_events rows = %d, want 2", eventCount)
	}
}

func TestStoreTurnDiagnostics_DerivesInterpretiveNarratives(t *testing.T) {
	store := testutil.TempStore(t)
	pipe := New(PipelineDeps{Store: store})
	dr := NewTurnDiagnosticsRecorder("sess-1", "turn-derived", "api")
	dr.SetSummaryField("status", "degraded")
	dr.SetSummaryField("final_provider", "moonshot")
	dr.SetSummaryField("final_model", "kimi-k2-turbo-preview")
	dr.SetSummaryField("user_narrative", "The system is collecting evidence about request size, retries, and model behavior for this turn.")
	dr.SetSummaryField("operator_narrative", "Turn diagnostics active: request-shape, fallback, and provider-attempt facts are being recorded.")
	dr.RecordEvent("model_attempt_started", "running", "", "", map[string]any{
		"provider": "moonshot",
		"model":    "kimi-k2-turbo-preview",
	})
	dr.RecordTimedEvent("model_attempt_finished", "ok", "", "", time.Now().Add(-10*time.Millisecond), "", map[string]any{
		"provider": "moonshot",
		"model":    "kimi-k2-turbo-preview",
	})
	dr.IncrementSummaryCounter("guard_retry_count", 1)
	dr.RecordEvent("guard_retry_scheduled", "error", "", "", map[string]any{
		"violations":   []string{"non_repetition_v2"},
		"retry_reason": "response repeats previous assistant message",
	})
	dr.RecordEvent("model_attempt_started", "running", "", "", map[string]any{
		"provider": "moonshot",
		"model":    "kimi-k2-turbo-preview",
	})
	dr.RecordTimedEvent("model_attempt_finished", "ok", "", "", time.Now().Add(-5*time.Millisecond), "", map[string]any{
		"provider": "moonshot",
		"model":    "kimi-k2-turbo-preview",
	})
	pipe.storeTurnDiagnostics(context.Background(), dr)

	var userNarrative, operatorNarrative string
	if err := store.QueryRowContext(context.Background(),
		`SELECT user_narrative, operator_narrative FROM turn_diagnostics WHERE turn_id = ?`,
		"turn-derived",
	).Scan(&userNarrative, &operatorNarrative); err != nil {
		t.Fatalf("query narratives: %v", err)
	}
	if strings.Contains(strings.ToLower(userNarrative), "collecting evidence") {
		t.Fatalf("user_narrative stayed boilerplate: %q", userNarrative)
	}
	if !strings.Contains(userNarrative, "first attempt succeeded") {
		t.Fatalf("user_narrative = %q, want interpretive retry conclusion", userNarrative)
	}
	if !strings.Contains(operatorNarrative, "guard=non_repetition_v2") {
		t.Fatalf("operator_narrative = %q, want guard attribution", operatorNarrative)
	}
}

func TestTurnDiagnosticsRecorder_LivenessSnapshotDistinguishesRetryChurn(t *testing.T) {
	dr := NewTurnDiagnosticsRecorder("sess-1", "turn-3", "telegram")
	dr.RecordEvent("model_attempt_started", "running",
		"starting inference attempt", "", map[string]any{
			"provider": "ollama",
			"model":    "qwen2.5:32b",
		},
	)
	first := dr.LivenessSnapshot("inference", 40*time.Second)
	if first.Phase != "initial_attempt_wait" {
		t.Fatalf("first phase = %q, want initial_attempt_wait", first.Phase)
	}
	if first.Scope != "model_attempt" {
		t.Fatalf("first scope = %q, want model_attempt", first.Scope)
	}

	dr.RecordTimedEvent("model_attempt_finished", "ok",
		"inference attempt succeeded", "", time.Now().Add(-2*time.Second), "", map[string]any{
			"provider": "ollama",
			"model":    "gemma4",
		},
	)
	dr.RecordEvent("verifier_retry_scheduled", "error", "verifier requested retry", "", nil)
	dr.RecordEvent("model_attempt_started", "running",
		"starting inference attempt", "", map[string]any{
			"provider": "openrouter",
			"model":    "openai/gpt-4o-mini",
		},
	)
	retry := dr.LivenessSnapshot("inference", 95*time.Second)
	if retry.Phase != "retry_attempt_wait" {
		t.Fatalf("retry phase = %q, want retry_attempt_wait", retry.Phase)
	}
	if retry.Details["retry_kind"] != "verifier" {
		t.Fatalf("retry_kind = %v, want verifier", retry.Details["retry_kind"])
	}

	dr.RecordTimedEvent("model_attempt_finished", "ok",
		"inference attempt succeeded", "", time.Now().Add(-1*time.Second), "", map[string]any{
			"provider": "openrouter",
			"model":    "openai/gpt-4o-mini",
		},
	)
	post := dr.LivenessSnapshot("inference", 105*time.Second)
	if post.Phase != "verification_or_guard_recovery" {
		t.Fatalf("post phase = %q, want verification_or_guard_recovery", post.Phase)
	}
	if post.Scope != "post_attempt_recovery" {
		t.Fatalf("post scope = %q, want post_attempt_recovery", post.Scope)
	}
}

func TestPipelineRun_PersistsVerifierDiagnosticsAgainstTurnRecord(t *testing.T) {
	store := testutil.TempStore(t)
	exec := &sequencedExecutor{responses: []string{
		"The deployment failed because the canary rollout was misconfigured.",
		"Based on the available evidence, I am not certain yet. We need deployment logs to confirm the root cause.",
	}}

	pipe := New(PipelineDeps{
		Store:     store,
		Executor:  exec,
		Guards:    DefaultGuardChain(),
		Retriever: &stubRetriever{result: "[Active Memory]\n\n[Retrieved Evidence]\n1. [semantic, 0.90] deployment policy\n\n[Gaps]\n- No past experiences found for this query"},
	})

	cfg := PresetAPI()
	cfg.DecompositionGate = false
	cfg.DelegatedExecution = false
	cfg.TaskOperatingState = "test"
	cfg.GuardSet = GuardSetNone

	outcome, err := pipe.Run(context.Background(), cfg, Input{
		Content: "Why did the deployment fail?",
		AgentID: "test-agent",
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if outcome == nil {
		t.Fatal("outcome should not be nil")
	}

	var turnID string
	if err := store.QueryRowContext(context.Background(),
		`SELECT id FROM turns WHERE session_id = ? ORDER BY created_at DESC LIMIT 1`, outcome.SessionID,
	).Scan(&turnID); err != nil {
		t.Fatalf("query turn id: %v", err)
	}
	if turnID == "" {
		t.Fatal("expected persisted turn id")
	}

	var verifierRetries int
	var status string
	if err := store.QueryRowContext(context.Background(),
		`SELECT verifier_retry_count, status FROM turn_diagnostics WHERE turn_id = ?`, turnID,
	).Scan(&verifierRetries, &status); err != nil {
		t.Fatalf("query diagnostics summary: %v", err)
	}
	if verifierRetries != 1 {
		t.Fatalf("verifier_retry_count = %d, want 1", verifierRetries)
	}
	if status != "degraded" {
		t.Fatalf("status = %q, want degraded", status)
	}

	var verifierEvents int
	if err := store.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM turn_diagnostic_events WHERE turn_id = ? AND event_type = ?`,
		turnID, "verifier_retry_scheduled",
	).Scan(&verifierEvents); err != nil {
		t.Fatalf("query verifier event count: %v", err)
	}
	if verifierEvents != 1 {
		t.Fatalf("verifier_retry_scheduled events = %d, want 1", verifierEvents)
	}
}
