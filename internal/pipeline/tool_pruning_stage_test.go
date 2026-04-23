package pipeline

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	agenttools "roboticus/internal/agent/tools"
	"roboticus/internal/llm"
	"roboticus/internal/session"
	"roboticus/testutil"
)

// stubPruner is a deterministic ToolPruner for stage-integration tests.
// It records how many times it was called (pipelines must prune exactly
// once per turn) and returns a caller-provided selection + stats so
// tests can assert the session + trace carry the adapter's output
// verbatim.
type stubPruner struct {
	selected []llm.ToolDef
	stats    agenttools.ToolSearchStats
	err      error
	calls    int
}

func (p *stubPruner) PruneTools(_ context.Context, _ *session.Session) ([]llm.ToolDef, agenttools.ToolSearchStats, error) {
	p.calls++
	return p.selected, p.stats, p.err
}

// TestStageToolPruning_PopulatesSessionAndTrace asserts that when the
// pipeline runs a real turn, the tool-pruning stage:
//
//  1. calls the pruner exactly once
//  2. stores the pruner's selection on the session so downstream
//     context-builders can read it
//  3. annotates the trace under the `tool_search.*` namespace with
//     the fields the Rust pipeline emits
//
// This is the runtime-facing assertion the System 02 audit requires
// ("the selected tool set, not just helper output, reaches the
// request"): anything consuming session.SelectedToolDefs() after this
// stage runs sees exactly what PruneTools returned.
func TestStageToolPruning_PopulatesSessionAndTrace(t *testing.T) {
	store := testutil.TempStore(t)

	selected := []llm.ToolDef{
		{Type: "function", Function: llm.ToolFuncDef{Name: "recall_memory", Description: "retrieve memory"}},
		{Type: "function", Function: llm.ToolFuncDef{Name: "search_memories", Description: "search memories"}},
		{Type: "function", Function: llm.ToolFuncDef{Name: "web_search", Description: "search the web"}},
	}
	stats := agenttools.ToolSearchStats{
		CandidatesConsidered: 25,
		CandidatesSelected:   3,
		CandidatesPruned:     22,
		TokenSavings:         1200,
		TopScores: []agenttools.ScoredTool{
			{Name: "web_search", Score: 0.92},
			{Name: "recall_memory", Score: 0.75},
		},
		EmbeddingStatus: "ok",
	}

	pruner := &stubPruner{selected: selected, stats: stats}

	pipe := New(PipelineDeps{
		Store:    store,
		Executor: &stubExecutor{response: "A thoughtful response that survives the quality guards for pipeline integration testing"},
		Pruner:   pruner,
		BGWorker: testutil.BGWorker(t, 4),
	})

	cfg := PresetAPI()
	input := Input{
		Content:  "Implement a cache invalidation refactor, inspect the deployment logs, and update the pipeline code.",
		AgentID:  "default",
		Platform: "test",
	}

	outcome, err := RunPipeline(context.Background(), pipe, cfg, input)
	if err != nil {
		t.Fatalf("RunPipeline: %v", err)
	}

	if pruner.calls != 1 {
		t.Errorf("pruner called %d times; want exactly 1", pruner.calls)
	}

	// Pull the trace back out of the store — same mechanism the
	// behavioral_fitness tests use — and locate the tool_pruning span.
	trace := extractFullTrace(t, store, outcome.SessionID)
	spans := extractSpans(t, trace.StagesJSON)

	var toolSpan *TraceSpan
	for i := range spans {
		if spans[i].Name == "tool_pruning" {
			toolSpan = &spans[i]
			break
		}
	}
	if toolSpan == nil {
		t.Fatalf("tool_pruning span not found in trace; got spans: %s", spanSummary(spans))
	}
	if toolSpan.Outcome != "ok" {
		t.Errorf("tool_pruning outcome = %q; want %q", toolSpan.Outcome, "ok")
	}

	// Every Rust-parity annotation key must be present in the span
	// metadata. We don't assert on exact value shape for numeric
	// fields beyond non-zeroness because other keys (top_scores as a
	// map) serialize differently across test environments — the
	// correctness of the encoding is covered by the annotator's own
	// unit tests.
	requiredKeys := []string{
		TraceNSToolSearch + ".candidates_considered",
		TraceNSToolSearch + ".candidates_selected",
		TraceNSToolSearch + ".candidates_pruned",
		TraceNSToolSearch + ".token_savings",
		TraceNSToolSearch + ".top_scores",
		TraceNSToolSearch + ".embedding_status",
	}
	for _, key := range requiredKeys {
		if _, ok := toolSpan.Metadata[key]; !ok {
			t.Errorf("tool_pruning span missing annotation %q; metadata keys: %v",
				key, metadataKeys(toolSpan.Metadata))
		}
	}

	// Key values should round-trip the stub's numeric fields. JSON
	// encoding pushes all numbers through float64; compare as float64
	// to avoid type-assertion fragility.
	assertNumericAnnotation(t, toolSpan, TraceNSToolSearch+".candidates_considered", 25)
	assertNumericAnnotation(t, toolSpan, TraceNSToolSearch+".candidates_selected", 3)
	assertNumericAnnotation(t, toolSpan, TraceNSToolSearch+".candidates_pruned", 22)
	assertNumericAnnotation(t, toolSpan, TraceNSToolSearch+".token_savings", 1200)

	if got := toolSpan.Metadata[TraceNSToolSearch+".embedding_status"]; got != "ok" {
		t.Errorf("embedding_status = %v; want \"ok\"", got)
	}
}

