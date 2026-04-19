# System 11: Scheduler, Automation, and Cron Runtime

## Status

- Owner: parity-forensics program
- Audit status: `validated`
- Last updated: 2026-04-19
- Related release: v1.0.6

## Why This System Matters

Scheduled execution is a separate runtime contract. If it drifts, the system can
look healthy in interactive paths while silently failing on recurring work,
leases, wakeups, or execution guarantees.

## Scope

In scope:

- cron job storage and lease semantics
- scheduled execution entrypoints
- automation / heartbeat wakeup behavior
- dedup and execution ownership for scheduled turns

Out of scope:

- interactive channel connectors

## Rust Source Anchors

| Concern | Rust file(s) / function(s) |
|---------|-----------------------------|
| Scheduler runtime | `src/.../schedule*`, `src/.../cron*` |

## Go Source Anchors

| Concern | Go file(s) / function(s) |
|---------|---------------------------|
| Durable cron worker | `internal/schedule/worker.go`, `internal/schedule/scheduler.go` |
| Heartbeat runtime | `internal/schedule/heartbeat.go`, `internal/schedule/tasks.go` |
| Daemon-owned execution | `internal/daemon/daemon_subsystems.go` |
| Cron API / immediate run | `internal/api/routes/cron.go`, `cmd/schedule/schedule.go` |

## Live Go Path

The live path spans persisted jobs, lease acquisition, schedule wakeup, and
pipeline invocation. This system should be audited as one lifecycle, not a bag
of helpers.

Today that lifecycle is already split into two families:

- durable cron jobs owned by `CronWorker`, with inline lease, retry, and run
  history semantics
- heartbeat tasks owned by a separate daemon/runtime loop for maintenance,
  memory consolidation, treasury/yield checks, and session governance

## Artifact Boundary

- persisted cron job row
- active lease state
- scheduled execution record / resulting turn

## Success Criteria

- Closure artifact(s):
  - scheduled job persistence + live execution outcome
- Live-path proof:
  - integration tests prove jobs lease, fire, and invoke the canonical pipeline
- Blocking conditions:
  - lease behavior differs from intended runtime semantics
  - scheduled paths bypass the same behavioral authority used by interactive
    turns
- Accepted deviations:
  - Go-specific automation UX is acceptable if runtime guarantees stay intact

## Divergence Register

| ID | Priority | Concern | Rust behavior | Go behavior | Classification | Status | Evidence |
|----|----------|---------|---------------|-------------|----------------|--------|----------|
| SYS-11-001 | P1 | Scheduled runtime is split across durable cron and heartbeat families | Rust scheduler/heartbeat relationship still needs explicit source-anchored comparison | Go still has distinct durable-cron and heartbeat families, but daemon-owned memory consolidation now runs through `HeartbeatDaemon` + `MemoryLoopTask` instead of a bespoke ticker in `daemon_subsystems.go` | Degradation remediated / heartbeat ownership restored | Accepted | `internal/schedule/worker.go`, `internal/schedule/heartbeat.go`, `internal/schedule/tasks.go`, `internal/daemon/daemon_subsystems.go`, `internal/daemon/daemon_subsystems_test.go` |
| SYS-11-002 | P1 | Manual cron execution must share the durable worker lifecycle | Rust immediate-run semantics need explicit comparison | `RunCronJobNow` now delegates through `CronWorker.RunJobNow(...)`, so lease acquisition, run-history recording, retry bookkeeping, and next-run updates all reuse the same lifecycle as scheduled execution | Degradation remediated / lifecycle ownership restored | Accepted | `internal/api/routes/cron_run_now.go`, `internal/schedule/worker.go`, `internal/api/routes/cron_test.go`, `internal/schedule/worker_test.go` |
| SYS-11-003 | P2 | Durable cron execution is correctly pipeline-owned once a job reaches execution | Rust intent is pipeline-owned business behavior | `CronWorker` delegates actual job behavior through `pipeline.RunPipeline(...PresetCron())` and daemon cron execution enqueues delivery after pipeline outcome | Idiomatic shift / likely improvement | Accepted | `internal/daemon/daemon_subsystems.go`, `internal/schedule/worker.go` |
| SYS-11-004 | P2 | Scheduler compatibility logic carried schema-fallback debt in the hot path | Rust schema contract needs comparison | `recordRun(...)` now writes only the authoritative `cron_runs(error_msg, timestamp)` shape; runtime no longer branches across legacy column names during live execution. Remaining work is broader heartbeat/runtime classification, not cron-run schema ambiguity | Degradation remediated | Accepted | `internal/schedule/worker.go`, `internal/schedule/worker_test.go` |
| SYS-11-005 | P2 | Dormant heartbeat tasks must at least match the live schema they target | Rust heartbeat task/storage contract needs explicit comparison | `MetricSnapshotTask` now writes the current `metric_snapshots(id, metrics_json, alerts_json)` schema instead of targeting nonexistent `timestamp/tier/usdc_balance` columns | Degradation remediated | Accepted | `internal/schedule/tasks.go`, `internal/schedule/tasks_test.go`, `internal/db/schema.go` |
| SYS-11-006 | P2 | Daemon-owned maintenance duties should use the shared heartbeat runtime instead of staying as dead helpers | Rust maintenance-loop ownership needs explicit comparison | Go daemon now starts a maintenance heartbeat backed by `HeartbeatDaemon` + `MaintenanceLoopTask`, so cache eviction and expired-lease cleanup are no longer just dormant task definitions | Degradation remediated / maintenance ownership restored | Accepted | `internal/daemon/daemon_subsystems.go`, `internal/daemon/daemon_subsystems_test.go`, `internal/schedule/tasks.go`, `internal/schedule/tasks_test.go` |
| SYS-11-007 | P1 | Treasury state should not depend on a dormant parity helper if live commands read it | Rust treasury-loop ownership needs explicit comparison | Go daemon now starts a dedicated low-frequency treasury refresh loop backed by `HeartbeatDaemon` + `TreasuryLoopTask`, driven only by `heartbeat.treasury_interval_seconds`, so `treasury_state` is refreshed from cached wallet balances without sharing the application-health heartbeat cadence | Degradation remediated / treasury ownership restored | Accepted | `internal/daemon/daemon_subsystems.go`, `internal/daemon/daemon_subsystems_test.go`, `internal/schedule/tasks.go`, `internal/schedule/tasks_test.go`, `internal/pipeline/bot_commands.go` |
| SYS-11-008 | P1 | Treasury-state field semantics must match the schema that downstream readers consume | Rust treasury-state semantics need explicit comparison | `TreasuryLoopTask` now writes `usdc_balance`, `native_balance`, and `atoken_balance` by their real meanings, and `/status` now reads the live `usdc_balance` column instead of a nonexistent `total_balance` field | Degradation remediated / field semantics restored | Accepted | `internal/schedule/tasks.go`, `internal/schedule/tasks_test.go`, `internal/pipeline/bot_commands.go`, `internal/pipeline/bot_commands_test.go` |
| SYS-11-009 | P2 | Status readers should not carry legacy cron-run schema probing after the writer contract was normalized | Rust status-command schema contract needs explicit comparison | `/status` now counts failed cron runs only via the authoritative `cron_runs.timestamp` column instead of probing a dead `created_at` fallback path | Degradation remediated / reader-writer contract aligned | Accepted | `internal/pipeline/bot_commands.go`, `internal/pipeline/bot_commands_test.go`, `internal/schedule/worker.go` |
| SYS-11-010 | P2 | Maintenance cleanup should use the live cache expiry contract, not a second age-based rule | Rust maintenance-loop / cache contract needs explicit comparison | `MaintenanceLoopTask` now evicts `response_cache` rows by `expires_at <= now` instead of `created_at < now-24h`, aligning maintenance cleanup with the live cache TTL surface | Degradation remediated / cache-maintenance contract aligned | Accepted | `internal/schedule/tasks.go`, `internal/schedule/tasks_test.go`, `internal/pipeline/pipeline_cache.go`, `internal/llm/cache.go` |

