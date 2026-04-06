# Roboticus Parity Implementation Design

**Date**: 2026-04-01
**Status**: Approved
**Approach**: Pipeline-Centric (Approach B)
**Module Style**: Idiomatic Go consolidation
**Testing**: Table-driven unit tests per module, 80%+ coverage target on new code
**Prerequisite**: Phase 0 pipeline interface extraction before any feature work

---

## 1. Context

Roboticus is a Go port of Roboticus (Rust). The Connector-Factory architecture is correctly
implemented: route handlers use `pipeline.Runner` interface, channel adapters implement
`channel.Adapter`, and the architecture test enforces no `internal/agent` imports in routes.

Current parity sits at approximately 57% by module count, with the deepest gaps in:

- **Agent intelligence** (49%): Missing action planner, subagents, compaction, retrieval strategy
- **Database persistence** (14%): 6 files vs. roboticus's 43 specialized repositories
- **Pipeline stage depth** (37%): Missing flight recorder, guard retry, quality gate, tool prune

Channels (83%), wallet (71%), and CLI (100%+) are near or at parity.

The single Clean Architecture violation is `internal/pipeline/pipeline.go` importing
`internal/agent` directly for concrete types. This is resolved in Phase 0.

---

## 2. Architectural Principles

All work follows these non-negotiable principles from roboticus's ARCHITECTURE.md:

### Connector-Factory Pattern
- All business logic lives in `internal/pipeline/`
- Connectors (routes, channel adapters, cron) do exactly three things: Parse, Call, Format
- Test: "If I deleted this connector and wrote a new one, would the new channel get all the same behavior?"

### Dependency Rule (Clean Architecture)
- Dependencies point inward: routes -> pipeline -> agent/llm/db -> core
- Pipeline depends on interfaces, not concrete agent types
- Core has zero upward imports

### Cognitive Scaffold, Not Tool Harness
- Every pipeline stage either enriches the model's input or validates its output
- Features that only give the model "hands" without improving cognition are incomplete
- When a guard rejects, the framework preserves the reasoning chain

### Go Idioms
- Interfaces defined at the consumer site (pipeline defines what it needs from agent)
- Compile-time interface satisfaction: `var _ Runner = (*Pipeline)(nil)`
- `context.Context` threaded through all blocking operations
- Table-driven tests with `t.Run` subtests
- Function-field mocks (no mock framework dependency)
- Error wrapping with `%w` for `errors.Is`/`errors.As` compatibility
- Domain types in `core`, not raw SQL rows leaking through layers

---

## 3. Phase 0: Pipeline Interface Extraction

**Goal**: Remove `internal/pipeline`'s import of `internal/agent`. The pipeline depends on
interfaces it defines; `daemon.go` wires concrete implementations.

### New File: `pipeline/interfaces.go`

```go
package pipeline

import "context"

// InjectionChecker scores input text for prompt injection risk.
type InjectionChecker interface {
    CheckInput(text string) ThreatScore
    Sanitize(text string) string
}

// MemoryRetriever fetches relevant memories for context assembly.
type MemoryRetriever interface {
    Retrieve(ctx context.Context, sessionID, query string, budget int) ([]MemoryFragment, error)
}

// SkillMatcher attempts to fulfill a request via skill triggers.
type SkillMatcher interface {
    TryMatch(ctx context.Context, content string) *Outcome
}

// ToolExecutor runs the ReAct tool-calling loop.
type ToolExecutor interface {
    RunLoop(ctx context.Context, session *Session, cfg LoopConfig) (*Outcome, error)
}

// Ingestor handles post-turn memory ingestion.
type Ingestor interface {
    IngestTurn(ctx context.Context, sessionID string, messages []Message) error
}

// NicknameRefiner generates session nicknames via LLM.
type NicknameRefiner interface {
    Refine(ctx context.Context, sessionID string, messages []Message) (string, error)
}
```

### Changes to `pipeline/pipeline.go`

