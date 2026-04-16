package pipeline

// AnnotateToolSearchTrace writes tool-pruning telemetry onto the current
// trace span under the `tool_search.*` namespace. Matches Rust's
// `annotate_tool_search` in crates/roboticus-pipeline/src/trace_helpers.rs,
// which is the canonical trace contract downstream consumers (dashboards,
// operator forensics, parity audits) expect.
//
// Keys emitted (all under TraceNSToolSearch):
//   - candidates_considered — total tools ranked before pruning
//   - candidates_selected   — tools kept after ranking + budget
//   - candidates_pruned     — tools dropped
//   - token_savings         — token delta between ranked and selected sets
//   - top_scores            — name→score map for the selected set
//                             (up to 10; omitted when the ranker ran without
//                             a query embedding, matching Rust's
//                             skip_serializing_if behavior)
//   - embedding_status      — "ok" when both query and tool embeddings
//                             succeeded; Go additionally emits
//                             "no_query_embedding" and "no_tool_embeddings"
//                             for richer diagnostics than Rust's "ok"/"failed"
//                             pair. Preserved as an intentional Go
//                             improvement (see System 01 Intentional
//                             Deviations).
//
// Safe to call with a nil recorder — no-ops. Safe to call outside a span —
// the underlying Annotate is a no-op when no span is open.

import (
	agenttools "roboticus/internal/agent/tools"
)

// AnnotateToolSearchTrace annotates the current span with tool-search stats.
// Pass a zero-valued stats struct to emit zero-valued keys (useful when a
// stage wants to record that pruning ran but produced a degenerate result).
func AnnotateToolSearchTrace(tr *TraceRecorder, stats agenttools.ToolSearchStats) {
	if tr == nil {
		return
	}
	tr.Annotate(TraceNSToolSearch+".candidates_considered", stats.CandidatesConsidered)
	tr.Annotate(TraceNSToolSearch+".candidates_selected", stats.CandidatesSelected)
	tr.Annotate(TraceNSToolSearch+".candidates_pruned", stats.CandidatesPruned)
	tr.Annotate(TraceNSToolSearch+".token_savings", stats.TokenSavings)

	if len(stats.TopScores) > 0 {
		scores := make(map[string]float64, len(stats.TopScores))
		for _, s := range stats.TopScores {
			scores[s.Name] = s.Score
		}
		tr.Annotate(TraceNSToolSearch+".top_scores", scores)
	}

	if stats.EmbeddingStatus != "" {
		tr.Annotate(TraceNSToolSearch+".embedding_status", stats.EmbeddingStatus)
	}
}