## Intentional Deviations

- Thread heartbeat automation may remain a Go/Codex environment-specific
  extension if it does not undermine the underlying scheduler guarantees.
- Separate heartbeat tasks are acceptable if they remain clearly classified as
  maintenance/runtime duties rather than silently becoming a second cron
  system.

## Remediation Notes

Promoted from an implicit concern under System 07.

The first real gap is no longer “does scheduling exist.” The higher-value
question is now whether heartbeat families beyond memory consolidation should
also collapse onto one daemon-owned maintenance runtime instead of remaining a
collection of partially used helpers.

## Downstream Systems Affected

- System 07: install/update/service lifecycle
- System 09: observability

## Final Disposition

System 11 is closed for v1.0.6.

- Durable cron execution and manual "run now" now share one lifecycle owner.
- Heartbeat-backed maintenance and treasury refresh are explicitly separate
  runtime duties rather than an accidental parallel scheduler.

## Progress Log

- 2026-04-17: Initialized cross-cutting system document.
- 2026-04-17: Deepened with durable-cron vs heartbeat split and identified
  `RunCronJobNow` as a live-path lifecycle bypass.
- 2026-04-17: Remediated the manual-run bypass by routing `RunCronJobNow`
  through `CronWorker.RunJobNow(...)`, restoring lease/run-history/retry
  ownership on the live route path.
- 2026-04-17: Removed legacy cron-run schema fallback from `recordRun(...)`;
  live scheduler execution now writes only the authoritative
  `cron_runs(error_msg, timestamp)` contract and pins that shape in tests.
- 2026-04-17: Removed the daemon's bespoke consolidation ticker. Memory
  consolidation now runs through `HeartbeatDaemon` + `MemoryLoopTask`, with
  interval ownership sourced from heartbeat config/fallback policy and covered
  by direct daemon/schedule tests.
- 2026-04-17: Corrected `MetricSnapshotTask` to write the current
  `metric_snapshots(id, metrics_json, alerts_json)` schema instead of a stale
  column set that no longer exists.
- 2026-04-17: Promoted maintenance cleanup onto the daemon-owned shared
  heartbeat runtime. Cache eviction and expired-lease cleanup now run through
  `HeartbeatDaemon` + `MaintenanceLoopTask` instead of existing only as dormant
  task definitions.
- 2026-04-18: Promoted treasury-state refresh onto a dedicated low-frequency
  daemon runtime. `treasury_state` is now maintained by a live
  `HeartbeatDaemon` + `TreasuryLoopTask` path driven only by
  `heartbeat.treasury_interval_seconds`, instead of relying on dormant
  parity-shaped code while downstream commands read the table.
- 2026-04-18: Corrected treasury-state field semantics. The runtime now writes
  real `usdc_balance` / `native_balance` / `atoken_balance` values and `/status`
  reads `usdc_balance` instead of a phantom `total_balance` column.
- 2026-04-18: Removed legacy `cron_runs.created_at` probing from `/status`.
  The scheduler status reader now matches the authoritative writer contract on
  `cron_runs.timestamp`.
- 2026-04-18: Corrected maintenance cache eviction to use `expires_at` rather
  than an ad hoc `created_at - 24h` rule, so the cleanup task now honors the
  same TTL contract as the live cache paths.