- Remove `import "roboticus/internal/agent"`
- Replace concrete types in `PipelineDeps` with interfaces:
  - `*agent.InjectionDetector` -> `InjectionChecker`
  - `*agent.ToolRegistry` -> removed (pipeline uses `ToolExecutor` for the loop)
  - `*agent.MemoryRetriever` -> `MemoryRetriever`
  - `*agent.MemoryManager` -> `Ingestor`
  - `[]*agent.LoadedSkill` -> `SkillMatcher`
  - `agent.LoopConfig` / `agent.ContextConfig` / `agent.PromptConfig` -> moved to `pipeline` or `core`
  - `*agent.PolicyEngine` -> removed (policy is internal to ToolExecutor)

### Changes to `daemon/daemon.go`

- Construct concrete agent types and pass them as interface implementations
- This is the composition root where all dependencies are wired

### New Test Files

- `pipeline/mock_test.go` — function-field mocks for all interfaces
- Update all existing pipeline tests to use mocks instead of real agent instances

### New File: `pipeline/types.go`

Shared types that pipeline interfaces reference. These must live in `pipeline` (not `agent`)
so the pipeline package is self-contained:

- `ThreatScore` — already exists in `core/types.go` as `ThreatScore`, reuse via import
- `MemoryFragment` — new struct: `{Tier, Content, Score, Timestamp}`
- `LoopConfig` — move from `agent.LoopConfig` (max_turns, max_tool_calls, etc.)
- `Session` — pipeline already has this concept; ensure it's the pipeline's own type
  (agent's `Session` adapts to it at the boundary in `daemon.go`)
- `Message` — use `llm.Message` (already a shared type)
- `ToolDef` — new struct: `{Name, Description, ParametersJSON, RiskLevel}` for tool pruning

### Migration Checklist

1. Create `pipeline/interfaces.go` with all interface definitions
2. Create `pipeline/types.go` with shared types (`MemoryFragment`, `LoopConfig`, `ToolDef`)
3. Update `Pipeline` struct fields to use interfaces
4. Update `PipelineDeps` struct fields to use interfaces
5. Create mock implementations in test files
6. Update `daemon.go` to wire concrete -> interface (composition root)
7. Run `go build ./...` and `go test ./...` — zero behavioral change
8. Verify architecture test still passes

---

## 4. Phase 1: Pipeline Stage Completeness

### 1a. Flight Recorder (`pipeline/flight_recorder.go`)

Step-level observability for the ReAct loop.

**Types:**

```go
type ReactTrace struct {
    Steps     []ReactStep
    StartedAt time.Time
    TotalMs   int64
}

type ReactStep struct {
    Kind       StepKind   // ToolCall, LLMCall, GuardCheck, Retry
    Name       string
    DurationMs int64
    Success    bool
    Source     ToolSource // Builtin, MCP, Plugin, Skill
    Input      string     // truncated to 500 chars for storage
    Output     string     // truncated to 500 chars for storage
}

type StepKind int
const (
    StepToolCall  StepKind = iota
    StepLLMCall
    StepGuardCheck
    StepRetry
)

type ToolSource struct {
    Kind   string // "builtin", "mcp", "plugin", "skill"
    Server string // MCP server name or plugin name
}
```

**Integration**: The `ToolExecutor` interface gains an optional `ReactTrace` parameter
(or the executor returns it alongside the Outcome). The pipeline persists it after inference
via `db/traces_repo.go`.

**Tests**: Verify step recording, truncation, serialization roundtrip.

### 1b. Guard Retry (`pipeline/guard_retry.go`)

When a guard rejects, re-invoke inference with the rejection reason injected as context.

```go
type RetryPolicy struct {
    MaxRetries    int  // default 2
    InjectReason  bool // append guard rejection reason to next prompt
    PreserveChain bool // carry forward rejected response as context
}

func (p *Pipeline) retryInference(
    ctx context.Context, cfg Config, session *Session,
    rejected string, reason string, policy RetryPolicy,
) (*Outcome, error)
```

**Behavior**: On guard rejection with `Retry: true`, the pipeline:
1. Appends a system message: "Your previous response was rejected: {reason}. Please revise."
2. If `PreserveChain`, includes the rejected text as context
3. Re-runs inference (up to `MaxRetries`)
4. Each retry is recorded in the flight recorder as `StepRetry`

**Tests**: Table-driven: guard passes first time (no retry), guard rejects once then passes,
guard rejects past max retries (fallback triggered), context preservation verification.

### 1c. Guard Fallback (`pipeline/guard_fallback.go`)

When retries are exhausted, produce a continuity-preserving fallback.

```go
func (p *Pipeline) fallbackResponse(
    ctx context.Context, session *Session,
    lastAttempt string, guardName string, reason string,
) *Outcome
```

**Behavior**: Generates a response that acknowledges the limitation without losing the
reasoning chain. The fallback includes:
- What the agent was trying to do (from session history)
- Why it couldn't complete (guard name + reason)
- A suggestion for how to proceed

**Tests**: Verify fallback contains context from session, doesn't contain rejected content,
is non-empty.

### 1d. Guard Registry (`pipeline/guard_registry.go`)

Named guard sets materialized from preset enums.

```go
type GuardRegistry struct {
    guards map[string]Guard
}

func NewGuardRegistry() *GuardRegistry
func (r *GuardRegistry) Register(g Guard)
func (r *GuardRegistry) Get(name string) (Guard, bool)
func (r *GuardRegistry) Chain(preset GuardSetPreset) *GuardChain
```

Replaces the current free functions `FullGuardChain()` and `StreamGuardChain()` in
`guards.go`. The registry is constructed once at startup and passed into the pipeline.

**Tests**: Verify all presets produce expected guard counts, registry lookup by name.

### 1e. Tool Prune (`pipeline/tool_prune.go`)

Token-budget-aware tool list pruning before inference.

```go
type ToolPruner struct {
    maxToolTokens int
    embedder      Embedder // optional
}

type Embedder interface {
    Embed(ctx context.Context, text string) ([]float32, error)
}

func (tp *ToolPruner) Prune(
    ctx context.Context, tools []ToolDef, query string, budget int,
) []ToolDef
```

**Strategy** (in priority order):
1. Always include tools used in current session
2. If embedder available, rank remaining by cosine similarity to query
3. Drop lowest-relevance tools until under budget
4. If no embedder, sort by recent usage frequency

**Tests**: Budget enforcement, relevance ordering, fallback without embedder.

### 1f. Intent Registry (`pipeline/intent_registry.go`)

Semantic intent classification for routing decisions.

```go
type IntentRegistry struct {
    classifiers []IntentClassifier
}

type IntentClassifier interface {
    Classify(content string) (Intent, float64)
}

type Intent string
const (
    IntentQuestion Intent = "question"
    IntentCommand  Intent = "command"
    IntentCreative Intent = "creative"
    IntentAnalysis Intent = "analysis"
    IntentChat     Intent = "chat"
)

func (ir *IntentRegistry) Classify(content string) (Intent, float64)
```

Default classifier uses keyword heuristics. Can be upgraded to embedding-based via
Phase 3's `SemanticClassifier`.

**Tests**: Known-intent inputs, ambiguous inputs, empty input handling.

### 1g. Bot Commands (`pipeline/bot_commands.go`)

Handle `/command` style inputs in channel contexts, bypassing LLM inference.

```go
type BotCommandHandler struct {
    commands map[string]CommandFunc
}

type CommandFunc func(ctx context.Context, args string, session *Session) (*Outcome, error)

func (h *BotCommandHandler) TryHandle(
    ctx context.Context, content string, session *Session,
) (*Outcome, bool)
```

Built-in commands: `/help`, `/status`, `/memory search <query>`, `/tools`, `/skills`.

**Tests**: Command matching, args parsing, unknown command passthrough.

### 1h. DB: Trace Repository (`db/traces_repo.go`)

Replaces inline SQL in `pipeline.go:storeTrace`.

```go
type TraceRepository struct {
    q Querier
}

func (r *TraceRepository) SavePipelineTrace(ctx context.Context, row PipelineTraceRow) error
func (r *TraceRepository) SaveReactTrace(ctx context.Context, traceID, reactJSON string) error
func (r *TraceRepository) ListTraces(ctx context.Context, filter TraceFilter) ([]PipelineTraceRow, error)
func (r *TraceRepository) GetByTurnID(ctx context.Context, turnID string) (*PipelineTraceRow, error)
```

**Migration 016**: `react_traces` table.

---

## 5. Phase 2: Agent Cognitive Scaffold

### 2a. Subagent Manager (`agent/subagents.go`)

Concurrent subagent lifecycle with bounded concurrency.

```go
type SubagentManager struct {
    mu         sync.RWMutex
    agents     map[string]*AgentInstance
    semaphore  chan struct{}
    maxSlots   int
    allowedIDs []string
}

type AgentInstance struct {
    ID, Config, Status, Error, StartedAt, UpdatedAt
}

type AgentStatus int // Registered, Running, Stopped, Error

// Methods: Register, Start, Stop, Unregister, AcquireSlot, ListAgents, RunningCount
```

**Concurrency**: Buffered channel as semaphore (`make(chan struct{}, maxSlots)`).
`AcquireSlot` does a context-aware channel send; on context cancellation returns error.

**DB**: `db/agents_repo.go` — `SaveAgent`, `ListAgents`, `UpdateAgentStatus`, `DeleteAgent`.

**Tests**: Lifecycle transitions, slot exhaustion, concurrent start/stop, allowlist enforcement.

### 2b. Action Planner (`agent/action_planner.go`)

Pure function: operating state + input -> planned action.

```go
type PlannedAction int // ActionInfer, ActionDelegate, ActionSkillExec, ActionRetrieve, ActionEscalate, ActionWait

type ActionPlan struct {
    Action     PlannedAction
    Reason     string
    Confidence float64
    Context    map[string]any
}

func PlanNextAction(state *OperatingState, input string, history []Message) ActionPlan
```

**Decision logic** (priority order):
1. Pending approval -> `ActionWait`
2. Pending delegation with results -> `ActionDelegate`
3. Matched skill trigger -> `ActionSkillExec`
4. Memory-only query detected -> `ActionRetrieve`
5. Low confidence on current tier -> `ActionEscalate`
6. Default -> `ActionInfer`

**Tests**: Table-driven with all action paths. Pure function = highly testable.

### 2c. Task State (`agent/task_state.go`)

Type-safe task lifecycle extending existing `OperatingState`.

```go
type TaskPhase int // TaskPending, TaskPlanning, TaskExecuting, TaskValidating, TaskComplete, TaskFailed

type TaskState struct {
    ID, Phase, ParentID, Goal, Steps, CurrentStep, CreatedAt, UpdatedAt
}

type TaskStep struct {
    Description, Status, ToolCalls, Output
}
```

**DB**: `db/tasks_repo.go` — `CreateTask`, `UpdateTaskPhase`, `ListTasks`, `GetTask`, `ListSubtasks`.

**Tests**: Phase transitions (valid and invalid), subtask creation, step completion.

### 2d. Retrieval Strategy (`agent/retrieval_strategy.go`)

Adaptive retrieval mode selection.

```go
type RetrievalMode int // Hybrid, Semantic, Keyword, ANN, Recency

type RetrievalStrategy struct {
    embeddingAvailable bool
    corpusSize         int
    annThreshold       int // default 1000
}

func (rs *RetrievalStrategy) SelectMode(query string, sessionAge time.Duration) RetrievalMode
```

**Decision logic**:
- No embeddings -> `Keyword`
- Session < 5 min old -> `Recency`
- Corpus > annThreshold -> `ANN`
- Default -> `Hybrid`

**Tests**: All mode selection paths, threshold boundaries.

### 2e. Compaction (`agent/compaction.go`)

Context window compression for long sessions.

```go
type Compactor struct {
    maxTokens  int
    summarizer Summarizer
}

type Summarizer interface {
    Summarize(ctx context.Context, messages []Message) (string, error)
}

func (c *Compactor) Compact(ctx context.Context, messages []Message, budget int) ([]Message, error)
```

**Strategy**: Keep most recent N messages verbatim (where N fills ~60% of budget),
summarize older messages into a single context block for remaining ~40%.

**Tests**: Under-budget passthrough, over-budget compaction, summarizer error handling.

### 2f. Capability Discovery (`agent/capability.go`)

Self-knowledge for the model.

```go
type CapabilityRegistry struct {
    tools, skills, plugins, mcp []Capability
}

func (cr *CapabilityRegistry) Discover() CapabilityManifest
func (cr *CapabilityRegistry) DiscoverForPrompt(budget int) string
```

**Tests**: Registry population, prompt budget enforcement, empty registry handling.

### 2g. Tool Output Filter (`agent/tool_output_filter.go`)

Truncate/summarize verbose tool outputs.

```go
type OutputFilter struct {
    maxOutputTokens int // default 2000
}

func (f *OutputFilter) Filter(toolName string, output string) string
```

**Tests**: Under-limit passthrough, truncation with marker, empty output.

### 2h. Governor (`agent/governor.go`)

Rate-limiting and cost-control.

```go
type Governor struct {
    maxTurnsPerSession, maxTokensPerSession int
    maxCostPerSession float64
    cooldownAfterError time.Duration
}

type GovernorDecision int // Allow, Throttle, Deny

func (g *Governor) Check(session *Session, tokensSoFar int, costSoFar float64) GovernorDecision
```

**Tests**: All decision paths, boundary conditions.

### Phase 2 DB Dependencies

**Migration 017**: `agent_instances`, `agent_tasks`, `task_steps`, `delegation_outcomes` tables.

| File | Methods |
|------|---------|
| `db/agents_repo.go` | `SaveAgent`, `ListAgents`, `UpdateAgentStatus`, `DeleteAgent` |
| `db/tasks_repo.go` | `CreateTask`, `UpdateTaskPhase`, `ListTasks`, `GetTask`, `ListSubtasks` |
| `db/delegation_repo.go` | `SaveDelegation`, `ListDelegations`, `UpdateDelegationOutcome` |

---

## 6. Phase 3: DB + LLM Infrastructure

### DB Repository Expansion

Consolidate all inline SQL into domain-grouped repositories.

| File | Covers | Key Methods |
|------|--------|-------------|
| `db/memory_repo.go` | 5-tier memory, hippocampus | `StoreMemory`, `QueryByTier`, `HybridSearch`, `DecayEpisodic`, `Consolidate` |
| `db/cache_repo.go` | semantic cache | `Lookup`, `Store`, `Evict`, `Stats` |
| `db/approvals_repo.go` | approval requests | `Create`, `ListPending`, `Approve`, `Deny` |
| `db/tools_repo.go` | tool calls, tool embeddings | `RecordCall`, `ListCalls`, `StoreEmbedding`, `SearchByEmbedding` |
| `db/metrics_repo.go` | inference costs, snapshots | `RecordCost`, `AggregateByModel`, `SaveSnapshot`, `ListSnapshots` |
| `db/skills_repo.go` | skill definitions | `Save`, `List`, `IncrementSuccess`, `IncrementFailure` |
| `db/delivery_repo.go` | delivery queue | Migrate SQL from `channel/delivery.go` |

All repositories take `Querier` interface, return domain types from `core`.

### LLM Enhancements

| File | Purpose |
|------|---------|
| `llm/classifier.go` | Semantic intent classification using embeddings |
| `llm/ml_router.go` | Logistic regression model routing (pure Go, no ML deps) |
| `llm/compression.go` | Pre-inference prompt compression |
| `llm/oauth.go` | OAuth2 token management for provider auth |
| `llm/capacity.go` | TPM/RPM sliding-window capacity tracking |

**Migration 018**: `routing_dataset`, `tool_embeddings`, `learned_skills` columns, `metric_snapshots`.

---

## 7. Phase 4: Extended Agent Features

| File | Purpose | LOC Est. |
|------|---------|----------|
| `agent/knowledge.go` | Structured fact storage + graph queries | ~200 |
| `agent/learning.go` | Post-turn pattern extraction | ~150 |
| `agent/consolidation.go` | Memory entry merging | ~150 |
| `agent/topic.go` | Auto-topic classification | ~100 |
| `agent/digest.go` | Session summarization | ~120 |
| `agent/ranking.go` | Memory ranking with recency decay | ~100 |
| `agent/recommendations.go` | Proactive action suggestions | ~130 |
| `agent/speculative.go` | Parallel branch evaluation (goroutine fan-out) | ~200 |
| `agent/manifest.go` | Agent capability manifest for A2A | ~100 |
| `agent/workspace.go` | Multi-agent workspace coordination | ~150 |
| `api/routes/config_apply.go` | Live config apply via admin API | ~100 |
| `api/routes/diagnostics.go` | Self-repair and health diagnostics | ~150 |

**Migration 019**: `knowledge_facts`, `knowledge_edges` (if knowledge graph implemented).

---

## 8. Phase 5: Revenue, Apps, WASM

Only implemented if specific business requirements demand them.

| File | Purpose |
|------|---------|
| `db/revenue_repo.go` | Opportunity scoring, settlement, tax |
| `agent/apps.go` + `cmd/apps.go` | Installable agent applications |
| `agent/wasm.go` | WebAssembly plugin execution |
| `cmd/ingest.go` | Document ingestion |

---

## 9. Testing Strategy

### Per-Module Pattern

Every new `.go` file gets a `_test.go` companion with table-driven tests:

```go
func TestXxx(t *testing.T) {
    tests := []struct {
        name string
        // inputs
        // expected outputs
    }{
        {"happy path", ...},
        {"edge case", ...},
        {"error path", ...},
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) { ... })
    }
}
```

### Coverage Targets

| Layer | Target | Rationale |
|-------|--------|-----------|
| Pipeline interfaces + mocks | 100% | Architectural spine |
| Pure functions (planner, strategy) | 90%+ | No I/O, highly testable |
| DB repositories | 80%+ | Uses `testutil.TempStore` |
| Integration (pipeline -> mock agent -> mock LLM) | 70%+ | End-to-end stage verification |
| CLI commands | 60%+ | Input parsing, error display |

### Mock Infrastructure

Function-field mocks in `testutil/`:

```go
type MockInjectionChecker struct {
    CheckInputFunc func(string) ThreatScore
    SanitizeFunc   func(string) string
}
```

No mock framework dependency. Each mock has safe defaults (e.g., `CheckInput` returns
`ThreatScore(0)` if func field is nil).

### Integration Test Expansion

Extend existing `smoke_test.go` to exercise new pipeline stages:
- Guard retry path (inject a guard that rejects once, then passes)
- Flight recorder populated after inference
- Bot command handling
- Subagent lifecycle

---

## 10. Migration Plan

| Migration File | Phase | Content |
|----------------|-------|---------|
| `016_react_traces.sql` | 1 | `react_traces` table |
| `017_agent_tasks.sql` | 2 | `agent_instances`, `agent_tasks`, `task_steps`, `delegation_outcomes` |
| `018_infrastructure.sql` | 3 | `routing_dataset`, `tool_embeddings`, `metric_snapshots`, `learned_skills` columns |
| `019_knowledge_graph.sql` | 4 | `knowledge_facts`, `knowledge_edges` |

All migrations are additive (CREATE TABLE, ALTER TABLE ADD COLUMN). No destructive changes
to existing tables.

---

## 11. Estimated Scope

| Phase | New Files | Est. LOC | Tests LOC | Duration Est. |
|-------|-----------|----------|-----------|---------------|
| 0 | 2 + 1 migration | ~200 | ~150 | Short |
| 1 | 8 + 1 migration | ~1200 | ~800 | Medium |
| 2 | 8 + 2 repos + 1 migration | ~1500 | ~1000 | Medium-Long |
| 3 | 12 (7 repos + 5 LLM) + 1 migration | ~1800 | ~1200 | Medium-Long |
| 4 | 12 | ~1700 | ~1000 | Medium |
| 5 | 4 (conditional) | ~600 | ~400 | Short |
| **Total** | **~46 new files** | **~7000** | **~4550** | — |

---

## 12. Success Criteria

- `go build ./...` passes at every phase boundary
- `go test ./...` passes at every phase boundary
- `go vet ./...` clean
- Architecture test (`TestArchitecture_RoutesDontImportAgent`) passes
- Pipeline no longer imports `internal/agent` after Phase 0
- Parity audit reports 90%+ coverage across all subsystems after Phase 4
- All new code has table-driven unit tests with 80%+ line coverage
- Every pipeline interface has a mock in `testutil/` and at least one integration test
