package db

import (
	"context"
	"testing"
)

func TestExerciseScorecard_IncludesModelAttributableLatency(t *testing.T) {
	store := testTempStore(t)
	ctx := context.Background()

	if err := InsertBaselineRun(ctx, store, BaselineRunRow{
		RunID:      "run-latency",
		Initiator:  "test",
		Status:     "completed",
		ModelCount: 1,
		Models:     []string{"model-a"},
		Iterations: 1,
	}); err != nil {
		t.Fatalf("InsertBaselineRun: %v", err)
	}
	if err := InsertExerciseResult(ctx, store, ExerciseResultRow{
		ID:           "row-1",
		RunID:        "run-latency",
		Model:        "model-a",
		IntentClass:  "EXECUTION",
		Complexity:   "trivial",
		Prompt:       "p1",
		Content:      "ok",
		Quality:      0.9,
		LatencyMs:    1000,
		PhaseTimings: `{"total_ms":1000,"model_inference_ms":250}`,
		Passed:       true,
	}); err != nil {
		t.Fatalf("InsertExerciseResult row-1: %v", err)
	}
	if err := InsertExerciseResult(ctx, store, ExerciseResultRow{
		ID:           "row-2",
		RunID:        "run-latency",
		Model:        "model-a",
		IntentClass:  "EXECUTION",
		Complexity:   "simple",
		Prompt:       "p2",
		Content:      "ok",
		Quality:      0.7,
		LatencyMs:    2000,
		PhaseTimings: `{"total_ms":2000,"model_inference_ms":750}`,
		Passed:       true,
	}); err != nil {
		t.Fatalf("InsertExerciseResult row-2: %v", err)
	}

	entries := ExerciseScorecard(ctx, store)
	if len(entries) != 1 {
		t.Fatalf("scorecard entries = %d, want 1: %#v", len(entries), entries)
	}
	if entries[0].AvgLatencyMs != 500 {
		t.Fatalf("avg latency = %d, want model-attributable 500", entries[0].AvgLatencyMs)
	}
}

func TestExerciseScorecard_PartialIntentRunPreservesOtherIntentEvidence(t *testing.T) {
	store := testTempStore(t)
	ctx := context.Background()

	for _, runID := range []string{"run-full", "run-delegation"} {
		if err := InsertBaselineRun(ctx, store, BaselineRunRow{
			RunID:      runID,
			Initiator:  "test",
			Status:     "completed",
			ModelCount: 1,
			Models:     []string{"model-a"},
			Iterations: 1,
		}); err != nil {
			t.Fatalf("InsertBaselineRun %s: %v", runID, err)
		}
	}

	rows := []ExerciseResultRow{
		{ID: "row-full-exec", RunID: "run-full", Model: "model-a", IntentClass: "EXECUTION", Complexity: "trivial", Prompt: "p", Content: "exec", Quality: 0.9, LatencyMs: 100, Passed: true},
		{ID: "row-full-deleg", RunID: "run-full", Model: "model-a", IntentClass: "DELEGATION", Complexity: "trivial", Prompt: "p", Content: "deleg-old", Quality: 0.5, LatencyMs: 200, Passed: true},
		{ID: "row-new-deleg", RunID: "run-delegation", Model: "model-a", IntentClass: "DELEGATION", Complexity: "trivial", Prompt: "p", Content: "deleg-new", Quality: 0.8, LatencyMs: 300, Passed: true},
	}
	for _, row := range rows {
		if err := InsertExerciseResult(ctx, store, row); err != nil {
			t.Fatalf("InsertExerciseResult %s: %v", row.ID, err)
		}
	}

	entries := ExerciseScorecard(ctx, store)
	if len(entries) != 2 {
		t.Fatalf("scorecard entries = %d, want 2: %#v", len(entries), entries)
	}
	qualities := map[string]float64{}
	for _, entry := range entries {
		qualities[entry.IntentClass] = entry.AvgQuality
	}
	if qualities["EXECUTION"] != 0.9 {
		t.Fatalf("execution quality = %.2f, want historical 0.90", qualities["EXECUTION"])
	}
	if qualities["DELEGATION"] != 0.8 {
		t.Fatalf("delegation quality = %.2f, want fresh 0.80", qualities["DELEGATION"])
	}
}

func TestExerciseScorecard_ExcludesValidityOnlyFailures(t *testing.T) {
	store := testTempStore(t)
	ctx := context.Background()

	for _, runID := range []string{"run-valid", "run-invalid"} {
		if err := InsertBaselineRun(ctx, store, BaselineRunRow{
			RunID:      runID,
			Initiator:  "test",
			Status:     "completed",
			ModelCount: 1,
			Models:     []string{"model-a"},
			Iterations: 1,
		}); err != nil {
			t.Fatalf("InsertBaselineRun %s: %v", runID, err)
		}
	}

	rows := []ExerciseResultRow{
		{ID: "row-valid", RunID: "run-valid", Model: "model-a", IntentClass: "TOOL_USE", Complexity: "trivial", Prompt: "p", Content: "4", Quality: 0.9, LatencyMs: 100, Passed: true, ResultClass: "clean_pass"},
		{ID: "row-invalid", RunID: "run-invalid", Model: "model-a", IntentClass: "TOOL_USE", Complexity: "trivial", Prompt: "p", Quality: 0, LatencyMs: 200, Passed: false, ResultClass: "transport_error", ErrorMsg: "API error: internal error"},
		{ID: "row-empty", RunID: "run-invalid", Model: "model-a", IntentClass: "MEMORY_RECALL", Complexity: "trivial", Prompt: "p", Quality: 0, LatencyMs: 300, Passed: false, ResultClass: "empty_response"},
	}
	for _, row := range rows {
		if err := InsertExerciseResult(ctx, store, row); err != nil {
			t.Fatalf("InsertExerciseResult %s: %v", row.ID, err)
		}
	}

	entries := ExerciseScorecard(ctx, store)
	qualities := map[string]float64{}
	counts := map[string]int{}
	for _, entry := range entries {
		qualities[entry.IntentClass] = entry.AvgQuality
		counts[entry.IntentClass] = entry.Count
	}
	if qualities["TOOL_USE"] != 0.9 {
		t.Fatalf("tool-use quality = %.2f, want previous valid 0.90 after latest transport-only run", qualities["TOOL_USE"])
	}
	if counts["TOOL_USE"] != 1 {
		t.Fatalf("tool-use count = %d, want transport row excluded", counts["TOOL_USE"])
	}
	if _, ok := qualities["MEMORY_RECALL"]; !ok {
		t.Fatalf("empty response should remain zero-quality efficacy evidence: %#v", entries)
	}
	if qualities["MEMORY_RECALL"] != 0 {
		t.Fatalf("memory-recall quality = %.2f, want empty response zero", qualities["MEMORY_RECALL"])
	}
}
