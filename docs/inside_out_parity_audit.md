# Inside-Out Parity Audit

## Status Legend
- `aligned`: behavior and exposed seams appear materially equivalent
- `partial`: substantial overlap, but the system is still shallower or narrower than Rust
- `incorrect`: the system exists but behaves differently in parity-significant ways
- `missing`: the corresponding system/stage/surface is absent
- `unvalidated`: implementation may exist, but evidence is still weak

## 1. Core Execution Pipeline
Status: `partial` (was `incorrect` — critical gaps closed 2026-04-07)

### Goboticus core seam
- `/Users/jmachen/code/goboticus/internal/pipeline/pipeline.go`
- `/Users/jmachen/code/goboticus/internal/pipeline/pipeline_stages.go`
- `/Users/jmachen/code/goboticus/internal/pipeline/config.go`
- `/Users/jmachen/code/goboticus/internal/pipeline/interfaces.go`

### Roboticus core seam
- `/Users/jmachen/code/roboticus/crates/roboticus-pipeline/src/run.rs`
- `/Users/jmachen/code/roboticus/crates/roboticus-pipeline/src/pipeline_types.rs`
- `/Users/jmachen/code/roboticus/crates/roboticus-pipeline/src/config.rs`
- `/Users/jmachen/code/roboticus/crates/roboticus-pipeline/src/capabilities.rs`

### Alignment findings (updated 2026-04-07)

Critical gaps closed:
- ~~dedup tracking is missing from the Go pipeline~~ **Fixed**: `dedup.go` implements concurrent request rejection via SHA-256 fingerprint with RAII-style release and TTL expiry. Wired as pipeline stage 3.
- ~~task-operating-state synthesis/planner stage is missing~~ **Fixed**: `task_state.go` implements full task lifecycle (pending/running/completed/failed/delegated) with classification (simple/complex/multi_step/specialist). Wired into pipeline with Create/Start/Classify/Complete/Fail/Delegate.
- ~~decomposition is a lightweight heuristic~~ **Fixed**: decomposition gate now wired as pipeline stage 8, classifies tasks, records delegation decisions in task state tracker.

Significant gaps closed:
- ~~guard chain is stateless~~ **Fixed**: `buildGuardContext()` creates full context from session; `ApplyFullWithContext` used in inference path; retry on guard rejection injects directive and re-runs (max 1 retry).
- ~~no per-turn memory ingest~~ **Fixed**: `post_turn.go` runs background goroutine after each turn: chunks response at sentence boundaries, generates embeddings, stores in DB.
- ~~no context window compaction~~ **Fixed**: `compaction.go` implements 5-stage progressive compaction (verbatim → trim → compress → extract → skeleton). Wired into inference preparation.

Pipeline now has 11 stages matching Rust's authority level:
1. Validation → 2. Injection → 3. Dedup → 4. Session → 5. Followup → 6. Storage → 7. Authority → 8. Decomposition → 9. Skill → 10. Shortcut → 11. Inference+Guards

Remaining gaps:
- Cross-channel consent flow still missing from Go pipeline
- Cron delegation wrapping / model preference hooks not pipeline-level stages
- Goboticus pipeline config does not expose Rust fields like `session_nickname_override`, `model_override`, `background_budget`, `bot_command_dispatch`, `cron_delegation_wrap`, `prefer_local_model`
- Goboticus pipeline interfaces are narrower than Rust capability traits

### Consumed surfaces not in full alignment
- LLM routing/inference contract
- retrieval contract
- tool catalog/policy/approval contract
- event/progress bus contract

### Exposed surfaces not in full alignment
- connector request/result contract is simpler in Go than Rust
- streaming contract is thinner in Go (`Outcome.StreamRequest`) than Rust (`PipelineOutcome::StreamReady(StreamContext)` with dedup guard, prepared inference, shortcut result, resolved stream metadata)

## 2. LLM Routing And Inference
Status: `partial`

### Goboticus
- `/Users/jmachen/code/goboticus/internal/llm/router.go`
- `/Users/jmachen/code/goboticus/internal/llm/service.go`

### Roboticus
- `/Users/jmachen/code/roboticus/crates/roboticus-llm/src/router.rs`
- plus Rust pipeline-driven inference runner integration via AppState in `/Users/jmachen/code/roboticus/crates/roboticus-api/src/api/routes/mod.rs`

