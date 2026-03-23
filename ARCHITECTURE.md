# Goboticus — Architectural Principles

This document defines the non-negotiable architectural principles for the
Goboticus codebase. Every contributor — human or AI — must follow these
principles. Deviations are bugs and must be corrected before merge.

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
2. **Call** — Invoke `pipeline.Run()` with a `PipelineConfig` preset and a
   `ChannelContext` struct.
3. **Format** — Transform the `PipelineOutcome` into the channel-specific
   response format (JSON, SSE stream, Telegram message, etc.).

Connectors contain **zero business logic**. If you find yourself writing an
`if` statement in a connector that isn't about parsing or formatting, the
logic belongs in the factory.

### How to verify compliance

Ask: "If I deleted this connector and wrote a new one for a hypothetical
channel, would the new channel get all the same behavior?" If the answer is
no, business logic has leaked into the connector.

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

```
cmd/ ──> daemon/ ──> api/ ──> pipeline/ ──> agent/ ──> llm/ ──> core/
                       │          │            │          │        ^
                       │          │            │          └────────┘
                       │          │            ├──> db/ ──> core/
                       │          │            │
                       │          │            └──> channel/ ──> core/
                       │          │
                       │          ├──> schedule/ ──> db/
                       │          │
                       │          └──> wallet/ ──> core/
                       │
                       └──> browser/ ──> core/
```

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
