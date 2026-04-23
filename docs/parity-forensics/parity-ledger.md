# Parity Ledger

This file is the program-level index for the parity-forensics effort.

For v1.0.7, this ledger and
[v1.0.7-parity-roadmap.md](./v1.0.7-roadmap.md) are the authoritative parity
surfaces. If a remaining gap is not represented in the roadmap, it is not
scoped clearly enough yet.

## System Tracker

| Order | System | Doc | Status | Notes |
|------:|--------|-----|--------|-------|
| 1 | Request construction and context assembly | [01-request-construction-and-context-assembly.md](./systems/01-request-construction-and-context-assembly.md) | Validated | Final request artifact remains strong; checkpoint restore fidelity is closed and prompt compression now has an explicit benchmark-only disposition instead of a lingering deferral |
| 2 | Tool exposure, pruning, and execution loop | [02-tool-exposure-pruning-and-execution-loop.md](./systems/02-tool-exposure-pruning-and-execution-loop.md) | Validated | One authoritative selected tool surface exists, and the full v1.0.7 operational-family split is now closed through explicit live-path roster/inventory, skill composition, subagent composition, delegated task lifecycle, and bounded multi-subagent orchestration |
| 3 | Memory retrieval, compaction, and injection | [03-memory-retrieval-compaction-and-injection.md](./systems/03-memory-retrieval-compaction-and-injection.md) | Validated | Stage 8.5 ownership and typed-evidence handoff remain strong; v1.0.7 closed the remaining retrieval full-vision seams (`PAR-012` through `PAR-014`) with explicit fusion, optional LLM reranking, and semantic FTS cleanup |
| 4 | Verification, guards, and post-processing | [04-verification-guards-and-post-processing.md](./systems/04-verification-guards-and-post-processing.md) | Validated | Guard/retry ownership remains closed, and v1.0.7 closed the verifier-depth seam by promoting structured contradiction artifacts plus per-claim proof diagnostics under `PAR-010` and `PAR-011` |
| 5 | Routing and model selection | [05-routing-and-model-selection.md](./systems/05-routing-and-model-selection.md) | Validated | Runtime routing, trace winner, and audit events now share one request shape; retrieval-side fusion/reranking seams are closed and no longer hiding under routing folklore |
| 6 | Session continuity, persistence, and learning | [06-session-continuity-persistence-and-learning.md](./systems/06-session-continuity-persistence-and-learning.md) | Validated | Artifact-driven continuity remains accepted, and v1.0.7 closed the remaining consolidation breadth seam by explicitly classifying Go's broader structured distillation surface as a bounded synthesis rather than parity drift |
| 7 | Install, update, service lifecycle, and config loading | [07-install-update-service-lifecycle-and-config-loading.md](./systems/07-install-update-service-lifecycle-and-config-loading.md) | Validated | Operator contract clarified and retained |
| 8 | MCP and external integrations | [08-mcp-and-external-integrations.md](./systems/08-mcp-and-external-integrations.md) | Validated with narrowed claim | Transport/runtime governance remains strong, and v1.0.7 now has the central SSE validation harness plus endpoint/auth-capable transport and shared config conversion for `PAR-008`; the release claim is explicitly narrowed away from proven cross-vendor third-party SSE interoperability |
| 9 | Admin, dashboard, and observability surfaces | [09-admin-dashboard-and-observability-surfaces.md](./systems/09-admin-dashboard-and-observability-surfaces.md) | Validated | Canonical route families and release-truth surfaces mapped explicitly |
| 10 | Security, policy, and sandbox semantics | [10-security-policy-and-sandbox-semantics.md](./systems/10-security-policy-and-sandbox-semantics.md) | Validated | Claims, sandbox, config protection, and denial truth now have one coherent operator contract |
| 11 | Scheduler, automation, and cron runtime | [11-scheduler-automation-and-cron-runtime.md](./systems/11-scheduler-automation-and-cron-runtime.md) | Validated | Durable cron and heartbeat-backed maintenance/runtime duties classified explicitly |
| 12 | Plugin and script runtime | [12-plugin-and-script-runtime.md](./systems/12-plugin-and-script-runtime.md) | Validated | Plugin/runtime lifecycle and shared script execution contract closed |
| 13 | Channel adapter behavior | [13-channel-adapter-behavior.md](./systems/13-channel-adapter-behavior.md) | Validated | Matrix limitation classified explicitly; no invented transport semantics |
| 14 | Cache and replay semantics | [14-cache-and-replay-semantics.md](./systems/14-cache-and-replay-semantics.md) | Validated | Cache/replay semantics remain strong, and prompt compression now has an explicit benchmark-only disposition rather than a buried carry-forward deferral |

## Program Rules

- Update this ledger when a new system document is created.
- Update system status whenever remediation materially changes the live path.
- If a finding changes the seam for a downstream system, note that here.
- Do not delete system docs after remediation; mark them validated and retain
  them as audit evidence.
- Do not leave a remaining parity gap only in prose. Every remaining gap must
  have one roadmap entry in `v1.0.7-roadmap.md`.
- Do not mark a system `validated` unless its document has an explicit
  `Success Criteria` section with artifact-level closure proof.
- "Validated" means the live path, artifact boundary, and accepted deviations
  have all been re-audited after remediation.

## v1.0.7 Reopened Items

The following items were explicitly carried forward from v1.0.6 and are now
active roadmap work rather than passive deferrals:

- `PAR-008`: cross-vendor SSE MCP proof
