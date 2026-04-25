package models

import (
	"context"
	"encoding/json"
	"net/http"
	"roboticus/cmd/internal/testhelp"
	"roboticus/internal/llm"
	"strings"
	"testing"
	"time"
)

func TestModelsListCmd_NonArrayModels(t *testing.T) {
	cleanup := testhelp.SetupMockAPI(t, testhelp.JSONHandler(map[string]any{
		"info": "model data not available",
	}))
	defer cleanup()

	err := modelsListCmd.RunE(modelsListCmd, nil)
	if err != nil {
		t.Fatalf("models list non-array: %v", err)
	}
}

func TestModelsListCmd_MultipleModels(t *testing.T) {
	cleanup := testhelp.SetupMockAPI(t, testhelp.JSONHandler(map[string]any{
		"models": []any{
			map[string]any{"id": "gpt-4o", "provider": "openai", "context_window": float64(128000)},
			map[string]any{"id": "claude-3-opus", "provider": "anthropic", "context_window": float64(200000)},
			map[string]any{"id": "gemini-pro", "provider": "google", "context_window": float64(32000)},
		},
	}))
	defer cleanup()

	err := modelsListCmd.RunE(modelsListCmd, nil)
	if err != nil {
		t.Fatalf("models list multiple: %v", err)
	}
}

func TestModelsListCmd_ServerError(t *testing.T) {
	cleanup := testhelp.SetupMockAPI(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": "internal error"})
	}))
	defer cleanup()

	err := modelsListCmd.RunE(modelsListCmd, nil)
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
}

func TestModelsDiagnosticsCmd_ServerError(t *testing.T) {
	cleanup := testhelp.SetupMockAPI(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": "routing error"})
	}))
	defer cleanup()

	err := modelsDiagnosticsCmd.RunE(modelsDiagnosticsCmd, nil)
	if err == nil {
		t.Fatal("expected error for routing diagnostics failure")
	}
}

func TestCliPromptSender_SetsNoEscalate(t *testing.T) {
	cleanup := testhelp.SetupMockAPI(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/agent/message" {
			t.Fatalf("path = %q, want /api/agent/message", r.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if body["no_cache"] != true {
			t.Fatalf("no_cache = %#v, want true", body["no_cache"])
		}
		if body["no_escalate"] != true {
			t.Fatalf("no_escalate = %#v, want true", body["no_escalate"])
		}
		if body["turn_id"] == "" {
			t.Fatal("turn_id should be established before benchmark execution")
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"content": "ok"})
	}))
	defer cleanup()

	dispatch, err := cliPromptSender(context.Background(), "ollama/gemma3:12b", "test prompt", 5*time.Second)
	if err != nil {
		t.Fatalf("cliPromptSender: %v", err)
	}
	if dispatch.ResponseText != "ok" {
		t.Fatalf("content = %q, want ok", dispatch.ResponseText)
	}
	if dispatch.TurnID == "" {
		t.Fatal("dispatch turn id should be preserved even when response omits it")
	}
}

func TestCliWarmupSender_SetsNoEscalate(t *testing.T) {
	cleanup := testhelp.SetupMockAPI(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/agent/message" {
			t.Fatalf("path = %q, want /api/agent/message", r.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if body["content"] != "Reply with just: ready" {
			t.Fatalf("content = %#v, want warmup prompt", body["content"])
		}
		if body["no_cache"] != true {
			t.Fatalf("no_cache = %#v, want true", body["no_cache"])
		}
		if body["no_escalate"] != true {
			t.Fatalf("no_escalate = %#v, want true", body["no_escalate"])
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"content": "ready"})
	}))
	defer cleanup()

	sender := cliWarmupSender(map[string]any{})
	res := sender(context.Background(), "ollama/gemma3:12b", 5*time.Second)
	if res.Err != nil {
		t.Fatalf("cliWarmupSender err: %v", res.Err)
	}
}