### Alignment findings
- Goboticus is stronger than earlier here: it has runtime router selection, circuit breakers, cache, dedup, streaming, quality tracking, and metascore fitness tests.
- But the seam still differs materially from Rust:
  - Rust routes inference through an explicit `InferenceRunner` capability contract consumed by the pipeline.
  - Goboticus keeps routing/inference inside `llm.Service`, but the core pipeline only sees a coarse executor/stream preparer abstraction instead of the richer Rust inference boundary.
- Goboticus router is more advanced than the simple Rust `ModelRouter` file inspected, but parity is still not guaranteed because Rust’s true behavior is pipeline-integrated through routing/audit persistence in the API bridge.
- Goboticus service does cache, fallback, breaker, and metascore behavior, but its pipeline does not own model audit and selection as explicitly as Rust does.

### Primary gap type
- architecture/seam mismatch more than missing feature nouns

## 3. Memory Retrieval And Memory Management
Status: `partial`

### Goboticus
- `/Users/jmachen/code/goboticus/internal/agent/memory/retrieval.go`
- `/Users/jmachen/code/goboticus/internal/agent/memory/manager.go`

### Roboticus
- `/Users/jmachen/code/roboticus/crates/roboticus-agent/src/retrieval.rs`

### Alignment findings
- Goboticus has real multi-tier memory retrieval and ingestion, not placeholders.
- But Rust’s retrieval seam is more mature and observability-rich:
  - Rust returns retrieval text plus structured retrieval metrics
  - Rust supports ANN-backed search integration at the retrieval seam
  - Rust injects ambient recent activity and uses memory index injection rather than simply formatting all tiers
  - Rust retrieval explicitly filters inactive memories and reranks episodic results with decay in a more formalized pipeline
- Goboticus retrieval is substantial but still simpler:
  - returns only formatted text through the pipeline-facing interface
  - embeds episodic content on the fly when available, which is behaviorally heavier and architecturally different
  - lacks the same explicit retrieval metrics contract at the pipeline seam
- Goboticus memory manager ingestion is also simpler and more heuristic than Rust’s memory ecosystem.

### Primary gap type
- shallower internal behavior and thinner observability contract

## 4. Tooling, Policy, Approvals, And Runtime Execution
Status: `partial`

### Goboticus
- `/Users/jmachen/code/goboticus/internal/agent/loop.go`
- `/Users/jmachen/code/goboticus/internal/agent/tools/tool.go`
- `/Users/jmachen/code/goboticus/internal/agent/tools/registry.go`
- `/Users/jmachen/code/goboticus/internal/agent/policy/engine.go`

### Roboticus
- `/Users/jmachen/code/roboticus/crates/roboticus-pipeline/src/capabilities.rs`
- `/Users/jmachen/code/roboticus/crates/roboticus-pipeline/src/tool_executor.rs`
- `/Users/jmachen/code/roboticus/crates/roboticus-agent/src/tools/mod.rs`
- `/Users/jmachen/code/roboticus/crates/roboticus-agent/src/approvals.rs`
- `/Users/jmachen/code/roboticus/crates/roboticus-agent/src/capability.rs`

### Alignment findings
- Goboticus has a real tool registry, tool interface, policy engine, and approval manager.
- Rust’s seam is still much richer and more compositional:
  - capability registry mirrors tool registry and is pipeline-visible
  - approvals are explicitly part of the pipeline tooling contract
  - execution/runtime dependencies are split from tool catalog dependencies
  - MCP live runtime, plugin registry, subagent registry, and approval state are all first-class seam inputs
- Goboticus loop executes tool calls with policy checks, but the pipeline boundary itself does not model the full tool/runtime contract the way Rust does.
- Goboticus tool execution and registry are materially simpler than Rust’s tool/capability/MCP bridge ecosystem.

### Primary gap type
- same class of subsystem exists, but Rust owns much more behavior at the seam and in runtime composition

## 5. API Surface
Status: `partial`

### Goboticus
- `/Users/jmachen/code/goboticus/internal/api/server.go`

### Roboticus
- `/Users/jmachen/code/roboticus/crates/roboticus-api/src/api/routes/mod.rs`

