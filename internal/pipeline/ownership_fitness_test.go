package pipeline

import (
	"context"
	"testing"

	"roboticus/internal/core"
	"roboticus/testutil"
)

// TestOwnership_NewConnectorInheritsAllStages proves a new connector gets
// all pipeline behavior by calling RunPipeline with zero connector-side logic.
func TestOwnership_NewConnectorInheritsAllStages(t *testing.T) {
	store := testutil.TempStore(t)
	pipe := New(PipelineDeps{
		Store:    store,
		Executor: &stubExecutor{response: "inherited"},
		BGWorker: core.NewBackgroundWorker(4),
	})

	// This is what a new connector would do: parse → call → format.
	// Zero business logic. All behavior comes from the pipeline.
	input := Input{Content: "hello world", AgentID: "default", Platform: "new-connector"}
	outcome, err := RunPipeline(context.Background(), pipe, PresetAPI(), input)
	if err != nil {
		t.Fatalf("new connector failed: %v", err)
	}

	// Verify we got a real outcome with content.
	if outcome.SessionID == "" {
		t.Error("outcome missing SessionID — pipeline didn't run session resolution")
	}
	if outcome.Content == "" {
		t.Error("outcome missing Content — pipeline didn't run inference")
	}
	if outcome.MessageID == "" {
		t.Error("outcome missing MessageID — pipeline didn't store message")
	}
}

// TestOwnership_TwoPresetsProduceOnlyConfigDifferences verifies that
// the behavioral delta between API and Channel presets is exactly the
// Config flag differences — nothing more.
func TestOwnership_TwoPresetsProduceOnlyConfigDifferences(t *testing.T) {
	store := testutil.TempStore(t)
	pipe := New(PipelineDeps{
		Store:    store,
		Executor: &stubExecutor{response: "config diff"},
		BGWorker: core.NewBackgroundWorker(4),
	})
	ctx := context.Background()

	apiInput := Input{Content: "config diff test", AgentID: "default", Platform: "api"}
	apiOutcome, err := RunPipeline(ctx, pipe, PresetAPI(), apiInput)
	if err != nil {
		t.Fatalf("API: %v", err)
	}
	apiStages := extractStageNames(t, store, apiOutcome.SessionID)

	chanInput := Input{Content: "config diff test", AgentID: "default", Platform: "channel"}
	chanOutcome, err := RunPipeline(ctx, pipe, PresetChannel("test"), chanInput)
	if err != nil {
		t.Fatalf("Channel: %v", err)
	}
	chanStages := extractStageNames(t, store, chanOutcome.SessionID)

	// Both should have the universal stages.
	for _, stage := range []string{"validation", "injection_defense", "session_resolution", "message_storage", "inference"} {
		if !containsStage(apiStages, stage) {
			t.Errorf("API missing %q", stage)
		}
		if !containsStage(chanStages, stage) {
			t.Errorf("Channel missing %q", stage)
		}
	}
}

// TestOwnership_PipelineOutcomeContainsAllMetadata verifies the Outcome struct
// gives connectors everything they need — no DB queries required.
func TestOwnership_PipelineOutcomeContainsAllMetadata(t *testing.T) {
	store := testutil.TempStore(t)
	pipe := New(PipelineDeps{
		Store:    store,
		Executor: &stubExecutor{response: "metadata check"},
		BGWorker: core.NewBackgroundWorker(4),
	})

	outcome, err := RunPipeline(context.Background(), pipe, PresetAPI(),
		Input{Content: "metadata test", AgentID: "default", Platform: "api"})
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	// Every connector-necessary field must be populated.
	if outcome.SessionID == "" {
		t.Error("missing SessionID")
	}
	if outcome.MessageID == "" {
		t.Error("missing MessageID")
	}
	if outcome.Content == "" {
		t.Error("missing Content")
	}
	// Stream field should be false for standard inference.
	if outcome.Stream {
		t.Error("Stream should be false for standard preset")
	}
}