func TestExerciseRunLifecycleHelpers(t *testing.T) {
	var (
		sawStart    bool
		sawResult   bool
		sawComplete bool
	)
	cleanup := testhelp.SetupMockAPI(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/models/exercise/runs":
			sawStart = true
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode start body: %v", err)
			}
			if body["initiator"] != "cli" {
				t.Fatalf("initiator = %#v, want cli", body["initiator"])
			}
			if body["notes"] != "intent filter: TOOL_USE" {
				t.Fatalf("notes = %#v, want intent filter note", body["notes"])
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"run_id": "run-123"})
		case r.Method == http.MethodPost && r.URL.Path == "/api/models/exercise/runs/run-123/results":
			sawResult = true
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode result body: %v", err)
			}
			if body["model"] != "ollama/gemma4" {
				t.Fatalf("model = %#v, want ollama/gemma4", body["model"])
			}
			if body["result_class"] != "clean_pass" {
				t.Fatalf("result_class = %#v, want clean_pass", body["result_class"])
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
		case r.Method == http.MethodPost && r.URL.Path == "/api/models/exercise/runs/run-123/complete":
			sawComplete = true
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode complete body: %v", err)
			}
			if body["status"] != "completed" {
				t.Fatalf("status = %#v, want completed", body["status"])
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer cleanup()

	runID, err := startExerciseRun([]string{"ollama/gemma4"}, 1, map[string]any{"llm": map[string]any{"primary": "ollama/gemma4"}}, "TOOL_USE")
	if err != nil {
		t.Fatalf("startExerciseRun: %v", err)
	}
	if runID != "run-123" {
		t.Fatalf("runID = %q, want run-123", runID)
	}
	err = appendExerciseRunResult("run-123", llm.PromptOutcome{
		Model: "ollama/gemma4",
		Prompt: llm.ExercisePrompt{
			Prompt:     "Say hello.",
			Intent:     llm.IntentConversation,
			Complexity: llm.ComplexitySimple,
		},
		Content:      "hello",
		Quality:      0.9,
		LatencyMs:    1234,
		Passed:       true,
		OutcomeClass: llm.ExerciseOutcomeCleanPass,
	})
	if err != nil {
		t.Fatalf("appendExerciseRunResult: %v", err)
	}
	err = completeExerciseRun("run-123", "completed", "", []string{"ollama/gemma4"})
	if err != nil {
		t.Fatalf("completeExerciseRun: %v", err)
	}
	if !sawStart || !sawResult || !sawComplete {
		t.Fatalf("lifecycle missing: start=%v result=%v complete=%v", sawStart, sawResult, sawComplete)
	}
}

func TestStartExerciseRun_OmitsNotesWithoutIntentFilter(t *testing.T) {
	cleanup := testhelp.SetupMockAPI(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/models/exercise/runs" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode start body: %v", err)
		}
		if _, ok := body["notes"]; ok {
			t.Fatalf("notes should be omitted when no intent filter is set")
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"run_id": "run-123"})
	}))
	defer cleanup()

	if _, err := startExerciseRun([]string{"ollama/gemma4"}, 1, map[string]any{}, ""); err != nil {
		t.Fatalf("startExerciseRun: %v", err)
	}
}

func TestCurrentGitRevision_TrimsOutput(t *testing.T) {
	rev := currentGitRevision()
	if strings.Contains(rev, "\n") {
		t.Fatalf("git revision contains newline: %q", rev)
	}
}

func TestFormatExercisePreviewSingleLineSafe(t *testing.T) {
	raw := "\x1b[31m**C**\x1b[0m\n\n```c\nvoid reverse(char *s) {\n\tif (!s) return;\r\n}\n```"

	preview := formatExercisePreview(raw, 80)

	for _, forbidden := range []string{"\n", "\r", "\t", "\x1b", "```"} {
		if strings.Contains(preview, forbidden) {
			t.Fatalf("preview contains forbidden sequence %q: %q", forbidden, preview)
		}
	}
	if !strings.Contains(preview, "void reverse") {
		t.Fatalf("preview lost useful content: %q", preview)
	}
	if len([]rune(preview)) > 83 {
		t.Fatalf("preview was not bounded: %d runes in %q", len([]rune(preview)), preview)
	}
}

