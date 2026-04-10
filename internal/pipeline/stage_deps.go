// Pipeline stage dependency declarations (C6: dependency narrowing).
//
// Each pipeline stage declares a narrow interface that specifies exactly which
// capabilities it needs. This matches Rust's per-stage typed dep structs from
// stage_deps.rs and serves three purposes:
//
//  1. Documentation: the dependency matrix is explicit, not implicit
//  2. Testability: tests can mock only the deps a stage actually uses
//  3. Audit: adding a new dependency to a stage is visible in code review
//
// The Pipeline orchestrator satisfies all interfaces via its concrete fields.
// Stage functions that need narrowing should accept these interfaces instead of
// the full Pipeline pointer, but existing methods are grandfathered — the
// interfaces are used for NEW stages and for test mocking.

package pipeline

import (
	"roboticus/internal/core"
	"roboticus/internal/db"
	"roboticus/internal/llm"
)

// ── Per-Stage Dependency Interfaces ───────────────────────────────────────

// SessionDeps are the dependencies for session resolution (Stage 4).
type SessionDeps interface {
	DB() *db.Store
}

// RetrievalDeps are the dependencies for memory retrieval (Stage 8.5).
type RetrievalDeps interface {
	MemRetriever() MemoryRetriever
}

// DelegationDeps are the dependencies for delegated execution (Stage 9).
type DelegationDeps interface {
	ToolExec() ToolExecutor
	DB() *db.Store
	BGWorker() *core.BackgroundWorker
}

// SkillDeps are the dependencies for skill-first dispatch (Stage 10).
type SkillDeps interface {
	Skills() SkillMatcher
}

// InferenceCoreDeps are the core inference dependencies (LLM + guards + tools).
type InferenceCoreDeps interface {
	ToolExec() ToolExecutor
	GuardChain() *GuardChain
	LLM() *llm.Service
}

// InferenceMemDeps are the memory-related inference dependencies.
type InferenceMemDeps interface {
	MemIngestor() Ingestor
	NickRefiner() NicknameRefiner
	EmbedClient() *llm.EmbeddingClient
}

// InferenceAsyncDeps are the async + storage inference dependencies.
type InferenceAsyncDeps interface {
	DB() *db.Store
	BGWorker() *core.BackgroundWorker
}

// InferenceDeps are the dependencies for inference execution (Stage 12).
// This is the widest stage — composed from narrower sub-interfaces.
// Individual sub-interfaces can be used for testing when full InferenceDeps
// is not needed.
type InferenceDeps interface {
	InferenceCoreDeps
	InferenceMemDeps
	InferenceAsyncDeps
}

// CacheDeps are the dependencies for cache check/store (Stage 11.5/12.5).
type CacheDeps interface {
	DB() *db.Store
}

// PostTurnCoreDeps are the core post-turn dependencies (store + async).
type PostTurnCoreDeps interface {
	DB() *db.Store
	BGWorker() *core.BackgroundWorker
}

// PostTurnDeps are the dependencies for post-turn processing (Stage 13).
// Composed from PostTurnCoreDeps plus embedding and error reporting.
type PostTurnDeps interface {
	PostTurnCoreDeps
	EmbedClient() *llm.EmbeddingClient
	ErrReporter() *core.ErrorBus
}

// ── Pipeline satisfies all stage dep interfaces ───────────────────────────

// DB returns the database store.
func (p *Pipeline) DB() *db.Store { return p.store }

// ToolExec returns the tool executor.
func (p *Pipeline) ToolExec() ToolExecutor { return p.executor }

// BGWorker returns the background worker pool.
func (p *Pipeline) BGWorker() *core.BackgroundWorker { return p.bgWorker }

// MemRetriever returns the memory retriever.
func (p *Pipeline) MemRetriever() MemoryRetriever { return p.retriever }

// Skills returns the skill matcher.
func (p *Pipeline) Skills() SkillMatcher { return p.skills }

// GuardChain returns the guard chain.
func (p *Pipeline) GuardChain() *GuardChain { return p.guards }

// MemIngestor returns the memory ingestor.
func (p *Pipeline) MemIngestor() Ingestor { return p.ingestor }

// NickRefiner returns the nickname refiner.
func (p *Pipeline) NickRefiner() NicknameRefiner { return p.refiner }

// LLM returns the LLM service.
func (p *Pipeline) LLM() *llm.Service { return p.llmSvc }

// EmbedClient returns the embedding client.
func (p *Pipeline) EmbedClient() *llm.EmbeddingClient { return p.embeddings }

// ErrReporter returns the error bus.
func (p *Pipeline) ErrReporter() *core.ErrorBus { return p.errBus }

// ── Compile-time interface satisfaction checks ────────────────────────────

var (
	_ SessionDeps      = (*Pipeline)(nil)
	_ RetrievalDeps    = (*Pipeline)(nil)
	_ DelegationDeps   = (*Pipeline)(nil)
	_ SkillDeps        = (*Pipeline)(nil)
	_ InferenceCoreDeps  = (*Pipeline)(nil)
	_ InferenceMemDeps   = (*Pipeline)(nil)
	_ InferenceAsyncDeps = (*Pipeline)(nil)
	_ InferenceDeps      = (*Pipeline)(nil)
	_ CacheDeps        = (*Pipeline)(nil)
	_ PostTurnCoreDeps = (*Pipeline)(nil)
	_ PostTurnDeps     = (*Pipeline)(nil)
)

// ── Dependency Matrix (for audit reference) ───────────────────────────────
//
// Stage                  | store | executor | llmSvc | injection | retriever | skills | guards | ingestor | refiner | streamer | bgWorker | embeddings | errBus
// ---------------------- |-------|----------|--------|-----------|-----------|--------|--------|----------|---------|----------|----------|------------|-------
// 1. Validation          |       |          |   ✓    |           |           |        |        |          |         |          |          |            |
// 2. Injection defense   |       |          |        |     ✓     |           |        |        |          |         |          |          |            |
// 3. Dedup tracking      |       |          |        |           |           |        |        |          |         |          |          |            |
// 4. Session resolution  |  ✓    |          |        |           |           |        |        |          |         |          |          |            |
// 5. Message storage     |  ✓    |          |        |           |           |        |        |          |         |          |          |            |
// 6. Turn creation       |  ✓    |          |        |           |           |        |        |          |         |          |          |            |
// 7. Decomposition gate  |       |          |        |           |           |        |        |          |         |          |          |            |
// 7.5 Task synthesis     |       |          |        |           |           |        |        |          |         |          |          |            |
// 8. Authority           |       |          |        |           |           |        |        |          |         |          |          |            |
// 8.5 Memory retrieval   |  ✓    |          |        |           |     ✓     |        |        |          |         |          |          |            |
// 9. Delegation          |  ✓    |    ✓     |        |           |           |        |        |          |         |          |    ✓     |            |
// 10. Skill dispatch     |       |          |        |           |           |   ✓    |        |          |         |          |          |            |
// 11. Shortcut dispatch  |       |          |        |           |           |        |        |          |         |          |          |            |
// 11.5 Cache check       |  ✓    |          |        |           |           |        |   ✓    |          |         |          |          |            |
// 12. Inference          |  ✓    |    ✓     |        |           |           |        |   ✓    |    ✓     |    ✓    |    ✓     |    ✓     |            |   ✓
// 12.5 Cache store       |  ✓    |          |        |           |           |        |        |          |         |          |    ✓     |            |
// 13. Post-turn          |  ✓    |          |        |           |           |        |        |          |         |          |    ✓     |     ✓      |   ✓
