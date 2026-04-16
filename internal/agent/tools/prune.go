// prune.go is the orchestration entry point for query-time tool
// selection. Callers (daemon context builder, HTTP route handlers)
// pass their Registry + embedding client + current user query and
// get back the pruned []llm.ToolDef ready to attach to an LLM
// request.
//
// The three pieces fit together as:
//
//   Registry.Descriptors()   → candidate set (with cached embeddings)
//   EmbeddingClient.EmbedSingle(query) → query vector
//   SearchAndPrune(candidates, query, config) → selected set
//   toToolDefs(selected)     → the injected list
//
// Any failure in embedding gracefully degrades to "descriptors
// without embeddings score 0 and AlwaysInclude still pins the
// essentials" — see the EmbeddingStatus field in ToolSearchStats.

package tools

import (
	"context"
	"encoding/json"

	"github.com/rs/zerolog/log"

	"roboticus/internal/llm"
)

// SelectToolDefs runs the query-time ranking pipeline and returns
// the selected []llm.ToolDef plus telemetry stats. Intended caller:
// daemon context-builder wiring (daemon_adapters.go), which
// previously injected every tool via Registry.ToolDefs().
//
// Zero-arg semantics:
//   - If registry is nil or has no tools, returns an empty slice +
//     zero stats.
//   - If query is empty (no user message yet — e.g., a turn-zero
//     warm-up with nothing but system context), skip embedding and
//     select AlwaysInclude only. This is the cheapest possible
//     tool payload while still giving the model memory + delegation
//     reach.
//   - If ec is nil or fails, proceed with zero-scored descriptors.
//     AlwaysInclude still works; non-essential tools score equally
//     and fall out by token budget.
func SelectToolDefs(ctx context.Context, registry *Registry, ec *llm.EmbeddingClient, query string, config ToolSearchConfig) ([]llm.ToolDef, ToolSearchStats) {
	if registry == nil {
		return nil, ToolSearchStats{}
	}

	descriptors := registry.Descriptors()
	if len(descriptors) == 0 {
		return nil, ToolSearchStats{}
	}

	var queryEmbedding []float32
	if query != "" && ec != nil {
		emb, err := ec.EmbedSingle(ctx, query)
		if err != nil {
			log.Debug().Err(err).Msg("tool_search: query embed failed; tool ranking falls back to always_include only")
			// queryEmbedding stays nil; SearchAndPrune handles it.
		} else {
			queryEmbedding = emb
		}
	}

	selected, stats := SearchAndPrune(descriptors, queryEmbedding, config)

	defs := make([]llm.ToolDef, 0, len(selected))
	for _, r := range selected {
		defs = append(defs, llm.ToolDef{
			Type: "function",
			Function: llm.ToolFuncDef{
				Name:        r.Descriptor.Name,
				Description: r.Descriptor.Description,
				Parameters:  lookupSchema(registry, r.Descriptor.Name),
			},
		})
	}
	return defs, stats
}

// lookupSchema fetches the JSON parameter schema for a named tool
// from the registry. Kept separate from ToolDescriptor so the
// descriptor (rank-relevant fields only) stays lightweight.
// Returns nil for unknown tools — a defensive case the ranker
// shouldn't produce but the type system doesn't prevent.
func lookupSchema(registry *Registry, name string) json.RawMessage {
	tool := registry.Get(name)
	if tool == nil {
		return nil
	}
	return tool.ParameterSchema()
}
