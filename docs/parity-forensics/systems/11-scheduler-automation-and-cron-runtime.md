# System 11: Scheduler, Automation, and Cron Runtime

## Status

- Owner: parity-forensics program
- Audit status: `in progress`
- Last updated: 2026-04-17
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
| SYS-11-001 | P1 | Scheduled runtime is split across durable cron and heartbeat families | Rust scheduler/heartbeat relationship still needs explicit source-anchored comparison | Go has a real split: `CronWorker` owns persisted cron jobs with lease/retry/run-history semantics, while `HeartbeatDaemon` runs maintenance-style recurring tasks outside the durable cron lifecycle | Open | Open | `internal/schedule/worker.go`, `internal/schedule/heartbeat.go`, `internal/schedule/tasks.go`, `internal/daemon/daemon_subsystems.go` |
| SYS-11-002 | P1 | Manual cron execution bypasses the durable worker lifecycle | Rust immediate-run semantics need explicit comparison | `RunCronJobNow` calls `pipeline.RunPipeline(...PresetCron())` directly and does not go through lease acquisition, retry bookkeeping, or `cron_runs` recording | Degradation / missing lifecycle ownership | Open | `internal/api/routes/cron.go` |
| SYS-11-003 | P2 | Durable cron execution is correctly pipeline-owned once a job reaches execution | Rust intent is pipeline-owned business behavior | `CronWorker` delegates actual job behavior through `pipeline.RunPipeline(...PresetCron())` and daemon cron execution enqueues delivery after pipeline outcome | Idiomatic shift / likely improvement | Accepted | `internal/daemon/daemon_subsystems.go`, `internal/schedule/worker.go` |
| SYS-11-004 | P2 | Scheduler compatibility logic still carries schema-fallback debt | Rust schema contract needs comparison | `recordRun(...)` still tries new cron-run columns first and falls back to old names, indicating runtime compatibility debt is still in the hot path | Open | Open | `internal/schedule/worker.go` |

## Intentional Deviations

- Thread heartbeat automation may remain a Go/Codex environment-specific
  extension if it does not undermine the underlying scheduler guarantees.
- Separate heartbeat tasks are acceptable if they remain clearly classified as
  maintenance/runtime duties rather than silently becoming a second cron
  system.

## Remediation Notes

Promoted from an implicit concern under System 07.

The first real gap is no longer “does scheduling exist.” It is that not all
scheduled execution paths share the same lifecycle guarantees.

## Downstream Systems Affected

- System 07: install/update/service lifecycle
- System 09: observability

## Open Questions

- Are automation and cron semantics one system or two tightly-coupled layers?
- Should immediate “run now” semantics reuse the durable cron lifecycle
  artifacts, or is bypassing lease/run-history an accepted operator shortcut?

## Progress Log

- 2026-04-17: Initialized cross-cutting system document.
- 2026-04-17: Deepened with durable-cron vs heartbeat split and identified
  `RunCronJobNow` as a live-path lifecycle bypass.
