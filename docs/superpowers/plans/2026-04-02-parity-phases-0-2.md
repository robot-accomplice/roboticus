# Parity Implementation Plan — Phases 0-2

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Bring roboticus to full architectural parity with roboticus by extracting pipeline interfaces (Phase 0), completing pipeline stages (Phase 1), and adding the agent cognitive scaffold (Phase 2).

**Architecture:** Pipeline-centric approach — the unified pipeline is the architectural spine. Phase 0 breaks the `pipeline → agent` import dependency by defining interfaces in the pipeline package. Phase 1 adds missing pipeline stages (flight recorder, guard retry/fallback, guard registry, tool prune, intent registry, bot commands). Phase 2 adds agent intelligence modules (subagents, action planner, task state, retrieval strategy, compaction, capability discovery, tool output filter, governor). All new code uses table-driven tests with 80%+ coverage.

**Tech Stack:** Go 1.22+, modernc.org/sqlite (pure Go), chi/v5 router, zerolog logging, testutil.TempStore for DB tests.

**Connection pool rule:** All database access flows through `*db.Store` (which wraps Go's `*sql.DB` connection pool). New repositories accept the `db.Querier` interface — never open new connections or create new `*sql.DB` instances. The daemon creates the single `*db.Store` and passes it to all consumers.

---

## File Structure

### Phase 0: Pipeline Interface Extraction
| Action | File | Responsibility |
|--------|------|----------------|
| Create | `internal/pipeline/interfaces.go` | Pipeline's view of agent capabilities (6 interfaces) |
| Create | `internal/pipeline/types_pipeline.go` | Shared types: `MemoryFragment`, `LoopConfig`, `ToolDef` |
| Modify | `internal/pipeline/pipeline.go` | Replace concrete agent types with interfaces |
| Modify | `internal/pipeline/pipeline_stages.go` | Use interfaces instead of concrete agent types |
| Modify | `internal/daemon/daemon.go` | Wire concrete agent types to pipeline interfaces |
| Create | `internal/pipeline/mock_test.go` | Function-field mocks for all 6 interfaces |

### Phase 1: Pipeline Stage Completeness
| Action | File | Responsibility |
|--------|------|----------------|
| Create | `internal/pipeline/flight_recorder.go` | ReactTrace, ReactStep types + recording |
| Create | `internal/pipeline/flight_recorder_test.go` | Step recording, truncation, JSON roundtrip |
| Create | `internal/pipeline/guard_retry.go` | Re-inference with guard feedback |
| Create | `internal/pipeline/guard_retry_test.go` | Retry paths: pass, fail-then-pass, exhaust |
| Create | `internal/pipeline/guard_fallback.go` | Continuity-preserving fallback on retry exhaustion |
| Create | `internal/pipeline/guard_fallback_test.go` | Fallback content verification |
| Create | `internal/pipeline/guard_registry.go` | Named guard sets from preset enums |
| Create | `internal/pipeline/guard_registry_test.go` | Preset chain counts, lookup by name |
| Create | `internal/pipeline/tool_prune.go` | Token-budget tool list pruning |
| Create | `internal/pipeline/tool_prune_test.go` | Budget, relevance, no-embedder fallback |
| Create | `internal/pipeline/intent_registry.go` | Keyword-based intent classification |
| Create | `internal/pipeline/intent_registry_test.go` | Known intents, ambiguous, empty |
| Create | `internal/pipeline/bot_commands.go` | /command handler bypassing inference |
| Create | `internal/pipeline/bot_commands_test.go` | Match, args, passthrough |
| Create | `internal/db/traces_repo.go` | Pipeline + React trace persistence |
| Create | `internal/db/traces_repo_test.go` | Save/load roundtrip |
| Create | `internal/db/migrations/016_react_traces.sql` | `react_traces` table |

### Phase 2: Agent Cognitive Scaffold
| Action | File | Responsibility |
|--------|------|----------------|
| Create | `internal/agent/subagents.go` | Concurrent subagent lifecycle + semaphore |
| Create | `internal/agent/subagents_test.go` | Lifecycle, slot exhaustion, concurrency |
| Create | `internal/agent/action_planner.go` | Pure function: state → planned action |
| Create | `internal/agent/action_planner_test.go` | All 6 action paths |
| Create | `internal/agent/task_state.go` | Type-safe task lifecycle |
| Create | `internal/agent/task_state_test.go` | Phase transitions, subtasks |
| Create | `internal/agent/retrieval_strategy.go` | Adaptive retrieval mode selection |
| Create | `internal/agent/retrieval_strategy_test.go` | All mode paths, thresholds |
| Create | `internal/agent/compaction.go` | Context window compression |
| Create | `internal/agent/compaction_test.go` | Under-budget, over-budget, error |
| Create | `internal/agent/capability.go` | Self-knowledge capability registry |
| Create | `internal/agent/capability_test.go` | Population, prompt budget, empty |
| Create | `internal/agent/tool_output_filter.go` | Verbose output truncation |
| Create | `internal/agent/tool_output_filter_test.go` | Passthrough, truncation, empty |
| Create | `internal/agent/governor.go` | Rate-limiting + cost-control |
| Create | `internal/agent/governor_test.go` | Allow, throttle, deny paths |
| Create | `internal/db/agents_repo.go` | Agent instance persistence |
| Create | `internal/db/agents_repo_test.go` | CRUD + status transitions |
| Create | `internal/db/tasks_repo.go` | Task + step persistence |
| Create | `internal/db/tasks_repo_test.go` | CRUD + phase transitions |
| Create | `internal/db/delegation_repo.go` | Delegation outcome tracking |
| Create | `internal/db/delegation_repo_test.go` | Save/list/update outcomes |
| Create | `internal/db/migrations/017_agent_tasks.sql` | `agent_instances`, `agent_tasks`, `task_steps`, `delegation_outcomes` |

---

## Phase 0: Pipeline Interface Extraction

### Task 1: Create pipeline interfaces

**Files:**
- Create: `internal/pipeline/interfaces.go`

- [ ] **Step 1: Write the interface definitions**

```go
package pipeline

import (
	"context"

	"roboticus/internal/core"
	"roboticus/internal/llm"
)

// InjectionChecker scores input text for prompt injection risk.
type InjectionChecker interface {
	CheckInput(text string) core.ThreatScore
	Sanitize(text string) string
}

// MemoryRetriever fetches relevant memories for context assembly.
// Returns a pre-formatted block of memory text ready for system prompt injection.
type MemoryRetriever interface {
	Retrieve(ctx context.Context, sessionID, query string, budget int) string
}

// SkillMatcher attempts to fulfill a request via skill triggers before LLM inference.
// Returns nil if no skill matches.
type SkillMatcher interface {
	TryMatch(ctx context.Context, session *Session, content string) *Outcome
}

// ToolExecutor runs the ReAct tool-calling loop for standard inference.
// This is the boundary between the pipeline (orchestration) and agent (reasoning).
type ToolExecutor interface {
	RunLoop(ctx context.Context, session *Session) (content string, turns int, err error)
}

// Ingestor handles post-turn memory ingestion in the background.
type Ingestor interface {
	IngestTurn(ctx context.Context, session *Session)
}

// NicknameRefiner generates session nicknames via LLM.
type NicknameRefiner interface {
	Refine(ctx context.Context, session *Session)
}

// StreamPreparer builds a streaming inference request without executing it.
type StreamPreparer interface {
	PrepareStream(ctx context.Context, session *Session) (*llm.Request, error)
}
```

- [ ] **Step 2: Verify it compiles**

Run: `go build ./internal/pipeline/`
Expected: PASS (no other files reference these interfaces yet)

- [ ] **Step 3: Commit**

```bash
git add internal/pipeline/interfaces.go
git commit -m "refactor(pipeline): define pipeline capability interfaces

Preparation for removing pipeline's direct import of internal/agent.
Defines InjectionChecker, MemoryRetriever, SkillMatcher, ToolExecutor,
Ingestor, NicknameRefiner, and StreamPreparer interfaces."
```

### Task 2: Create pipeline shared types

**Files:**
- Create: `internal/pipeline/types_pipeline.go`

- [ ] **Step 1: Write the shared types**

```go
package pipeline

import (
	"roboticus/internal/core"
	"roboticus/internal/llm"
)

// Session holds the in-flight conversation state for the pipeline.
// This is the pipeline's own type — agent.Session adapts to it at the
// daemon wiring boundary.
type Session struct {
	ID           string
	AgentID      string
	AgentName    string
	Authority    core.AuthorityLevel
	Channel      string
	Workspace    string
	AllowedPaths []string

	messages     []llm.Message
	pendingCalls []llm.ToolCall
}

// NewSession creates a pipeline session with the given identity.
func NewSession(id, agentID, agentName string) *Session {
	return &Session{
		ID:        id,
		AgentID:   agentID,
		AgentName: agentName,
		Authority: core.AuthorityExternal,
	}
}

// Messages returns the full message history.
func (s *Session) Messages() []llm.Message { return s.messages }

// AddUserMessage appends a user message.
func (s *Session) AddUserMessage(content string) {
	s.messages = append(s.messages, llm.Message{Role: "user", Content: content})
}

// AddSystemMessage appends a system message.
func (s *Session) AddSystemMessage(content string) {
	s.messages = append(s.messages, llm.Message{Role: "system", Content: content})
}

// AddAssistantMessage appends an assistant message with optional tool calls.
func (s *Session) AddAssistantMessage(content string, toolCalls []llm.ToolCall) {
	s.messages = append(s.messages, llm.Message{
		Role: "assistant", Content: content, ToolCalls: toolCalls,
	})
	s.pendingCalls = toolCalls
}

// AddToolResult appends a tool result message.
func (s *Session) AddToolResult(callID, toolName, output string, isError bool) {
	content := output
	if isError {
		content = "Error: " + output
	}
	s.messages = append(s.messages, llm.Message{
		Role: "tool", Content: content, ToolCallID: callID, Name: toolName,
	})
	remaining := s.pendingCalls[:0]
	for _, tc := range s.pendingCalls {
		if tc.ID != callID {
			remaining = append(remaining, tc)
		}
	}
	s.pendingCalls = remaining
}

// PendingToolCalls returns tool calls not yet resolved.
func (s *Session) PendingToolCalls() []llm.ToolCall { return s.pendingCalls }

// LastAssistantContent returns the most recent assistant message content.
func (s *Session) LastAssistantContent() string {
	for i := len(s.messages) - 1; i >= 0; i-- {
		if s.messages[i].Role == "assistant" {
			return s.messages[i].Content
		}
	}
	return ""
}

// TurnCount returns the number of user messages.
func (s *Session) TurnCount() int {
	count := 0
	for _, m := range s.messages {
		if m.Role == "user" {
			count++
		}
	}
	return count
}

// MessageCount returns total messages in history.
func (s *Session) MessageCount() int { return len(s.messages) }

// LoopConfig controls the ReAct loop behavior.
// Defined here so pipeline can pass it without importing agent.
type LoopConfig struct {
	MaxTurns      int
	IdleThreshold int
	LoopWindow    int
}

// DefaultLoopConfig returns sensible defaults.
func DefaultLoopConfig() LoopConfig {
	return LoopConfig{MaxTurns: 25, IdleThreshold: 3, LoopWindow: 3}
}

// ToolDef describes a tool for token budgeting in tool pruning.
type ToolDef struct {
	Name           string `json:"name"`
	Description    string `json:"description"`
	ParametersJSON string `json:"parameters_json"`
	RiskLevel      core.RiskLevel `json:"risk_level"`
}

// EstimateTokens returns a rough token count for this tool definition.
func (td ToolDef) EstimateTokens() int {
	// ~4 chars per token heuristic
	return (len(td.Name) + len(td.Description) + len(td.ParametersJSON)) / 4
}

// MemoryFragment represents a single retrieved memory chunk.
type MemoryFragment struct {
	Tier      core.MemoryTier
	Content   string
	Score     float64
	Timestamp int64
}
```

- [ ] **Step 2: Verify it compiles**

Run: `go build ./internal/pipeline/`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add internal/pipeline/types_pipeline.go
git commit -m "refactor(pipeline): add pipeline-owned Session, LoopConfig, ToolDef types

These types live in the pipeline package so the pipeline can be fully
self-contained without importing internal/agent."
```

### Task 3: Create pipeline mock test infrastructure

**Files:**
- Create: `internal/pipeline/mock_test.go`

- [ ] **Step 1: Write function-field mocks for all interfaces**

```go
package pipeline_test

import (
	"context"

	"roboticus/internal/core"
	"roboticus/internal/llm"
	"roboticus/internal/pipeline"
)

// mockInjectionChecker is a test double for pipeline.InjectionChecker.
type mockInjectionChecker struct {
	CheckInputFunc func(string) core.ThreatScore
	SanitizeFunc   func(string) string
}

func (m *mockInjectionChecker) CheckInput(text string) core.ThreatScore {
	if m.CheckInputFunc != nil {
		return m.CheckInputFunc(text)
	}
	return core.ThreatScore(0) // safe by default
}

func (m *mockInjectionChecker) Sanitize(text string) string {
	if m.SanitizeFunc != nil {
		return m.SanitizeFunc(text)
	}
	return text
}

// mockMemoryRetriever is a test double for pipeline.MemoryRetriever.
type mockMemoryRetriever struct {
	RetrieveFunc func(ctx context.Context, sessionID, query string, budget int) string
}

func (m *mockMemoryRetriever) Retrieve(ctx context.Context, sessionID, query string, budget int) string {
	if m.RetrieveFunc != nil {
		return m.RetrieveFunc(ctx, sessionID, query, budget)
	}
	return ""
}

// mockSkillMatcher is a test double for pipeline.SkillMatcher.
type mockSkillMatcher struct {
	TryMatchFunc func(ctx context.Context, session *pipeline.Session, content string) *pipeline.Outcome
}

func (m *mockSkillMatcher) TryMatch(ctx context.Context, session *pipeline.Session, content string) *pipeline.Outcome {
	if m.TryMatchFunc != nil {
		return m.TryMatchFunc(ctx, session, content)
	}
	return nil
}

// mockToolExecutor is a test double for pipeline.ToolExecutor.
type mockToolExecutor struct {
	RunLoopFunc func(ctx context.Context, session *pipeline.Session) (string, int, error)
}

func (m *mockToolExecutor) RunLoop(ctx context.Context, session *pipeline.Session) (string, int, error) {
	if m.RunLoopFunc != nil {
		return m.RunLoopFunc(ctx, session)
	}
	return "mock response", 1, nil
}

// mockIngestor is a test double for pipeline.Ingestor.
type mockIngestor struct {
	IngestTurnFunc func(ctx context.Context, session *pipeline.Session)
}

func (m *mockIngestor) IngestTurn(ctx context.Context, session *pipeline.Session) {
	if m.IngestTurnFunc != nil {
		m.IngestTurnFunc(ctx, session)
	}
}

// mockNicknameRefiner is a test double for pipeline.NicknameRefiner.
type mockNicknameRefiner struct {
	RefineFunc func(ctx context.Context, session *pipeline.Session)
}

func (m *mockNicknameRefiner) Refine(ctx context.Context, session *pipeline.Session) {
	if m.RefineFunc != nil {
		m.RefineFunc(ctx, session)
	}
}

// mockStreamPreparer is a test double for pipeline.StreamPreparer.
type mockStreamPreparer struct {
	PrepareStreamFunc func(ctx context.Context, session *pipeline.Session) (*llm.Request, error)
}

func (m *mockStreamPreparer) PrepareStream(ctx context.Context, session *pipeline.Session) (*llm.Request, error) {
	if m.PrepareStreamFunc != nil {
		return m.PrepareStreamFunc(ctx, session)
	}
	return &llm.Request{Stream: true}, nil
}
```

- [ ] **Step 2: Verify it compiles**

Run: `go build ./internal/pipeline/`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add internal/pipeline/mock_test.go
git commit -m "test(pipeline): add function-field mocks for all pipeline interfaces

Each mock has safe defaults. No mock framework dependency."
```

### Task 4: Refactor Pipeline struct to use interfaces

**Files:**
- Modify: `internal/pipeline/pipeline.go` (lines 1-110)

This is the critical refactor. Replace all `*agent.X` concrete types with the interfaces from Task 1.

- [ ] **Step 1: Update Pipeline struct and PipelineDeps**

Replace the entire import block, Pipeline struct, PipelineDeps struct, and New function in `internal/pipeline/pipeline.go` (lines 1-105):

```go
package pipeline

import (
	"context"
	"fmt"

	"github.com/rs/zerolog/log"

	"roboticus/internal/core"
	"roboticus/internal/db"
	"roboticus/internal/llm"
)

// Outcome represents the result of a pipeline run.
type Outcome struct {
	SessionID  string `json:"session_id"`
	MessageID  string `json:"message_id"`
	Content    string `json:"content"`
	Model      string `json:"model,omitempty"`
	TokensIn   int    `json:"tokens_in,omitempty"`
	TokensOut  int    `json:"tokens_out,omitempty"`
	ReactTurns int    `json:"react_turns,omitempty"`
	FromCache  bool   `json:"from_cache,omitempty"`
	Stream     bool   `json:"stream,omitempty"`
}

// Input is the raw request to the pipeline.
type Input struct {
	Content   string
	SessionID string
	AgentID   string
	AgentName string
	Platform  string
	SenderID  string
	ChatID    string
	Claim     *ChannelClaimContext
}

// Runner is the interface for executing the pipeline.
type Runner interface {
	Run(ctx context.Context, cfg Config, input Input) (*Outcome, error)
}

var _ Runner = (*Pipeline)(nil)

// Pipeline is the unified factory. Connectors call Run() with a Config preset
// and an Input — the pipeline handles everything else.
type Pipeline struct {
	store     *db.Store
	llmSvc    *llm.Service
	injection InjectionChecker
	executor  ToolExecutor
	skills    SkillMatcher
	retriever MemoryRetriever
	ingestor  Ingestor
	refiner   NicknameRefiner
	streamer  StreamPreparer
	guards    *GuardChain
	bgWorker  *core.BackgroundWorker
}

// PipelineDeps bundles dependencies for the Pipeline.
type PipelineDeps struct {
	Store     *db.Store
	LLM       *llm.Service
	Injection InjectionChecker
	Executor  ToolExecutor
	Skills    SkillMatcher
	Retriever MemoryRetriever
	Ingestor  Ingestor
	Refiner   NicknameRefiner
	Streamer  StreamPreparer
	Guards    *GuardChain
	BGWorker  *core.BackgroundWorker
}

// New creates the unified pipeline.
func New(deps PipelineDeps) *Pipeline {
	bgw := deps.BGWorker
	if bgw == nil {
		bgw = core.NewBackgroundWorker(16)
	}
	return &Pipeline{
		store:     deps.Store,
		llmSvc:    deps.LLM,
		injection: deps.Injection,
		executor:  deps.Executor,
		skills:    deps.Skills,
		retriever: deps.Retriever,
		ingestor:  deps.Ingestor,
		refiner:   deps.Refiner,
		streamer:  deps.Streamer,
		guards:    deps.Guards,
		bgWorker:  bgw,
	}
}
```

Note: The `Run()` method, `RunPipeline()`, and `storeTrace()` functions remain unchanged except that field access uses the new names (`p.injection`, `p.executor`, etc.).

- [ ] **Step 2: Update Run() method field references**

In the `Run()` method (same file), update:
- `p.injection.CheckInput` and `p.injection.Sanitize` — already interface-compatible, no change needed
- `p.trySkillFirst` calls `p.skills.TryMatch` instead of iterating `p.skills` slice
- Skill-first section (lines 196-204) becomes:

```go
	// Stage 7: Skill-first fulfillment.
	tr.BeginSpan("skill_dispatch")
	if cfg.SkillFirstEnabled && authority == core.AuthorityCreator && p.skills != nil {
		if result := p.skills.TryMatch(ctx, session, content); result != nil {
			tr.Annotate("matched", true)
			tr.EndSpan("ok")
			p.storeTrace(ctx, tr, msgID, cfg.ChannelLabel)
			return result, nil
		}
	}
	tr.EndSpan("skipped")
```

- [ ] **Step 3: Verify the build fails (expected — pipeline_stages.go still uses agent)**

Run: `go build ./internal/pipeline/`
Expected: FAIL — `pipeline_stages.go` still imports and uses `agent.*` types

### Task 5: Refactor pipeline_stages.go to use interfaces

**Files:**
- Modify: `internal/pipeline/pipeline_stages.go`

- [ ] **Step 1: Replace pipeline_stages.go with interface-based implementation**

Rewrite `internal/pipeline/pipeline_stages.go` to remove the `agent` import. The key changes:
- `runStandardInference` calls `p.executor.RunLoop(ctx, session)` instead of constructing an `agent.Loop`
- `prepareStreamInference` calls `p.streamer.PrepareStream(ctx, session)` instead of building an `agent.ContextBuilder`
- `resolveSession` returns `*pipeline.Session` (not `*agent.Session`)
- `trySkillFirst` is removed (replaced by `p.skills.TryMatch` in Task 4)
- `refineNickname` calls `p.refiner.Refine(ctx, session)`
- Post-turn ingest calls `p.ingestor.IngestTurn(ctx, session)`
- Memory retrieval calls `p.retriever.Retrieve(ctx, sessionID, query, budget)`

The full replacement file:

```go
package pipeline

import (
	"context"
	"fmt"
	"strings"

	"github.com/rs/zerolog/log"

	"roboticus/internal/core"
	"roboticus/internal/db"
)

// runStandardInference delegates to the ToolExecutor for the full ReAct loop.
func (p *Pipeline) runStandardInference(ctx context.Context, cfg Config, session *Session, msgID string) (*Outcome, error) {
	content, turns, err := p.executor.RunLoop(ctx, session)
	if err != nil {
		return nil, core.WrapError(core.ErrLLM, "inference failed", err)
	}

	// Guard chain.
	if p.guards != nil && cfg.GuardSet != GuardSetNone {
		content = p.guards.Apply(content)
	}

	// Post-turn ingest (background).
	if cfg.PostTurnIngest && p.ingestor != nil {
		sess := session
		p.bgWorker.Submit("ingestTurn", func(bgCtx context.Context) {
			p.ingestor.IngestTurn(bgCtx, sess)
		})
	}

	// Store assistant response.
	assistantMsgID := db.NewID()
	_, storeErr := p.store.ExecContext(ctx,
		`INSERT INTO session_messages (id, session_id, role, content)
		 VALUES (?, ?, 'assistant', ?)`,
		assistantMsgID, session.ID, content,
	)
	if storeErr != nil {
		log.Error().Err(storeErr).Str("session", session.ID).Msg("failed to store assistant message")
	}

	// Nickname refinement (background).
	if cfg.NicknameRefinement && session.TurnCount() >= 4 && p.refiner != nil {
		sess := session
		p.bgWorker.Submit("refineNickname", func(bgCtx context.Context) {
			p.refiner.Refine(bgCtx, sess)
		})
	}

	return &Outcome{
		SessionID:  session.ID,
		MessageID:  msgID,
		Content:    content,
		ReactTurns: turns,
	}, nil
}

// prepareStreamInference sets up streaming inference.
func (p *Pipeline) prepareStreamInference(ctx context.Context, _ Config, session *Session, msgID string) (*Outcome, error) {
	if p.streamer != nil {
		if _, err := p.streamer.PrepareStream(ctx, session); err != nil {
			return nil, core.WrapError(core.ErrLLM, "stream preparation failed", err)
		}
	}

	return &Outcome{
		SessionID: session.ID,
		MessageID: msgID,
		Stream:    true,
	}, nil
}

// resolveSession finds or creates a session based on the resolution mode.
func (p *Pipeline) resolveSession(ctx context.Context, cfg Config, input Input) (*Session, error) {
	switch cfg.SessionResolution {
	case SessionFromBody:
		if input.SessionID != "" {
			return p.loadSession(ctx, input)
		}
		return p.createSession(ctx, input)
	case SessionFromChannel:
		scope := fmt.Sprintf("%s:%s", input.Platform, input.ChatID)
		row := p.store.QueryRowContext(ctx,
			`SELECT id FROM sessions WHERE agent_id = ? AND scope_key = ? AND status = 'active'
			 ORDER BY created_at DESC LIMIT 1`,
			input.AgentID, scope,
		)
		var sessionID string
		if err := row.Scan(&sessionID); err == nil {
			return p.loadSessionByID(ctx, sessionID, input)
		}
		return p.createSessionWithScope(ctx, input, scope)
	case SessionDedicated:
		return p.createSession(ctx, input)
	}
	return nil, core.NewError(core.ErrConfig, "unknown session resolution mode")
}

func (p *Pipeline) loadSession(ctx context.Context, input Input) (*Session, error) {
	sess := NewSession(input.SessionID, input.AgentID, input.AgentName)
	sess.Channel = input.Platform

	rows, err := p.store.QueryContext(ctx,
		`SELECT role, content FROM session_messages WHERE session_id = ? ORDER BY created_at ASC LIMIT 50`,
		input.SessionID,
	)
	if err != nil {
		log.Warn().Err(err).Str("session_id", input.SessionID).Msg("failed to load session history")
		return sess, nil
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var role, content string
		if err := rows.Scan(&role, &content); err != nil {
			continue
		}
		switch role {
		case "user":
			sess.AddUserMessage(content)
		case "assistant":
			sess.AddAssistantMessage(content, nil)
		case "system":
			sess.AddSystemMessage(content)
		}
	}
	return sess, nil
}

func (p *Pipeline) loadSessionByID(ctx context.Context, sessionID string, input Input) (*Session, error) {
	input.SessionID = sessionID
	return p.loadSession(ctx, input)
}

func (p *Pipeline) createSession(ctx context.Context, input Input) (*Session, error) {
	return p.createSessionWithScope(ctx, input, input.Platform)
}

func (p *Pipeline) createSessionWithScope(ctx context.Context, input Input, scopeKey string) (*Session, error) {
	id := db.NewID()
	_, err := p.store.ExecContext(ctx,
		`INSERT INTO sessions (id, agent_id, scope_key) VALUES (?, ?, ?)`,
		id, input.AgentID, scopeKey,
	)
	if err != nil {
		return nil, err
	}
	sess := NewSession(id, input.AgentID, input.AgentName)
	sess.Channel = input.Platform
	return sess, nil
}

// expandShortFollowup detects short reactions and prepends prior context.
func (p *Pipeline) expandShortFollowup(session *Session, content string) string {
	if len(content) < 20 && session.TurnCount() > 0 {
		prior := session.LastAssistantContent()
		if prior != "" {
			prefix := prior
			if len(prefix) > 200 {
				prefix = prefix[:200] + "..."
			}
			return fmt.Sprintf("[Regarding your previous response: %q]\n\n%s", prefix, content)
		}
	}
	return content
}

// tryShortcut checks for simple shortcuts that don't need full LLM inference.
func (p *Pipeline) tryShortcut(_ context.Context, session *Session, content string) *Outcome {
	lower := strings.TrimSpace(strings.ToLower(content))

	if lower == "who are you" || lower == "who are you?" || lower == "what are you?" {
		return &Outcome{
			SessionID: session.ID,
			Content:   fmt.Sprintf("I am %s, an autonomous AI agent.", session.AgentName),
		}
	}

	switch lower {
	case "ok", "okay", "thanks", "thank you", "got it", "understood", "k", "ty":
		return &Outcome{
			SessionID: session.ID,
			Content:   "Acknowledged. Let me know if you need anything else.",
		}
	}

	if lower == "help" || lower == "/help" {
		return &Outcome{
			SessionID: session.ID,
			Content: fmt.Sprintf("%s can help with:\n- General conversation and reasoning\n- File operations and code tasks\n- Web search and information retrieval\n- Scheduling and reminders\n- Financial operations\n\nJust describe what you need.", session.AgentName),
		}
	}

	return nil
}
```

Note: The `tryShortcut` method now uses `session.AgentName` instead of `p.promptCfg.AgentName` — the agent name is available on the session.

- [ ] **Step 2: Verify pipeline package builds**

Run: `go build ./internal/pipeline/`
Expected: PASS — no more `agent` import

- [ ] **Step 3: Verify agent import is gone**

Run: `grep -r '"roboticus/internal/agent"' internal/pipeline/`
Expected: No matches

### Task 6: Update daemon.go wiring (composition root)

**Files:**
- Modify: `internal/daemon/daemon.go` (lines 90-130)

The daemon must now create adapter types that satisfy the pipeline interfaces.

- [ ] **Step 1: Create adapter wrappers in daemon.go**

Add adapter types that bridge `agent.*` concrete types to `pipeline.*` interfaces. Add these above the `New` function:

```go
// agentInjectionAdapter adapts *agent.InjectionDetector to pipeline.InjectionChecker.
type agentInjectionAdapter struct{ det *agent.InjectionDetector }

func (a *agentInjectionAdapter) CheckInput(text string) core.ThreatScore { return a.det.CheckInput(text) }
func (a *agentInjectionAdapter) Sanitize(text string) string             { return a.det.Sanitize(text) }

// agentRetrieverAdapter adapts *agent.MemoryRetriever to pipeline.MemoryRetriever.
type agentRetrieverAdapter struct{ r *agent.MemoryRetriever }

func (a *agentRetrieverAdapter) Retrieve(ctx context.Context, sessionID, query string, budget int) string {
	return a.r.Retrieve(ctx, sessionID, query, budget)
}

// agentIngestorAdapter adapts *agent.MemoryManager to pipeline.Ingestor.
type agentIngestorAdapter struct{ m *agent.MemoryManager }

func (a *agentIngestorAdapter) IngestTurn(ctx context.Context, session *pipeline.Session) {
	// Convert pipeline.Session to agent.Session for the agent layer.
	agentSess := agent.NewSession(session.ID, session.AgentID, session.AgentName)
	agentSess.Authority = session.Authority
	agentSess.Channel = session.Channel
	for _, msg := range session.Messages() {
		switch msg.Role {
		case "user":
			agentSess.AddUserMessage(msg.Content)
		case "assistant":
			agentSess.AddAssistantMessage(msg.Content, msg.ToolCalls)
		case "system":
			agentSess.AddSystemMessage(msg.Content)
		}
	}
	a.m.IngestTurn(ctx, agentSess)
}

// agentExecutorAdapter adapts the agent loop to pipeline.ToolExecutor.
type agentExecutorAdapter struct {
	loopCfg   agent.LoopConfig
	llmSvc    *llm.Service
	tools     *agent.ToolRegistry
	policy    *agent.PolicyEngine
	injection *agent.InjectionDetector
	memory    *agent.MemoryManager
	retriever *agent.MemoryRetriever
	ctxCfg    agent.ContextConfig
	promptCfg agent.PromptConfig
}

func (a *agentExecutorAdapter) RunLoop(ctx context.Context, session *pipeline.Session) (string, int, error) {
	// Convert pipeline.Session to agent.Session.
	agentSess := agent.NewSession(session.ID, session.AgentID, session.AgentName)
	agentSess.Authority = session.Authority
	agentSess.Channel = session.Channel
	for _, msg := range session.Messages() {
		switch msg.Role {
		case "user":
			agentSess.AddUserMessage(msg.Content)
		case "assistant":
			agentSess.AddAssistantMessage(msg.Content, msg.ToolCalls)
		case "system":
			agentSess.AddSystemMessage(msg.Content)
		case "tool":
			agentSess.AddToolResult(msg.ToolCallID, msg.Name, msg.Content, false)
		}
	}

	ctxBuilder := agent.NewContextBuilder(a.ctxCfg)
	ctxBuilder.SetSystemPrompt(agent.BuildSystemPrompt(a.promptCfg))
	ctxBuilder.SetTools(a.tools.ToolDefs())

	if a.retriever != nil {
		memBlock := a.retriever.Retrieve(ctx, session.ID, session.LastAssistantContent(), a.ctxCfg.MaxTokens/4)
		if memBlock != "" {
			ctxBuilder.SetMemory(memBlock)
		}
	}

	deps := agent.LoopDeps{
		LLM:       a.llmSvc,
		Tools:     a.tools,
		Policy:    a.policy,
		Injection: a.injection,
		Memory:    a.memory,
		Context:   ctxBuilder,
	}
	loop := agent.NewLoop(a.loopCfg, deps)

	result, err := loop.Run(ctx, agentSess)
	if err != nil {
		return "", 0, err
	}

	// Sync messages back to pipeline session.
	for _, msg := range agentSess.Messages()[session.MessageCount():] {
		switch msg.Role {
		case "assistant":
			session.AddAssistantMessage(msg.Content, msg.ToolCalls)
		case "tool":
			session.AddToolResult(msg.ToolCallID, msg.Name, msg.Content, false)
		case "system":
			session.AddSystemMessage(msg.Content)
		}
	}

	return result, loop.TurnCount(), nil
}
```

- [ ] **Step 2: Update the pipeline construction in New()**

Replace the `pipeline.New(pipeline.PipelineDeps{...})` block (around line 119-129) with:

```go
	executor := &agentExecutorAdapter{
		loopCfg:   agent.DefaultLoopConfig(),
		llmSvc:    llmSvc,
		tools:     tools,
		policy:    policyEngine,
		injection: injection,
		memory:    memMgr,
		retriever: retriever,
		ctxCfg:    agent.ContextConfig{MaxTokens: 4096},
		promptCfg: agent.PromptConfig{AgentName: cfg.Agent.Name},
	}

	pipe := pipeline.New(pipeline.PipelineDeps{
		Store:     store,
		LLM:       llmSvc,
		Injection: &agentInjectionAdapter{det: injection},
		Executor:  executor,
		Skills:    nil, // skill matcher wired later when skills are loaded
		Retriever: &agentRetrieverAdapter{r: retriever},
		Ingestor:  &agentIngestorAdapter{m: memMgr},
		Guards:    guards,
		BGWorker:  bgWorker,
	})
```

- [ ] **Step 3: Build the full project**

Run: `go build ./...`
Expected: PASS

- [ ] **Step 4: Run all tests**

Run: `go test ./... 2>&1 | tail -20`
Expected: All tests PASS (zero behavioral change)

- [ ] **Step 5: Verify architecture test passes**

Run: `go test -v -run TestArchitecture ./internal/api/`
Expected: PASS

- [ ] **Step 6: Verify no agent import in pipeline**

Run: `grep -rn '"roboticus/internal/agent"' internal/pipeline/`
Expected: No output (no matches)

- [ ] **Step 7: Commit**

```bash
git add internal/pipeline/pipeline.go internal/pipeline/pipeline_stages.go internal/daemon/daemon.go
git commit -m "refactor(pipeline): remove agent import via interface extraction

Pipeline now depends on InjectionChecker, ToolExecutor, SkillMatcher,
MemoryRetriever, Ingestor, NicknameRefiner, and StreamPreparer interfaces.
Daemon.go is the composition root that wires concrete agent types.
Zero behavioral change — all existing tests pass."
```

---

## Phase 1: Pipeline Stage Completeness

### Task 7: Flight Recorder

**Files:**
- Create: `internal/pipeline/flight_recorder.go`
- Create: `internal/pipeline/flight_recorder_test.go`

- [ ] **Step 1: Write flight recorder tests**

```go
package pipeline

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestReactTrace_RecordStep(t *testing.T) {
	tests := []struct {
		name      string
		steps     []ReactStep
		wantCount int
		wantKinds []StepKind
	}{
		{
			name:      "empty trace",
			steps:     nil,
			wantCount: 0,
			wantKinds: nil,
		},
		{
			name: "single tool call",
			steps: []ReactStep{
				{Kind: StepToolCall, Name: "web_search", DurationMs: 150, Success: true, Source: ToolSource{Kind: "builtin"}},
			},
			wantCount: 1,
			wantKinds: []StepKind{StepToolCall},
		},
		{
			name: "mixed step types",
			steps: []ReactStep{
				{Kind: StepLLMCall, Name: "llm", DurationMs: 500, Success: true},
				{Kind: StepToolCall, Name: "file_read", DurationMs: 10, Success: true, Source: ToolSource{Kind: "builtin"}},
				{Kind: StepGuardCheck, Name: "empty_response", DurationMs: 1, Success: true},
				{Kind: StepRetry, Name: "repetition", DurationMs: 0, Success: false},
			},
			wantCount: 4,
			wantKinds: []StepKind{StepLLMCall, StepToolCall, StepGuardCheck, StepRetry},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rt := NewReactTrace()
			for _, s := range tt.steps {
				rt.RecordStep(s)
			}
			if len(rt.Steps) != tt.wantCount {
				t.Errorf("step count = %d, want %d", len(rt.Steps), tt.wantCount)
			}
			for i, wk := range tt.wantKinds {
				if rt.Steps[i].Kind != wk {
					t.Errorf("step[%d].Kind = %d, want %d", i, rt.Steps[i].Kind, wk)
				}
			}
		})
	}
}

func TestReactTrace_Truncation(t *testing.T) {
	rt := NewReactTrace()
	longInput := strings.Repeat("x", 1000)
	longOutput := strings.Repeat("y", 2000)
	rt.RecordStep(ReactStep{
		Kind:   StepToolCall,
		Name:   "test",
		Input:  longInput,
		Output: longOutput,
	})
	if len(rt.Steps[0].Input) > maxStepFieldLen+3 { // +3 for "..."
		t.Errorf("input not truncated: len=%d", len(rt.Steps[0].Input))
	}
	if len(rt.Steps[0].Output) > maxStepFieldLen+3 {
		t.Errorf("output not truncated: len=%d", len(rt.Steps[0].Output))
	}
}

func TestReactTrace_JSON(t *testing.T) {
	rt := NewReactTrace()
	rt.RecordStep(ReactStep{Kind: StepToolCall, Name: "echo", Success: true})
	rt.Finish()

	data, err := json.Marshal(rt)
	if err != nil {
		t.Fatal(err)
	}
	var decoded ReactTrace
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded.Steps) != 1 {
		t.Errorf("roundtrip: got %d steps, want 1", len(decoded.Steps))
	}
}

func TestReactTrace_Finish(t *testing.T) {
	rt := NewReactTrace()
	time.Sleep(5 * time.Millisecond)
	rt.Finish()
	if rt.TotalMs <= 0 {
		t.Error("TotalMs should be > 0 after Finish")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/pipeline/ -run TestReactTrace -v`
Expected: FAIL — types not defined yet

- [ ] **Step 3: Write the implementation**

```go
package pipeline

import "time"

const maxStepFieldLen = 500

// StepKind categorizes a ReactTrace step.
type StepKind int

const (
	StepToolCall   StepKind = iota // Tool execution
	StepLLMCall                    // LLM inference call
	StepGuardCheck                 // Guard evaluation
	StepRetry                      // Guard-triggered retry
)

// ToolSource identifies where a tool came from.
type ToolSource struct {
	Kind   string `json:"kind"`   // "builtin", "mcp", "plugin", "skill"
	Server string `json:"server"` // MCP server or plugin name (empty for builtin)
}

// ReactStep records a single step in the ReAct loop.
type ReactStep struct {
	Kind       StepKind   `json:"kind"`
	Name       string     `json:"name"`
	DurationMs int64      `json:"duration_ms"`
	Success    bool       `json:"success"`
	Source     ToolSource `json:"source"`
	Input      string     `json:"input"`
	Output     string     `json:"output"`
}

// ReactTrace records all steps of a ReAct tool-calling loop.
type ReactTrace struct {
	Steps     []ReactStep `json:"steps"`
	StartedAt time.Time   `json:"started_at"`
	TotalMs   int64       `json:"total_ms"`
}

// NewReactTrace starts a new trace.
func NewReactTrace() *ReactTrace {
	return &ReactTrace{StartedAt: time.Now()}
}

// RecordStep adds a step, truncating Input/Output to maxStepFieldLen.
func (rt *ReactTrace) RecordStep(s ReactStep) {
	s.Input = truncate(s.Input, maxStepFieldLen)
	s.Output = truncate(s.Output, maxStepFieldLen)
	rt.Steps = append(rt.Steps, s)
}

// Finish marks the trace complete and records total duration.
func (rt *ReactTrace) Finish() {
	rt.TotalMs = time.Since(rt.StartedAt).Milliseconds()
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/pipeline/ -run TestReactTrace -v`
Expected: All PASS

- [ ] **Step 5: Commit**

```bash
git add internal/pipeline/flight_recorder.go internal/pipeline/flight_recorder_test.go
git commit -m "feat(pipeline): add flight recorder for ReAct loop observability

Records tool calls, LLM calls, guard checks, and retries with timing,
source attribution, and truncated I/O. JSON-serializable for persistence."
```

### Task 8: Trace Repository

**Files:**
- Create: `internal/db/migrations/016_react_traces.sql`
- Create: `internal/db/traces_repo.go`
- Create: `internal/db/traces_repo_test.go`

- [ ] **Step 1: Write the migration**

```sql
-- 016: React trace storage for flight recorder data.
-- Add session_id to pipeline_traces (existing table lacks it).
ALTER TABLE pipeline_traces ADD COLUMN session_id TEXT NOT NULL DEFAULT '';

CREATE TABLE IF NOT EXISTS react_traces (
    id TEXT PRIMARY KEY,
    pipeline_trace_id TEXT NOT NULL REFERENCES pipeline_traces(id),
    react_json TEXT NOT NULL DEFAULT '{}',
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX IF NOT EXISTS idx_react_traces_pipeline ON react_traces(pipeline_trace_id);
CREATE INDEX IF NOT EXISTS idx_pipeline_traces_session ON pipeline_traces(session_id);
```

- [ ] **Step 2: Write trace repository tests**

```go
package db

import (
	"context"
	"testing"
)

func TestTraceRepository_SaveAndLoad(t *testing.T) {
	store := testTempStore(t)
	repo := NewTraceRepository(store)
	ctx := context.Background()

	row := PipelineTraceRow{
		ID:        "trace-1",
		TurnID:    "turn-1",
		SessionID: "sess-1",
		Channel:   "api",
		TotalMs:   150,
		StagesJSON: `[{"name":"validation","duration_ms":5}]`,
	}

	if err := repo.SavePipelineTrace(ctx, row); err != nil {
		t.Fatalf("SavePipelineTrace: %v", err)
	}

	if err := repo.SaveReactTrace(ctx, "react-1", "trace-1", `{"steps":[]}`); err != nil {
		t.Fatalf("SaveReactTrace: %v", err)
	}

	got, err := repo.GetByTurnID(ctx, "turn-1")
	if err != nil {
		t.Fatalf("GetByTurnID: %v", err)
	}
	if got.Channel != "api" {
		t.Errorf("Channel = %q, want %q", got.Channel, "api")
	}
	if got.TotalMs != 150 {
		t.Errorf("TotalMs = %d, want 150", got.TotalMs)
	}
}

func TestTraceRepository_ListTraces(t *testing.T) {
	store := testTempStore(t)
	repo := NewTraceRepository(store)
	ctx := context.Background()

	for i, ch := range []string{"api", "telegram", "api"} {
		row := PipelineTraceRow{
			ID:        NewID(),
			TurnID:    NewID(),
			SessionID: "sess-1",
			Channel:   ch,
			TotalMs:   int64(i * 100),
			StagesJSON: "[]",
		}
		if err := repo.SavePipelineTrace(ctx, row); err != nil {
			t.Fatal(err)
		}
	}

	all, err := repo.ListTraces(ctx, TraceFilter{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 3 {
		t.Errorf("got %d traces, want 3", len(all))
	}

	filtered, err := repo.ListTraces(ctx, TraceFilter{Channel: "api", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(filtered) != 2 {
		t.Errorf("got %d api traces, want 2", len(filtered))
	}
}

// testTempStore creates an in-memory store for testing.
// Uses the same pattern as testutil.TempStore but avoids the import cycle.
func testTempStore(t *testing.T) *Store {
	t.Helper()
	store, err := Open(":memory:")
	if err != nil {
		t.Fatalf("testTempStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `go test ./internal/db/ -run TestTraceRepository -v`
Expected: FAIL — types not defined

- [ ] **Step 4: Write the trace repository implementation**

```go
package db

import (
	"context"
	"database/sql"
)

// PipelineTraceRow represents a stored pipeline trace.
type PipelineTraceRow struct {
	ID         string
	TurnID     string
	SessionID  string
	Channel    string
	TotalMs    int64
	StagesJSON string
	CreatedAt  string
}

// TraceFilter controls which traces to list.
type TraceFilter struct {
	Channel   string
	SessionID string
	Limit     int
}

// TraceRepository handles pipeline and react trace persistence.
// All queries go through the Querier interface (the centralized connection pool).
type TraceRepository struct {
	q Querier
}

// NewTraceRepository creates a trace repository.
func NewTraceRepository(q Querier) *TraceRepository {
	return &TraceRepository{q: q}
}

// SavePipelineTrace inserts a pipeline trace row.
func (r *TraceRepository) SavePipelineTrace(ctx context.Context, row PipelineTraceRow) error {
	_, err := r.q.ExecContext(ctx,
		`INSERT INTO pipeline_traces (id, turn_id, session_id, channel, total_ms, stages_json)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		row.ID, row.TurnID, row.SessionID, row.Channel, row.TotalMs, row.StagesJSON,
	)
	return err
}

// SaveReactTrace inserts a react trace linked to a pipeline trace.
func (r *TraceRepository) SaveReactTrace(ctx context.Context, id, pipelineTraceID, reactJSON string) error {
	_, err := r.q.ExecContext(ctx,
		`INSERT INTO react_traces (id, pipeline_trace_id, react_json)
		 VALUES (?, ?, ?)`,
		id, pipelineTraceID, reactJSON,
	)
	return err
}

// GetByTurnID retrieves a pipeline trace by turn ID.
func (r *TraceRepository) GetByTurnID(ctx context.Context, turnID string) (*PipelineTraceRow, error) {
	row := r.q.QueryRowContext(ctx,
		`SELECT id, turn_id, session_id, channel, total_ms, stages_json, created_at
		 FROM pipeline_traces WHERE turn_id = ?`,
		turnID,
	)
	var tr PipelineTraceRow
	err := row.Scan(&tr.ID, &tr.TurnID, &tr.SessionID, &tr.Channel, &tr.TotalMs, &tr.StagesJSON, &tr.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return &tr, err
}

// ListTraces returns traces matching the filter.
func (r *TraceRepository) ListTraces(ctx context.Context, filter TraceFilter) ([]PipelineTraceRow, error) {
	query := `SELECT id, turn_id, session_id, channel, total_ms, stages_json, created_at FROM pipeline_traces`
	var args []any
	var conditions []string

	if filter.Channel != "" {
		conditions = append(conditions, "channel = ?")
		args = append(args, filter.Channel)
	}
	if filter.SessionID != "" {
		conditions = append(conditions, "session_id = ?")
		args = append(args, filter.SessionID)
	}
	if len(conditions) > 0 {
		query += " WHERE " + joinConditions(conditions)
	}
	query += " ORDER BY created_at DESC"
	if filter.Limit > 0 {
		query += " LIMIT ?"
		args = append(args, filter.Limit)
	}

	rows, err := r.q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var result []PipelineTraceRow
	for rows.Next() {
		var tr PipelineTraceRow
		if err := rows.Scan(&tr.ID, &tr.TurnID, &tr.SessionID, &tr.Channel, &tr.TotalMs, &tr.StagesJSON, &tr.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, tr)
	}
	return result, rows.Err()
}

func joinConditions(conds []string) string {
	result := conds[0]
	for _, c := range conds[1:] {
		result += " AND " + c
	}
	return result
}
```

- [ ] **Step 5: Run tests**

Run: `go test ./internal/db/ -run TestTraceRepository -v`
Expected: PASS (may need migration 016 to be applied — verify the `Open` function runs all migrations)

- [ ] **Step 6: Commit**

```bash
git add internal/db/migrations/016_react_traces.sql internal/db/traces_repo.go internal/db/traces_repo_test.go
git commit -m "feat(db): add trace repository for pipeline and react traces

Replaces inline SQL in pipeline.go. All queries go through Querier
interface (centralized connection pool). Includes migration 016."
```

### Task 9: Guard Registry

**Files:**
- Create: `internal/pipeline/guard_registry.go`
- Create: `internal/pipeline/guard_registry_test.go`

- [ ] **Step 1: Write guard registry tests**

```go
package pipeline

import "testing"

func TestGuardRegistry_Chain(t *testing.T) {
	tests := []struct {
		name       string
		preset     GuardSetPreset
		wantCount  int
	}{
		{"full set", GuardSetFull, 17},
		{"stream set", GuardSetStream, 5},
		{"cached set", GuardSetCached, 17},
		{"none set", GuardSetNone, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reg := NewDefaultGuardRegistry()
			chain := reg.Chain(tt.preset)
			if chain.Len() != tt.wantCount {
				t.Errorf("Chain(%v).Len() = %d, want %d", tt.preset, chain.Len(), tt.wantCount)
			}
		})
	}
}

func TestGuardRegistry_Get(t *testing.T) {
	reg := NewDefaultGuardRegistry()

	g, ok := reg.Get("empty_response")
	if !ok {
		t.Fatal("expected to find empty_response guard")
	}
	if g.Name() != "empty_response" {
		t.Errorf("Name() = %q, want %q", g.Name(), "empty_response")
	}

	_, ok = reg.Get("nonexistent")
	if ok {
		t.Error("expected nonexistent guard to not be found")
	}
}

func TestGuardRegistry_Register(t *testing.T) {
	reg := NewGuardRegistry()
	reg.Register(&EmptyResponseGuard{})

	if _, ok := reg.Get("empty_response"); !ok {
		t.Error("registered guard not found")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/pipeline/ -run TestGuardRegistry -v`
Expected: FAIL

- [ ] **Step 3: Write the implementation**

```go
package pipeline

// GuardRegistry manages named guards and materializes guard chains from presets.
type GuardRegistry struct {
	guards map[string]Guard
}

// NewGuardRegistry creates an empty guard registry.
func NewGuardRegistry() *GuardRegistry {
	return &GuardRegistry{guards: make(map[string]Guard)}
}

// NewDefaultGuardRegistry creates a registry with all built-in guards registered.
func NewDefaultGuardRegistry() *GuardRegistry {
	r := NewGuardRegistry()
	// Core guards.
	r.Register(&EmptyResponseGuard{})
	r.Register(NewContentClassificationGuard())
	r.Register(NewRepetitionGuard())
	r.Register(NewSystemPromptLeakGuard())
	r.Register(NewInternalMarkerGuard())
	// Behavioral guards.
	r.Register(&SubagentClaimGuard{})
	r.Register(&TaskDeferralGuard{})
	r.Register(&InternalJargonGuard{})
	r.Register(&DeclaredActionGuard{})
	// Quality guards.
	r.Register(&LowValueParrotingGuard{})
	r.Register(&NonRepetitionGuardV2{})
	r.Register(&OutputContractGuard{})
	r.Register(&UserEchoGuard{})
	// Truthfulness guards.
	r.Register(&ModelIdentityTruthGuard{})
	r.Register(&CurrentEventsTruthGuard{})
	r.Register(&ExecutionTruthGuard{})
	r.Register(&PersonalityIntegrityGuard{})
	return r
}

// Register adds a guard to the registry.
func (r *GuardRegistry) Register(g Guard) {
	r.guards[g.Name()] = g
}

// Get returns a guard by name.
func (r *GuardRegistry) Get(name string) (Guard, bool) {
	g, ok := r.guards[name]
	return g, ok
}

// Chain materializes a guard chain for the given preset.
func (r *GuardRegistry) Chain(preset GuardSetPreset) *GuardChain {
	switch preset {
	case GuardSetFull, GuardSetCached:
		return r.chainFromNames(
			"empty_response", "content_classification", "repetition",
			"system_prompt_leak", "internal_marker",
			"subagent_claim", "task_deferral", "internal_jargon", "declared_action",
			"low_value_parroting", "non_repetition_v2", "output_contract", "user_echo",
			"model_identity_truth", "current_events_truth", "execution_truth", "personality_integrity",
		)
	case GuardSetStream:
		return r.chainFromNames(
			"empty_response", "subagent_claim", "internal_jargon",
			"personality_integrity", "non_repetition_v2",
		)
	case GuardSetNone:
		return NewGuardChain()
	}
	return NewGuardChain()
}

func (r *GuardRegistry) chainFromNames(names ...string) *GuardChain {
	var guards []Guard
	for _, name := range names {
		if g, ok := r.guards[name]; ok {
			guards = append(guards, g)
		}
	}
	return NewGuardChain(guards...)
}
```

Also add a `Len()` method to `GuardChain` in `guards.go`:

```go
// Len returns the number of guards in the chain.
func (gc *GuardChain) Len() int { return len(gc.guards) }
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/pipeline/ -run TestGuardRegistry -v`
Expected: All PASS

- [ ] **Step 5: Commit**

```bash
git add internal/pipeline/guard_registry.go internal/pipeline/guard_registry_test.go internal/pipeline/guards.go
git commit -m "feat(pipeline): add guard registry for named guard sets

Materializes Full/Cached/Stream/None guard chains from registered guards.
Replaces free functions FullGuardChain()/StreamGuardChain() with a
configurable registry."
```

### Task 10: Guard Retry

**Files:**
- Create: `internal/pipeline/guard_retry.go`
- Create: `internal/pipeline/guard_retry_test.go`

- [ ] **Step 1: Write guard retry tests**

```go
package pipeline

import (
	"context"
	"errors"
	"testing"
)

func TestRetryPolicy_Defaults(t *testing.T) {
	p := DefaultRetryPolicy()
	if p.MaxRetries != 2 {
		t.Errorf("MaxRetries = %d, want 2", p.MaxRetries)
	}
	if !p.InjectReason {
		t.Error("InjectReason should default to true")
	}
	if !p.PreserveChain {
		t.Error("PreserveChain should default to true")
	}
}

type countingExecutor struct {
	calls    int
	results  []string
	failErr  error
}

func (e *countingExecutor) RunLoop(_ context.Context, _ *Session) (string, int, error) {
	idx := e.calls
	e.calls++
	if e.failErr != nil && idx == 0 {
		return "", 0, e.failErr
	}
	if idx < len(e.results) {
		return e.results[idx], 1, nil
	}
	return "default response", 1, nil
}

func TestGuardRetry_PassFirstTime(t *testing.T) {
	executor := &countingExecutor{results: []string{"good response"}}
	chain := NewGuardChain(&EmptyResponseGuard{})

	content, turns, err := retryWithGuards(context.Background(), executor, &Session{}, chain, DefaultRetryPolicy())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if content != "good response" {
		t.Errorf("content = %q, want %q", content, "good response")
	}
	if turns != 1 {
		t.Errorf("turns = %d, want 1", turns)
	}
	if executor.calls != 1 {
		t.Errorf("executor called %d times, want 1", executor.calls)
	}
}

func TestGuardRetry_FailThenPass(t *testing.T) {
	executor := &countingExecutor{results: []string{"", "good on retry"}}
	chain := NewGuardChain(&EmptyResponseGuard{})

	content, _, err := retryWithGuards(context.Background(), executor, &Session{}, chain, DefaultRetryPolicy())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if content != "good on retry" {
		t.Errorf("content = %q, want %q", content, "good on retry")
	}
	if executor.calls != 2 {
		t.Errorf("executor called %d times, want 2", executor.calls)
	}
}

func TestGuardRetry_ExhaustRetries(t *testing.T) {
	executor := &countingExecutor{results: []string{"", "", ""}}
	chain := NewGuardChain(&EmptyResponseGuard{})

	_, _, err := retryWithGuards(context.Background(), executor, &Session{}, chain, DefaultRetryPolicy())
	if err == nil {
		t.Fatal("expected error when retries exhausted")
	}
	// 1 initial + 2 retries = 3 calls
	if executor.calls != 3 {
		t.Errorf("executor called %d times, want 3", executor.calls)
	}
}

func TestGuardRetry_ExecutorError(t *testing.T) {
	executor := &countingExecutor{failErr: errors.New("llm down")}
	chain := NewGuardChain()

	_, _, err := retryWithGuards(context.Background(), executor, &Session{}, chain, DefaultRetryPolicy())
	if err == nil {
		t.Fatal("expected executor error to propagate")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/pipeline/ -run TestGuardRetry -v`
Expected: FAIL

- [ ] **Step 3: Write the implementation**

```go
package pipeline

import (
	"context"
	"fmt"

	"roboticus/internal/core"
)

// RetryPolicy controls guard-triggered re-inference behavior.
type RetryPolicy struct {
	MaxRetries    int  // Maximum retry attempts (default 2)
	InjectReason  bool // Append guard rejection reason to next prompt
	PreserveChain bool // Carry forward rejected response as context
}

// DefaultRetryPolicy returns the standard retry policy.
func DefaultRetryPolicy() RetryPolicy {
	return RetryPolicy{
		MaxRetries:    2,
		InjectReason:  true,
		PreserveChain: true,
	}
}

// retryWithGuards runs inference through the executor and guard chain,
// retrying on guard rejection up to MaxRetries times.
// Returns the final content, total turns, and any error.
func retryWithGuards(
	ctx context.Context,
	executor ToolExecutor,
	session *Session,
	guards *GuardChain,
	policy RetryPolicy,
) (string, int, error) {
	totalTurns := 0
	var lastReason string

	for attempt := 0; attempt <= policy.MaxRetries; attempt++ {
		// Inject retry context if this is a retry.
		if attempt > 0 && policy.InjectReason && lastReason != "" {
			session.AddSystemMessage(fmt.Sprintf(
				"Your previous response was rejected by the %s guard: %s. Please revise your response.",
				lastReason, lastReason,
			))
		}

		content, turns, err := executor.RunLoop(ctx, session)
		if err != nil {
			return "", totalTurns, err
		}
		totalTurns += turns

		// Apply guard chain.
		if guards == nil || guards.Len() == 0 {
			return content, totalTurns, nil
		}

		result := guards.ApplyFull(content)
		if !result.RetryRequested {
			return result.Content, totalTurns, nil
		}

		lastReason = result.RetryReason
	}

	return "", totalTurns, core.NewError(core.ErrGuardExhausted, fmt.Sprintf(
		"guard retries exhausted after %d attempts: %s", policy.MaxRetries+1, lastReason,
	))
}
```

Also add `ErrGuardExhausted` to `internal/core/errors.go` if it doesn't exist:

```go
var ErrGuardExhausted = errors.New("guard retries exhausted")
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/pipeline/ -run TestGuardRetry -v`
Expected: All PASS

- [ ] **Step 5: Commit**

```bash
git add internal/pipeline/guard_retry.go internal/pipeline/guard_retry_test.go internal/core/errors.go
git commit -m "feat(pipeline): add guard retry with context-preserving re-inference

When a guard rejects with Retry:true, re-runs inference with the rejection
reason injected. Preserves the reasoning chain per ARCHITECTURE.md principle 4."
```

---

*Remaining Phase 1 tasks (guard fallback, tool prune, intent registry, bot commands) and all Phase 2 tasks follow the same TDD pattern. Due to plan size, these are documented in the companion plan file `2026-04-02-parity-phases-1b-2.md`.*

### Task 11: Verify Phase 0-1a complete

- [ ] **Step 1: Full build**

Run: `go build ./...`
Expected: PASS

- [ ] **Step 2: Full test suite**

Run: `go test ./... 2>&1 | tail -5`
Expected: All PASS

- [ ] **Step 3: Architecture test**

Run: `go test -v -run TestArchitecture ./internal/api/`
Expected: PASS

- [ ] **Step 4: Verify no agent import in pipeline**

Run: `grep -rn '"roboticus/internal/agent"' internal/pipeline/`
Expected: No output

- [ ] **Step 5: Check coverage on new files**

Run: `go test ./internal/pipeline/ -coverprofile=cover.out && go tool cover -func=cover.out | grep -E '(flight_recorder|guard_re|guard_registry|traces_repo)'`
Expected: 80%+ on each file

---

*Phase 2 tasks (subagents, action planner, task state, retrieval strategy, compaction, capability discovery, tool output filter, governor) will be planned in a companion document after Phase 0-1 is implemented, since their exact interfaces will be informed by the concrete pipeline changes.*
