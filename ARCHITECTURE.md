# Roboticus — Architectural Principles

This document defines the non-negotiable architectural principles for the
Roboticus codebase. Every contributor — human or AI — must follow these
principles. Deviations are bugs and must be corrected before merge.

The shared product and architecture vocabulary lives in
[`docs/ubiquitous-language.md`](docs/ubiquitous-language.md). New docs, logs,
CLI help, UI labels, and comments should use those terms unless a legacy API or
symbol name is being preserved for compatibility.

## 0. No canned user- or operator-facing prose

Roboticus **must not** append, substitute, or inject fixed template text into
agent answers, guard-chain output, tool surfaces shown to operators, or HTTP/API
bodies **unless** there is literally no alternative (for example, a response
shape mandated verbatim by an external protocol).

Explanations **must** be grounded in model-generated content, tool results,
structured errors, logs, or telemetry—not boilerplate blocks appended after
inference. This is permanent product policy; see also `AGENTS.md` /
`CLAUDE.md` (**Product rules**).

---

## 1. Connector-Factory Pattern

The system is divided into **connectors** and **factories**.

### Factories (business logic)

A factory is a self-contained unit that transforms input into output through
a deterministic pipeline. The factory has **no knowledge** of where its input
came from or where its output will go.

The primary factory is the **Unified Pipeline** (`internal/pipeline/pipeline.go`).
It owns the entire request lifecycle:

```
Input → Injection Defense → Session Resolution → Turn Creation →
Message Storage → Decomposition Gate → Delegation Execution →
Shortcut Dispatch → Cache Check → Inference (Standard or Streaming) →
Guard Chain → Post-Turn Ingest → Nickname Refinement → Output
```

**Every stage** is controlled by `PipelineConfig` flags. No stage is
implemented outside the pipeline. No stage is duplicated.

### Connectors (I/O adapters)

A connector translates between an external protocol and the factory interface.
Connectors do exactly three things:

1. **Parse** — Extract `Content`, channel context, and authentication metadata
   from the channel-specific request format.
2. **Call** — Invoke `pipeline.RunPipeline()` with a `PipelineConfig` preset and a
   `ChannelContext` struct.
3. **Format** — Transform the `PipelineOutcome` into the channel-specific
   response format (JSON, SSE stream, Telegram message, etc.).

Connectors contain **zero business logic**. If you find yourself writing an
`if` statement in a connector that isn't about parsing or formatting, the
logic belongs in the factory.

### API vs dashboard webchannels

`/api/**` routes are externally addressable control and data surfaces. Even
when they are served from the same daemon as the dashboard, they must be treated
as independently permission-controlled interfaces for remote management,
automation, debugging, bootstrap, and integration clients.

Dashboard webchannels are not API aliases. They are dashboard-internal delivery
channels for state snapshots and UI event streams. The dashboard may consume
shared producers through websocket/webchannel topics, but it must not drive its
normal interface state by direct API polling or by reconstructing state from
API route-specific payloads. If the dashboard and an API route expose the same
domain truth, both must call the same producer/composition seam; neither surface
owns separate business logic.

### How to verify compliance

Ask: "If I deleted this connector and wrote a new one for a hypothetical
channel, would the new channel get all the same behavior?" If the answer is
no, business logic has leaked into the connector.

### Off-pipeline exemptions

The approved off-pipeline API flows are:

**Configuration interview** — `/api/interview`:

- `/api/interview/start`
- `/api/interview/turn`
- `/api/interview/finish`

These routes implement a bounded configuration interview workflow rather than
agent inference. They may maintain local interview state and format interview
responses directly, but they must not become a second agent runtime. If the
interview flow ever needs LLM inference, memory, delegation, or tool execution,
that behavior must move behind `pipeline.RunPipeline()`.

**Diagnostic analysis** — post-hoc observability endpoints that call
`llm.Service.Complete()` directly for remediation analysis of historical
turns/sessions. These are NOT agent-turn behavior — they analyze completed data,
not execute turns. They still honor pipeline-aligned heuristics via
`pipeline.ContextAnalyzer`:

- `AnalyzeSession` (`sessions.go`) — LLM-enhanced session analysis
- `AnalyzeTurn` (`turn_detail.go`) — LLM-enhanced turn analysis

