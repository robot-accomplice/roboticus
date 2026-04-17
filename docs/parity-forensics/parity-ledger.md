# Parity Ledger

This file is the program-level index for the parity-forensics effort.

## System Tracker

| Order | System | Doc | Status | Notes |
|------:|--------|-----|--------|-------|
| 1 | Request construction and context assembly | [01-request-construction-and-context-assembly.md](./systems/01-request-construction-and-context-assembly.md) | In progress | Deepened with request-layer seams beyond pruning/compaction: checkpoint restore, prompt-layer tool discoverability, and complexity-tiered prompt assembly |
| 2 | Tool exposure, pruning, and execution loop | [02-tool-exposure-pruning-and-execution-loop.md](./systems/02-tool-exposure-pruning-and-execution-loop.md) | In progress | Split out once the request-shaping seam showed tool-pruning ownership deserved its own audit |
| 3 | Memory retrieval, compaction, and injection | [03-memory-retrieval-compaction-and-injection.md](./systems/03-memory-retrieval-compaction-and-injection.md) | In progress | Seeded with Stage 8.5 ownership; deeper pass now classifies `search_memories` / richer `recall_memory` as likely improvements and keeps the typed-evidence fallback seam as the main open degradation |
| 4 | Verification, guards, and post-processing | [04-verification-guards-and-post-processing.md](./systems/04-verification-guards-and-post-processing.md) | In progress | Deepened with typed-evidence fallback, partial `GuardContext` population, duplicated retry ownership, and a stale-context seam on guard retry |
| 5 | Routing and model selection | [05-routing-and-model-selection.md](./systems/05-routing-and-model-selection.md) | In progress | Deepened to document three competing routing stories: runtime inference, trace reconstruction, and persisted audit events |
| 6 | Session continuity, persistence, and learning | [06-session-continuity-persistence-and-learning.md](./systems/06-session-continuity-persistence-and-learning.md) | In progress | Seeded with working-memory continuity as a protected invariant; deeper pass now flags split checkpoint ownership, test-only checkpoint repository usage, tool-fact executive-state growth, and reflection/consolidation classification as the main open seams |
| 7 | Install, update, service lifecycle, and config loading | [07-install-update-service-lifecycle-and-config-loading.md](./systems/07-install-update-service-lifecycle-and-config-loading.md) | In progress | Deepened with retained operator-contract improvements: absolute config embedding, sidecar updater, repo parity tests, checksum hard-fail, and stub/PID-file lifecycle control |
| 8 | MCP and external integrations | [08-mcp-and-external-integrations.md](./systems/08-mcp-and-external-integrations.md) | In progress | Deepened with transport-vs-governance distinction, stronger validation-guidance tracking, and a concrete timeout/cancellation seam where per-call timeout currently closes the full connection |
| 9 | Admin, dashboard, and observability surfaces | [09-admin-dashboard-and-observability-surfaces.md](./systems/09-admin-dashboard-and-observability-surfaces.md) | In progress | Deepened with stronger observability assets (shape tests, log ring buffer, explicit caveats), direct HTTP-handler reuse for WebSocket snapshots, and sharper trace-route overlap classification |
| 10 | Security, policy, and sandbox semantics | [10-security-policy-and-sandbox-semantics.md](./systems/10-security-policy-and-sandbox-semantics.md) | In progress | Promoted from implicit cross-cutting concern because policy/runtime enforcement is too important to leave split across Systems 04, 07, and 08 |
| 11 | Scheduler, automation, and cron runtime | [11-scheduler-automation-and-cron-runtime.md](./systems/11-scheduler-automation-and-cron-runtime.md) | In progress | Deepened and partially remediated: durable cron worker remains the lifecycle owner, `RunCronJobNow` now reuses that lifecycle, and the remaining open seams are heartbeat/runtime classification plus schema-fallback debt |
| 12 | Plugin and script runtime | [12-plugin-and-script-runtime.md](./systems/12-plugin-and-script-runtime.md) | In progress | Deepened and materially remediated: daemon startup owns plugin registry construction/scan/init, install/enable hot-syncs plugin tools plus descriptor embeddings into the semantic tool surface, and skills/plugin scripts now share one execution contract; remaining work is wrapper-level classification rather than core runtime drift |
| 13 | Channel adapter behavior | [13-channel-adapter-behavior.md](./systems/13-channel-adapter-behavior.md) | In progress | Deepened and partially remediated: Telegram/WhatsApp now use adapter-owned webhook normalization, with remaining work focused on transport metadata classification |
| 14 | Cache and replay semantics | [14-cache-and-replay-semantics.md](./systems/14-cache-and-replay-semantics.md) | In progress | Promoted from Systems 01/04/05 because cache-path behavior has repeatedly created real drift; the stale-entry seam is now closed by explicit TTL ownership on the pipeline cache path, with compression/replay quality still open |

## Program Rules

- Update this ledger when a new system document is created.
- Update system status whenever remediation materially changes the live path.
- If a finding changes the seam for a downstream system, note that here.
- Do not delete system docs after remediation; mark them validated and retain
  them as audit evidence.
- Do not mark a system `validated` unless its document has an explicit
  `Success Criteria` section with artifact-level closure proof.
- "Remediation in progress" means code is moving; it does not imply closure.
- "Validated" means the live path, artifact boundary, and accepted deviations
  have all been re-audited after remediation.

## Current Program Risks

- Hidden shadow paths remain likely wherever Go carries both a parity-shaped
  implementation and an older live path.
- Release and spec docs have drifted multiple times and must be kept in sync
  with remediation ownership changes.
- Some systems are actively being remediated by another agent; document live
  observations separately from expected end state.

## Immediate Next-Pass Order

These are the highest-value next passes once the currently in-flight
request/tool remediation lands:

1. Re-audit System 01 and System 02 against the final `llm.Request` artifact.
   - Confirm the selected tool set, message set, compression/compaction path,
     checkpoint/summary injections, and any prompt-layer tool guidance that
     actually reaches inference.
2. Finish System 03 by validating whether any live downstream consumer still
   depends on rendered `MemoryContext` section parsing when typed evidence is
   present.
3. Continue System 04 with the full guard-registry / retry-path call-site sweep
   now that the main seams are classified.
4. Continue System 05 so routing traces and audit events use the same
   effective request shape as runtime selection.
5. Continue System 06 on checkpoint lifecycle and consolidation classification,
   while explicitly preserving the novel shutdown/startup working-memory model
   and executive-state architecture as protected invariants.
6. Continue the platform-by-platform pass for System 07, now that the retained
   operator-contract improvements and remaining edge-case seams are separated.
7. Continue System 08 with a transport-level classification sweep, keeping
   governance improvements and transport semantics separate.
8. Continue System 09 by mapping canonical observability routes and deciding
   whether JSON-`LIKE` trace search is acceptable or now its own audit target.
9. Use System 09 to keep operational truth and release truth aligned
   with the backend as the lower systems stabilize.
10. Seed and then deepen the new cross-cutting systems only where they expose
    real live-path divergence; do not let them become duplicate audits of the
    original 9.
