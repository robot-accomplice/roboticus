package db

import (
	"context"
	"testing"

	"roboticus/internal/hostresources"
	"roboticus/internal/modelstate"
)

func TestBaselineRuns_PersistAndList(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()

	if err := InsertBaselineRun(ctx, store, BaselineRunRow{
		RunID:             "run-1",
		Initiator:         "cli",
		Status:            "running",
		ModelCount:        2,
		Models:            []string{"ollama/gemma4", "ollama/phi4-mini:latest"},
		Iterations:        2,
		ConfigFingerprint: "cfg",
		GitRevision:       "deadbeef",
		StartResources: &hostresources.Snapshot{
			CollectedAt:          "2026-04-20T18:00:00Z",
			CPUPercent:           41.5,
			MemoryAvailableBytes: 12_000_000_000,
		},
		StartModelStates: []modelstate.Snapshot{{
			CollectedAt:        "2026-04-20T18:00:00Z",
			Model:              "ollama/gemma4",
			Provider:           "ollama",
			ProviderConfigured: true,
			ProviderReachable:  true,
			ModelAvailable:     true,
			ModelLoaded:        true,
			StateClass:         "ready",
		}},
	}); err != nil {
		t.Fatalf("InsertBaselineRun: %v", err)
	}
	if err := CompleteBaselineRun(ctx, store, "run-1", "completed", "ok", &hostresources.Snapshot{
		CollectedAt:          "2026-04-20T18:10:00Z",
		CPUPercent:           88.1,
		MemoryAvailableBytes: 4_000_000_000,
	}, []modelstate.Snapshot{{
		CollectedAt:        "2026-04-20T18:10:00Z",
		Model:              "ollama/gemma4",
		Provider:           "ollama",
		ProviderConfigured: true,
		ProviderReachable:  true,
		ModelAvailable:     true,
		ModelLoaded:        false,
		StateClass:         "installed_not_loaded",
	}}); err != nil {
		t.Fatalf("CompleteBaselineRun: %v", err)
	}

	runs := ListBaselineRuns(ctx, store, 10)
	if len(runs) != 1 {
		t.Fatalf("runs = %d, want 1", len(runs))
	}
	if runs[0].RunID != "run-1" {
		t.Fatalf("run_id = %q, want run-1", runs[0].RunID)
	}
	if runs[0].Status != "completed" {
		t.Fatalf("status = %q, want completed", runs[0].Status)
	}
	if len(runs[0].Models) != 2 {
		t.Fatalf("models = %d, want 2", len(runs[0].Models))
	}
	if runs[0].StartResources == nil || runs[0].EndResources == nil {
		t.Fatalf("expected start/end resource snapshots to round-trip")
	}
	if runs[0].StartResources.MemoryAvailableBytes != 12_000_000_000 {
		t.Fatalf("start resources memory_available_bytes = %d", runs[0].StartResources.MemoryAvailableBytes)
	}
	if len(runs[0].StartModelStates) != 1 || len(runs[0].EndModelStates) != 1 {
		t.Fatalf("expected model state snapshots to round-trip: %+v", runs[0])
	}
	if runs[0].EndModelStates[0].StateClass != "installed_not_loaded" {
		t.Fatalf("end model state = %q, want installed_not_loaded", runs[0].EndModelStates[0].StateClass)
	}
}
