package pipeline

// Pipeline Interface Mapping (Wave 8, #87)
//
// This file defines the Go interfaces that correspond to Rust's 9 pipeline traits.
// The mapping ensures feature parity between the Go and Rust implementations.
//
// Go Interface             -> Rust Trait
// -----------------------------------------------
// InjectionChecker         -> InputDefense (input sanitization & threat scoring)
// MemoryRetriever          -> ContextAssembler (memory retrieval & context window assembly)
// SkillMatcher             -> SkillDispatcher (skill trigger matching before LLM inference)
// ToolExecutor             -> InferenceExecutor (ReAct loop, tool calling, LLM interaction)
// Guard / ContextualGuard  -> OutputGuard (post-inference content filtering & rewriting)
// Ingestor                 -> PostTurnProcessor (background memory ingestion after turn)
// NicknameRefiner          -> SessionEnricher (session metadata refinement)
// StreamPreparer           -> StreamBuilder (streaming inference request preparation)
// IntentClassifier         -> IntentRouter (user intent classification & routing)

import (
	"context"

	agenttools "roboticus/internal/agent/tools"
	"roboticus/internal/core"
	"roboticus/internal/llm"
	"roboticus/internal/session"
)

// InjectionChecker scores input text for prompt injection risk.
// Rust equivalent: InputDefense trait — provides check_input() and sanitize().
type InjectionChecker interface {
	CheckInput(text string) core.ThreatScore
	Sanitize(text string) string
}

// MemoryRetriever fetches relevant memories for context assembly.
// Returns a pre-formatted block of memory text ready for system prompt injection.
// Rust equivalent: ContextAssembler trait — provides retrieve_context().
type MemoryRetriever interface {
	Retrieve(ctx context.Context, sessionID, query string, budget int) string
}

// SkillMatcher attempts to fulfill a request via skill triggers before LLM inference.
// Returns nil if no skill matches.
// Rust equivalent: SkillDispatcher trait — provides try_dispatch().
type SkillMatcher interface {
	TryMatch(ctx context.Context, session *session.Session, content string) *Outcome
}

// ToolExecutor runs the ReAct tool-calling loop for standard inference.
// This is the boundary between the pipeline (orchestration) and agent (reasoning).
// Rust equivalent: InferenceExecutor trait — provides run_loop().
type ToolExecutor interface {
	RunLoop(ctx context.Context, session *session.Session) (content string, turns int, err error)
}

// Ingestor handles post-turn memory ingestion in the background.
// Rust equivalent: PostTurnProcessor trait — provides ingest_turn().
type Ingestor interface {
	IngestTurn(ctx context.Context, session *session.Session)
}

// NicknameRefiner generates session nicknames via LLM.
// Rust equivalent: SessionEnricher trait — provides refine_nickname().
type NicknameRefiner interface {
	Refine(ctx context.Context, session *session.Session)
}

// StreamPreparer builds a streaming inference request without executing it.
// Rust equivalent: StreamBuilder trait — provides prepare_stream().
type StreamPreparer interface {
	PrepareStream(ctx context.Context, session *session.Session) (*llm.Request, error)
}

// ToolPruner selects the query-relevant tool set for a turn before
// inference. Rust-parity seam: `core/tool_prune.rs::prune_tool_definitions`
// owns this in the Rust pipeline; Go runs it as a dedicated pipeline
// stage (stageToolPruning) between memory retrieval and prepare-inference.
//
// Pruner implementations compute ranking against the current user
// query's embedding, honor the operator-configured AlwaysInclude pins,
// and enforce the top-k / token-budget limits. Returning an empty
// selection is valid (e.g. when no tools are registered); returning
// nil is reserved for "pruner not wired on this pipeline" and the
// stage treats that as a skipped span rather than an error.
//
// Implementations must be safe under the pipeline's existing
// context-cancellation model: if ctx.Err() becomes non-nil the pruner
// should return promptly with whatever partial result it has (or an
// empty selection).
type ToolPruner interface {
	PruneTools(ctx context.Context, session *session.Session) (defs []llm.ToolDef, stats agenttools.ToolSearchStats, err error)
}