### Alignment findings
- Goboticus exposes a very broad API surface and has closed many earlier missing-route gaps.
- But Rust’s API state and router remain richer and more deeply integrated with the core system:
  - more formal AppState -> pipeline dependency bridge
  - stronger nonce/CSP/dashboard handling
  - richer connector and runtime integration surface
- Goboticus API breadth is now respectable, but parity risk remains in behavioral depth, not only route presence.
- The Go route set should not be assumed aligned just because many endpoints exist; the inner-system audit shows several core consumed seams are still thinner.

### Primary gap type
- route presence is closer than route behavior depth

## 6. CLI Surface
Status: `partial`

### Goboticus
- `/Users/jmachen/code/goboticus/cmd/root.go`
- `/Users/jmachen/code/goboticus/cmd/*.go`

### Roboticus
- `/Users/jmachen/code/roboticus/crates/roboticus-server/src/cli_args.rs`
- `/Users/jmachen/code/roboticus/crates/roboticus-cli/src/cli/**`

### Alignment findings
- Goboticus has broad command coverage and many historical compatibility aliases.
- The surface is still not trustworthy by name alone because several commands are thinner than their Rust counterparts.
- Examples already evidenced in code:
  - `auth` in Go is still API-key centric and explicitly declares OAuth PKCE unsupported in `/Users/jmachen/code/goboticus/cmd/auth.go`, while Rust’s auth contract is OAuth-oriented.
  - `update` in Go has advanced substantially, but the Rust update subsystem still contains fuller maintenance choreography and broader release-system integration.
- The CLI needs command-by-command behavior validation, not just command-tree parity.

### Primary gap type
- behavioral parity still weaker than naming parity

## 7. Update And Maintenance Ceremony
Status: `partial`

### Goboticus
- `/Users/jmachen/code/goboticus/cmd/update.go`

### Roboticus
- `/Users/jmachen/code/roboticus/crates/roboticus-cli/src/cli/update/mod.rs`
- `/Users/jmachen/code/roboticus/crates/roboticus-cli/src/cli/update/update_providers.rs`
- `/Users/jmachen/code/roboticus/crates/roboticus-cli/src/cli/update/update_skills.rs`

### Alignment findings
- Goboticus now has real update state, registry manifest handling, provider/skills updates, and binary update orchestration.
- Rust still has the more mature release-maintenance system:
  - OAuth maintenance hooks
  - mechanic health checks/maintenance integration
  - richer overwrite/local-modification handling
  - fuller ceremony around runtime upgrade and service continuity
- This is no longer a fake shell gap, but it is still not yet a confident “same behavior” claim.

## 8. Dashboard / Web UI
Status: `partial`

### Goboticus
- `/Users/jmachen/code/goboticus/internal/api/dashboard_spa.html`

### Roboticus
- dashboard served from Rust API stack with stronger CSP/nonce handling visible in `/Users/jmachen/code/roboticus/crates/roboticus-api/src/api/routes/mod.rs`

### Alignment findings
- Goboticus still serves a monolithic SPA file, which makes drift easy to hide and hard to test at workflow granularity.
- Rust’s dashboard/security handling is more formalized, and the UI integration seams appear cleaner.
- This surface requires explicit workflow validation, not code-grep confidence.

## Aggregate Assessment (updated 2026-04-07)

The core pipeline was the deepest divergence. As of 2026-04-07, the three
critical gaps (dedup, task state, decomposition) and three significant gaps
(guard context, post-turn ingest, compaction) are closed. The pipeline now
has 11 stages matching Rust’s authority level.

The remaining divergence is now concentrated in:
1. **Architecture/seam width**: Rust’s capability traits are more compositional than Go’s broader interfaces. This is a structural difference, not a missing feature.
2. **Memory retrieval depth**: Go retrieval is simpler than Rust’s ANN-backed, decay-reranked, metrics-rich system.
3. **Tool/runtime composition**: Rust’s tool/capability/MCP bridge is more compositional than Go’s.
4. **Pipeline config fields**: Several Rust config knobs (`model_override`, `background_budget`, `cron_delegation_wrap`, etc.) are not exposed in Go.
5. **Cross-channel consent**: Not implemented in Go.

The most important remaining parity work is:
1. Bring memory retrieval up to Rust’s observability and reranking depth
2. Expose missing pipeline config fields for operator control
3. Strengthen tool/runtime seam composition
4. Only then treat inner-system parity as fully trustworthy