func TestBuildComparisonRows_MergesHistoricalScorecardEntriesWithFreshResults(t *testing.T) {
	cleanup := testhelp.SetupMockAPI(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/models/exercise/scorecard" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"entries": []any{
				map[string]any{
					"model":          "moonshot/kimi-k2.6",
					"intent_class":   "CONVERSATION",
					"avg_quality":    0.72,
					"avg_latency_ms": 20000,
					"count":          5,
				},
				map[string]any{
					"model":          "moonshot/kimi-k2.6",
					"intent_class":   "CODING",
					"avg_quality":    0.65,
					"avg_latency_ms": 30000,
					"count":          5,
				},
				map[string]any{
					"model":          "anthropic/claude-opus-4.1",
					"intent_class":   "CONVERSATION",
					"avg_quality":    0.91,
					"avg_latency_ms": 12000,
					"count":          5,
				},
				map[string]any{
					"model":          "anthropic/claude-opus-4.1",
					"intent_class":   "CODING",
					"avg_quality":    0.89,
					"avg_latency_ms": 8000,
					"count":          5,
				},
			},
		})
	}))
	defer cleanup()

	report := llm.ExerciseReport{
		Models: []llm.ModelExerciseResult{
			{
				Model: "moonshot/kimi-k2.6",
				IntentQuality: map[string]float64{
					"CONVERSATION": 0.84,
				},
				Latencies: map[string][]int64{
					"CONVERSATION": {2800, 8500},
				},
			},
		},
	}

	rows := buildComparisonRows(report, nil)
	if len(rows) != 2 {
		t.Fatalf("comparison rows = %d, want 2", len(rows))
	}

	var sawFresh bool
	var sawHistorical bool
	for _, row := range rows {
		switch row.Model {
		case "moonshot/kimi-k2.6":
			sawFresh = true
			if !row.Exercised {
				t.Fatalf("fresh row should be marked exercised")
			}
			if row.Intent["CONVERSATION"] != 0.84 {
				t.Fatalf("fresh conversation quality = %.2f, want 0.84", row.Intent["CONVERSATION"])
			}
			if row.Intent["CODING"] != 0.65 {
				t.Fatalf("historical coding quality was not preserved: %.2f, want 0.65", row.Intent["CODING"])
			}
			if row.AvgQuality != 0.7042857142857143 {
				t.Fatalf("merged avg quality = %.4f, want 0.7043", row.AvgQuality)
			}
			if row.AvgLatencyMs != 23042 {
				t.Fatalf("merged avg latency = %d, want 23042", row.AvgLatencyMs)
			}
			if row.ObservedIntents != 2 {
				t.Fatalf("merged evidence coverage = %d, want 2", row.ObservedIntents)
			}
		case "anthropic/claude-opus-4.1":
			sawHistorical = true
			if row.Exercised {
				t.Fatalf("historical row must not be marked exercised")
			}
			if row.Intent["CONVERSATION"] != 0.91 {
				t.Fatalf("historical conversation quality = %.2f, want 0.91", row.Intent["CONVERSATION"])
			}
			if row.Intent["CODING"] != 0.89 {
				t.Fatalf("historical coding quality = %.2f, want 0.89", row.Intent["CODING"])
			}
			if row.AvgLatencyMs != 10000 {
				t.Fatalf("historical avg latency = %d, want 10000", row.AvgLatencyMs)
			}
			if row.ObservedIntents != 2 {
				t.Fatalf("historical evidence coverage = %d, want 2", row.ObservedIntents)
			}
		}
	}
	if !sawFresh || !sawHistorical {
		t.Fatalf("expected both fresh and historical models, sawFresh=%v sawHistorical=%v", sawFresh, sawHistorical)
	}
}

func TestBuildComparisonRows_DoesNotOverwriteHistoricalEvidenceWithValidityOnlyFreshRun(t *testing.T) {
	cleanup := testhelp.SetupMockAPI(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/models/exercise/scorecard" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"entries": []any{
				map[string]any{
					"model":          "ollama/phi4-mini:latest",
					"intent_class":   "TOOL_USE",
					"avg_quality":    0.83,
					"avg_latency_ms": 1200,
					"count":          5,
				},
			},
		})
	}))
	defer cleanup()

	report := llm.ExerciseReport{
		Models: []llm.ModelExerciseResult{
			{
				Model:         "ollama/phi4-mini:latest",
				IntentQuality: map[string]float64{},
				Latencies: map[string][]int64{
					"TOOL_USE": {12, 13, 14, 15, 16},
				},
			},
		},
	}

	rows := buildComparisonRows(report, nil)
	if len(rows) != 1 {
		t.Fatalf("comparison rows = %d, want 1", len(rows))
	}
	row := rows[0]
	if !row.Exercised {
		t.Fatal("fresh validity-only run should still be marked exercised")
	}
	if row.Intent["TOOL_USE"] != 0.83 {
		t.Fatalf("tool-use quality = %.2f, want historical evidence preserved", row.Intent["TOOL_USE"])
	}
	if row.ObservedIntents != 1 {
		t.Fatalf("observed intents = %d, want historical 1", row.ObservedIntents)
	}
	if row.AvgLatencyMs != 1200 {
		t.Fatalf("avg latency = %d, want historical latency preserved", row.AvgLatencyMs)
	}
}
