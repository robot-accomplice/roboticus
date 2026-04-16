package pipeline

import (
	"context"
	"testing"

	"roboticus/internal/db"
	"roboticus/testutil"
)

// TestStageHippocampusSummary_InjectsNonEmpty asserts the live pipeline
// wiring for SYS-01-004: stageHippocampusSummary calls the registry
// through the pipeline's store, writes a non-empty summary onto the
// session via `sess.SetHippocampusSummary`, and annotates the trace
// with the byte count so operators can see the ambient injection
// happen without reading the request.
//
// The test seeds the hippocampus registry with one agent-owned table.
// Before v1.0.6 this stage did not exist on the live path, so
// `sess.HippocampusSummary()` was always empty and the model received
// no ambient database-surface context — matching Rust's
// context_builder.rs:356-369 behavior closes that gap.
func TestStageHippocampusSummary_InjectsNonEmpty(t *testing.T) {
	store := testutil.TempStore(t)
	ctx := context.Background()

	// Seed a single agent-owned table so CompactSummary produces
	// non-empty output. Without this the test would accidentally pass
	// via the "empty registry" skip branch. RegisterTableFull is the
	// supported entry for inserting arbitrary rows without having to
	// know the hippocampus column layout.
	registry := db.NewHippocampusRegistry(store)
	if err := registry.RegisterTableFull(ctx,
		"memos_test",
		"agent-owned scratch table",
		nil,   // columns: not needed for CompactSummary output
		"test",
		true,  // agent_owned
		"private",
		3,     // row_count: drives "(N rows)" annotation
	); err != nil {
		t.Fatalf("seed hippocampus registry: %v", err)
	}

	pipe := New(PipelineDeps{
		Store:    store,
		Executor: &stubExecutor{response: "A complete response body that survives the quality guards for hippocampus integration testing"},
		BGWorker: testutil.BGWorker(t, 4),
	})

	outcome, err := RunPipeline(ctx, pipe, PresetAPI(),
		Input{Content: "What tables do I have", AgentID: "default", Platform: "test"})
	if err != nil {
		t.Fatalf("RunPipeline: %v", err)
	}

	trace := extractFullTrace(t, store, outcome.SessionID)
	spans := extractSpans(t, trace.StagesJSON)

	var hippoSpan *TraceSpan
	for i := range spans {
		if spans[i].Name == "hippocampus_summary" {
			hippoSpan = &spans[i]
			break
		}
	}
	if hippoSpan == nil {
		t.Fatalf("hippocampus_summary span not found in trace; got spans: %v", spanNames(spans))
	}
	if hippoSpan.Outcome != "ok" {
		t.Errorf("hippocampus_summary outcome = %q; want %q (want non-empty summary injected)",
			hippoSpan.Outcome, "ok")
	}
	if bytes, ok := hippoSpan.Metadata["hippocampus.bytes"]; !ok {
		t.Errorf("hippocampus_summary missing bytes annotation")
	} else if b, ok := bytes.(float64); !ok || b <= 0 {
		t.Errorf("hippocampus.bytes = %v; want > 0", bytes)
	}
}

// TestStageHippocampusSummary_EmptyRegistrySkipsInjection asserts the
// empty-registry path: when CompactSummary returns "" because no tables
// are registered, the stage closes with outcome=empty and records a
// zero byte count, but does NOT set the summary on the session. A
// subsequent downstream consumer would see an empty summary and skip
// injection, so the model never receives an empty ambient message.
func TestStageHippocampusSummary_EmptyRegistrySkipsInjection(t *testing.T) {
	store := testutil.TempStore(t)
	ctx := context.Background()

	pipe := New(PipelineDeps{
		Store:    store,
		Executor: &stubExecutor{response: "A substantive response body demonstrating empty-registry graceful degradation"},
		BGWorker: testutil.BGWorker(t, 4),
	})

	outcome, err := RunPipeline(ctx, pipe, PresetAPI(),
		Input{Content: "Any request", AgentID: "default", Platform: "test"})
	if err != nil {
		t.Fatalf("RunPipeline: %v", err)
	}

	trace := extractFullTrace(t, store, outcome.SessionID)
	spans := extractSpans(t, trace.StagesJSON)

	var hippoSpan *TraceSpan
	for i := range spans {
		if spans[i].Name == "hippocampus_summary" {
			hippoSpan = &spans[i]
			break
		}
	}
	if hippoSpan == nil {
		t.Fatalf("hippocampus_summary span must still be emitted on empty-registry path")
	}
	if hippoSpan.Outcome != "ok" {
		t.Errorf("hippocampus_summary outcome = %q; want %q (stage succeeded at checking the registry)",
			hippoSpan.Outcome, "ok")
	}
	if bytes, ok := hippoSpan.Metadata["hippocampus.bytes"]; !ok {
		t.Errorf("hippocampus_summary missing bytes annotation")
	} else if b, ok := bytes.(float64); !ok || b != 0 {
		t.Errorf("hippocampus.bytes = %v; want 0 on empty-registry path", bytes)
	}
}