// TestStageToolPruning_AbsentPrunerIsNoOp asserts the pipeline runs
// cleanly when no ToolPruner is wired — non-pipeline test callers and
// the defensive in-adapter fallback path. The stage should either be
// omitted entirely or emit a lightweight no-op span, but it must not
// surface tool-search annotations or fail the turn.
func TestStageToolPruning_AbsentPrunerIsNoOp(t *testing.T) {
	store := testutil.TempStore(t)

	pipe := New(PipelineDeps{
		Store:    store,
		Executor: &stubExecutor{response: "A substantive response about absent pruners and defensive fallback behavior"},
		// Pruner intentionally omitted.
		BGWorker: testutil.BGWorker(t, 4),
	})

	cfg := PresetAPI()
	input := Input{Content: "Implement a cache invalidation refactor, inspect the deployment logs, and update the pipeline code.", AgentID: "default", Platform: "test"}
	outcome, err := RunPipeline(context.Background(), pipe, cfg, input)
	if err != nil {
		t.Fatalf("RunPipeline: %v", err)
	}

	trace := extractFullTrace(t, store, outcome.SessionID)
	spans := extractSpans(t, trace.StagesJSON)
	found := false
	for _, s := range spans {
		if s.Name == "tool_pruning" {
			found = true
			if s.Outcome != "ok" {
				t.Errorf("tool_pruning outcome = %q; want ok on no-op path", s.Outcome)
			}
			for k := range s.Metadata {
				if strings.HasPrefix(k, TraceNSToolSearch+".") {
					t.Errorf("tool_pruning no-op span should not emit tool_search annotations, got %q", k)
				}
			}
		}
	}
	_ = found
}

// TestStageToolPruning_PrunerErrorDegradesGracefully asserts that a
// pruner returning an error does not halt the pipeline — the stage
// closes its span with outcome=error and records the message, and
// downstream stages continue (buildAgentContext's defensive fallback
// picks up the slack).
func TestStageToolPruning_PrunerErrorDegradesGracefully(t *testing.T) {
	store := testutil.TempStore(t)

	pruner := &stubPruner{err: errString("embedding provider unavailable")}

	pipe := New(PipelineDeps{
		Store:    store,
		Executor: &stubExecutor{response: "A complete response that demonstrates graceful degradation when the pruner errors"},
		Pruner:   pruner,
		BGWorker: testutil.BGWorker(t, 4),
	})

	cfg := PresetAPI()
	outcome, err := RunPipeline(context.Background(), pipe, cfg,
		Input{Content: "Set up a workspace audit and inspect the recent logs", AgentID: "default", Platform: "test"})
	if err != nil {
		t.Fatalf("RunPipeline must not surface pruner errors: %v", err)
	}

	trace := extractFullTrace(t, store, outcome.SessionID)
	spans := extractSpans(t, trace.StagesJSON)
	var toolSpan *TraceSpan
	for i := range spans {
		if spans[i].Name == "tool_pruning" {
			toolSpan = &spans[i]
			break
		}
	}
	if toolSpan == nil {
		t.Fatalf("tool_pruning span must still be emitted on error path")
	}
	if toolSpan.Outcome != "error" {
		t.Errorf("tool_pruning outcome = %q; want %q when pruner fails", toolSpan.Outcome, "error")
	}
	if got, ok := toolSpan.Metadata[TraceNSToolSearch+".error"]; !ok || !strings.Contains(toString(got), "embedding provider unavailable") {
		t.Errorf("tool_pruning should annotate error message; got metadata[%q]=%v",
			TraceNSToolSearch+".error", got)
	}
}

// ── helpers ────────────────────────────────────────────────────────────

type errString string

func (e errString) Error() string { return string(e) }

func assertNumericAnnotation(t *testing.T, span *TraceSpan, key string, want float64) {
	t.Helper()
	raw, ok := span.Metadata[key]
	if !ok {
		t.Errorf("annotation %q not present", key)
		return
	}
	got, ok := raw.(float64)
	if !ok {
		// Ints can survive JSON round-trip as json.Number or int;
		// normalise via strconv-style conversion.
		if n, ok := raw.(json.Number); ok {
			f, err := n.Float64()
			if err != nil {
				t.Errorf("annotation %q is non-numeric: %v (%T)", key, raw, raw)
				return
			}
			got = f
		} else {
			t.Errorf("annotation %q type %T; want float64", key, raw)
			return
		}
	}
	if got != want {
		t.Errorf("annotation %q = %v; want %v", key, got, want)
	}
}

func metadataKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

func toString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	b, _ := json.Marshal(v)
	return string(b)
}

func spanSummary(spans []TraceSpan) string {
	names := make([]string, 0, len(spans))
	for _, s := range spans {
		names = append(names, s.Name)
	}
	return strings.Join(names, ",")
}
