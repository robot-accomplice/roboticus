package routes

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"roboticus/internal/core"
	"roboticus/internal/llm"
	"roboticus/internal/pipeline"
	"roboticus/testutil"
)

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
