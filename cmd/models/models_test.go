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
		_ = json.NewEncoder(w).Encode(map[string]any{"content": "ok"})
	}))
	defer cleanup()

	content, _, err := cliPromptSender(context.Background(), "ollama/gemma3:12b", "test prompt", 5*time.Second)
	if err != nil {
		t.Fatalf("cliPromptSender: %v", err)
	}
	if content != "ok" {
		t.Fatalf("content = %q, want ok", content)
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
		Content:   "hello",
		Quality:   0.9,
		LatencyMs: 1234,
		Passed:    true,
	})
	if err != nil {
		t.Fatalf("appendExerciseRunResult: %v", err)
	}
	err = completeExerciseRun("run-123", "completed", "")
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
