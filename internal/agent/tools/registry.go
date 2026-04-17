package tools

import (
	"context"
	"sync"

	"github.com/rs/zerolog/log"

	"roboticus/internal/llm"
	"roboticus/internal/plugin"
)

// Registry manages available tools for an agent.
//
// v1.0.6: the Registry now also tracks a ToolDescriptor per tool
// (lazily populated) to support semantic tool search — ranking tools
// by cosine similarity to the current query embedding and pruning to
// fit a token budget. See tool_search.go for the ranking pipeline and
// the v1.0.6 Rust-parity closure (roboticus-agent/src/tool_search.rs).
//
// Lifecycle of descriptors:
//  1. Register(tool) — creates a descriptor with nil embedding.
//  2. EmbedDescriptors(ctx, ec) — batch-embeds all descriptors' names
//     + descriptions via the embedding client. Called once at daemon
//     startup after all tools are registered and the embedding client
//     is ready. Idempotent: re-calling refreshes stale embeddings.
//  3. Descriptors() — returns the cached slice (may have nil embeddings
//     if step 2 was skipped or failed; the ranker handles that
//     gracefully — see RankTools).
type Registry struct {
	mu          sync.RWMutex
	tools       map[string]Tool
	descriptors map[string]*ToolDescriptor // parallel cache; keyed by tool name
}

// NewRegistry creates an empty tool registry.
func NewRegistry() *Registry {
	return &Registry{
		tools:       make(map[string]Tool),
		descriptors: make(map[string]*ToolDescriptor),
	}
}

// Register adds a tool. Overwrites if a tool with the same name exists.
// A ToolDescriptor is created in the same call with an initial token
// cost estimate (name + description + parameter-schema length) and a
// nil embedding; the embedding is filled in later via
// EmbedDescriptors.
func (r *Registry) Register(t Tool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tools[t.Name()] = t

	// Token cost: rough estimate of the full tool-def payload
	// injected into an LLM request (name + description + params JSON).
	// Uses the shared estimator for consistency with context builder.
	costText := t.Name() + "\n" + t.Description() + "\n" + string(t.ParameterSchema())
	source, server := classifyToolSource(t)
	r.descriptors[t.Name()] = &ToolDescriptor{
		Name:        t.Name(),
		Description: t.Description(),
		TokenCost:   llm.EstimateTokens(costText),
		Source:      source,
		MCPServer:   server,
		// Embedding intentionally left nil; EmbedDescriptors fills it.
	}
}

// classifyToolSource returns the provenance of t and (for MCP tools)
// the backing server name. Uses a type assertion on the concrete
// McpBridgeTool type since that's the single entry point for MCP
// bridging — not every operator-defined Tool will expose a Source()
// method, and adding one to the interface would be a breaking
// change for plugin authors.
func classifyToolSource(t Tool) (ToolSource, string) {
	if bridge, ok := t.(*McpBridgeTool); ok {
		return ToolSourceMCP, bridge.serverName
	}
	if _, ok := t.(*PluginBridgeTool); ok {
		return ToolSourcePlugin, ""
	}
	return ToolSourceBuiltIn, ""
}

// EmbedDescriptors batch-embeds every registered tool's (name + "\n" +
// description) via ec and caches the result on the descriptor. Called
// once at daemon startup after all tools (builtins, plugins, MCP
// bridges) are registered.
//
// Behavior on failure: individual embedding failures are logged at
// DEBUG and leave the descriptor's Embedding nil — the ranker scores
// nil-embedded tools at 0 and relies on AlwaysInclude to pin essential
// tools into the selection. A wholesale EmbeddingClient failure logs
// at WARN and returns the error; the registry remains usable (just
// without embeddings, which degrades ranking quality but preserves
// always-include behavior).
//
// Idempotent: callers can re-invoke to refresh stale embeddings after
// plugin hot-reload or similar runtime changes.
func (r *Registry) EmbedDescriptors(ctx context.Context, ec *llm.EmbeddingClient) error {
	if ec == nil {
		log.Debug().Msg("tool_search: no embedding client; tool descriptors will rank at 0")
		return nil
	}

	r.mu.RLock()
	names := make([]string, 0, len(r.descriptors))
	texts := make([]string, 0, len(r.descriptors))
	for name, d := range r.descriptors {
		names = append(names, name)
		texts = append(texts, d.Name+"\n"+d.Description)
	}
	r.mu.RUnlock()

	if len(texts) == 0 {
		return nil
	}

	embeddings, err := ec.Embed(ctx, texts)
	if err != nil {
		log.Warn().Err(err).Int("tool_count", len(texts)).
			Msg("tool_search: batch embedding failed; tools will rank at 0 (always_include still honored)")
		return err
	}
	if len(embeddings) != len(texts) {
		log.Warn().Int("expected", len(texts)).Int("got", len(embeddings)).
			Msg("tool_search: embedding count mismatch; skipping descriptor update")
		return nil
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	for i, name := range names {
		if d, ok := r.descriptors[name]; ok {
			d.Embedding = embeddings[i]
		}
	}
	log.Info().Int("count", len(names)).
		Msg("tool_search: tool descriptors embedded for ranking")
	return nil
}

// Descriptors returns a snapshot of the current tool descriptors.
// Safe to call concurrently with Register / EmbedDescriptors. Nil
// embeddings are possible (tool registered after embedding pass, or
// embedding call failed) — the ranker handles that.
//
// The returned slice is a shallow copy; descriptor pointers alias
// the registry's cache so ranking results reflect subsequent
// EmbedDescriptors updates, but mutating the slice header doesn't
// affect the registry.
func (r *Registry) Descriptors() []*ToolDescriptor {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*ToolDescriptor, 0, len(r.descriptors))
	for _, d := range r.descriptors {
		out = append(out, d)
	}
	return out
}

// Get returns a tool by name, or nil if not found.
func (r *Registry) Get(name string) Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.tools[name]
}

// Unregister removes a tool and its descriptor from the registry.
func (r *Registry) Unregister(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.tools, name)
	delete(r.descriptors, name)
}

// SyncPluginTools refreshes plugin-backed bridge tools from the live plugin registry.
func (r *Registry) SyncPluginTools(pluginReg *plugin.Registry) int {
	return RegisterPluginTools(r, pluginReg)
}

// List returns all registered tools.
func (r *Registry) List() []Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]Tool, 0, len(r.tools))
	for _, t := range r.tools {
		result = append(result, t)
	}
	return result
}

// Names returns the names of all registered tools.
func (r *Registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.tools))
	for name := range r.tools {
		names = append(names, name)
	}
	return names
}

// NamesWithDescriptions returns (name, description) pairs for all registered tools.
func (r *Registry) NamesWithDescriptions() [][2]string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	pairs := make([][2]string, 0, len(r.tools))
	for _, t := range r.tools {
		pairs = append(pairs, [2]string{t.Name(), t.Description()})
	}
	return pairs
}

// ToolDefs returns LLM-compatible tool definitions for all registered tools.
func (r *Registry) ToolDefs() []llm.ToolDef {
	r.mu.RLock()
	defer r.mu.RUnlock()
	defs := make([]llm.ToolDef, 0, len(r.tools))
	for _, t := range r.tools {
		defs = append(defs, llm.ToolDef{
			Type: "function",
			Function: llm.ToolFuncDef{
				Name:        t.Name(),
				Description: t.Description(),
				Parameters:  t.ParameterSchema(),
			},
		})
	}
	return defs
}