These endpoints MUST NOT evolve into agent-turn execution paths. If they ever
need tool execution, memory retrieval, or delegation, that behavior must move
behind `pipeline.RunPipeline()`.

---

## 2. DRY at the Architectural Level

Code duplication within a function is a style issue. Code duplication across
system boundaries is an **architectural defect**.

Rules:

- If the same logic appears in two or more connectors, it **must** be moved
  into the factory.
- If a pipeline stage is conditional, gate it with a `PipelineConfig` flag —
  do not implement it in some connectors and skip it in others.
- If a new feature only works on some channels, that is a bug — not a feature.
  The pipeline must handle it uniformly; the connector just decides whether
  to enable it via config.

---

## 3. Concurrency: Or-Done Pattern

All goroutines must respect context cancellation via the **or-done pattern**.

- Every long-running goroutine takes `ctx context.Context` or a `done <-chan struct{}`.
- Channel reads are wrapped with `core.OrDone(done, ch)` to prevent leaks.
- The daemon creates a root context cancelled on SIGTERM; all subsystem
  goroutines derive from this context.
- Graceful shutdown proceeds in reverse startup order.

```go
// Correct: goroutine exits when done closes
for msg := range core.OrDone(ctx.Done(), inboundCh) {
    process(msg)
}

// Wrong: blocks forever if inboundCh never closes
for msg := range inboundCh {
    process(msg)
}
```

---

## 4. Loose Coupling

### Between packages

Packages communicate through interfaces and data structs, never concrete
implementations. A package should be replaceable without modifying its
dependents.

### Between pipeline stages

Each pipeline stage receives its input from the previous stage's output and
the shared `PipelineContext`. Stages do not call each other directly and do
not share mutable state except through the context.

### Between channels and sessions

Sessions are scoped by `(agent_id, scope)`. A session belongs to exactly one
channel scope. **Cross-channel session continuity requires explicit user
consent** — the user must authorize session sharing from the originating
channel before it can be accessed from another. This is a security control,
not a convenience feature, and must never be bypassed or made implicit.

---

## 5. Dependency DAG

Primary import flow (see **Supplementary: Go package import graph** in `docs/diagrams.md` — that graph is not a C4 diagram; C4 Code views there use a UML-style class sketch):

```
cmd/ ──> daemon/ ──> api/ ──> pipeline/ ──> agent/ ──> llm/ ──> core/
                       │          │            │          │        ^
                       │          │            │          └────────┘
                       │          │            ├──> db/ ──> core/
                       │          │            │
                       │          │            ├──> session/ ──> llm/ , core/
                       │          │            │
                       │          │            └──> channel/ ──> core/
                       │          │
                       │          ├──> schedule/ ──> db/
                       │          │
                       │          ├──> mcp/ ──> core/
                       │          │
                       │          └──> plugin/ ──> core/
                       │
                       └──> browser/ ──> core/

daemon/ ──> agent/ , channel/ , schedule/ , session/ , mcp/   (composition root wires subsystems)
```

_Note: `internal/wallet/` is a standalone library (on-chain helpers); it is not yet imported by daemon or api. HTTP `/api/wallet/*` routes read wallet-related fields via `db/` today._

**Rule: No circular imports.** `core` has zero internal deps. `db` depends
only on `core`. Everything else forms a DAG.

---

## 6. Error Handling

- All errors use `core.GobError` with a category sentinel (`core.ErrDatabase`,
  `core.ErrLLM`, etc.) for programmatic matching via `errors.Is()`.
- Wrap errors with context: `core.WrapError(core.ErrDatabase, "failed to ...", err)`.
- Never swallow errors silently. Log at minimum warn level if an error is
  intentionally not propagated.
- Return errors rather than panicking. Reserve panic for truly unrecoverable
  programmer errors (e.g., nil interface assertion in init).

---

## 7. Feature Parity Across Channels

Every channel must have access to the same capabilities. The `PipelineConfig`
presets control which stages are active, but the **default** should be full
feature parity. Disabling a stage for a channel requires a documented
rationale in the preset constructor's doc comment.

---

## 8. No Symptom Fixes

When a feature works on one channel but fails on another:

1. **Trace the working path** end-to-end.
2. **Trace the broken path** end-to-end.
3. **The divergence is the bug.** Fix the divergence.

Never strip capabilities from the working path to "fix" the broken one.
Never copy-paste the working path's logic into the broken handler.
