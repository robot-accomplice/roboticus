# Parity Ledger

This file is the program-level index for the parity-forensics effort.

## System Tracker

| Order | System | Doc | Status | Notes |
|------:|--------|-----|--------|-------|
| 1 | Request construction and context assembly | [01-request-construction-and-context-assembly.md](./systems/01-request-construction-and-context-assembly.md) | Validated | Final request artifact audited; prompt compression explicitly deferred |
| 2 | Tool exposure, pruning, and execution loop | [02-tool-exposure-pruning-and-execution-loop.md](./systems/02-tool-exposure-pruning-and-execution-loop.md) | Validated | One authoritative selected tool surface; missing Rust operational tools explicitly deferred where no Go-native analogue exists |
| 3 | Memory retrieval, compaction, and injection | [03-memory-retrieval-compaction-and-injection.md](./systems/03-memory-retrieval-compaction-and-injection.md) | Validated | Stage 8.5 ownership and typed-evidence handoff closed; compatibility normalization retained intentionally |
| 4 | Verification, guards, and post-processing | [04-verification-guards-and-post-processing.md](./systems/04-verification-guards-and-post-processing.md) | Validated | Guard/retry ownership and typed-evidence path closed without canned-response regressions |
| 5 | Routing and model selection | [05-routing-and-model-selection.md](./systems/05-routing-and-model-selection.md) | Validated | Runtime routing, trace winner, and audit events now share one request shape |
| 6 | Session continuity, persistence, and learning | [06-session-continuity-persistence-and-learning.md](./systems/06-session-continuity-persistence-and-learning.md) | Validated | Artifact-driven continuity/learning model accepted |
| 7 | Install, update, service lifecycle, and config loading | [07-install-update-service-lifecycle-and-config-loading.md](./systems/07-install-update-service-lifecycle-and-config-loading.md) | Validated | Operator contract clarified and retained |
| 8 | MCP and external integrations | [08-mcp-and-external-integrations.md](./systems/08-mcp-and-external-integrations.md) | Validated | Transport semantics and readiness governance explicitly separated; cross-vendor SSE proof deferred explicitly |
| 9 | Admin, dashboard, and observability surfaces | [09-admin-dashboard-and-observability-surfaces.md](./systems/09-admin-dashboard-and-observability-surfaces.md) | Validated | Canonical route families and release-truth surfaces mapped explicitly |
| 10 | Security, policy, and sandbox semantics | [10-security-policy-and-sandbox-semantics.md](./systems/10-security-policy-and-sandbox-semantics.md) | Validated | Claims, sandbox, config protection, and denial truth now have one coherent operator contract |
| 11 | Scheduler, automation, and cron runtime | [11-scheduler-automation-and-cron-runtime.md](./systems/11-scheduler-automation-and-cron-runtime.md) | Validated | Durable cron and heartbeat-backed maintenance/runtime duties classified explicitly |
| 12 | Plugin and script runtime | [12-plugin-and-script-runtime.md](./systems/12-plugin-and-script-runtime.md) | Validated | Plugin/runtime lifecycle and shared script execution contract closed |
| 13 | Channel adapter behavior | [13-channel-adapter-behavior.md](./systems/13-channel-adapter-behavior.md) | Validated | Matrix limitation classified explicitly; no invented transport semantics |
| 14 | Cache and replay semantics | [14-cache-and-replay-semantics.md](./systems/14-cache-and-replay-semantics.md) | Validated | Cache/replay semantics closed; prompt compression explicitly rejected for v1.0.6 |

## Program Rules

- Update this ledger when a new system document is created.
- Update system status whenever remediation materially changes the live path.
- If a finding changes the seam for a downstream system, note that here.
- Do not delete system docs after remediation; mark them validated and retain
  them as audit evidence.
- Do not mark a system `validated` unless its document has an explicit
  `Success Criteria` section with artifact-level closure proof.
- "Validated" means the live path, artifact boundary, and accepted deviations
  have all been re-audited after remediation.

## Release-Scoped Deferred Items

These are explicitly *not* vague open seams. They were classified and deferred:

- Prompt compression for v1.0.6: rejected on negative paired-soak evidence and
  left disabled.
- Missing Rust operational tool families without a Go-native analogue:
  deferred beyond v1.0.6 because they require real subagent/task/skill runtime
  work, not narrow parity patches.
- Cross-vendor SSE MCP proof: deferred explicitly; the release documents
  fixture-level confidence only.
