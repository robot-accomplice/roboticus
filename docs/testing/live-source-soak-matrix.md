# Live Source Soak Matrix (Framework Blocking)

This matrix captures observed live-agent failures and maps each to one controlling
enforcement channel, so behavior is governed systemically rather than by endpoint-specific patches.

## Scope

- Source runtime only (`go run . serve`)
- Live API soak against `/api/agent/message`
- Blocking framework defects only (execution truth, delegation, formatting, continuity, filesystem agency)

## Matrix

| Case ID | Observed Failure | Prompt Archetype | Expected Behavior | Primary Control Channel | Go Status |
| --- | --- | --- | --- | --- | --- |
| LS-001 | Non-action prompts looked frozen | `Acknowledge in one sentence, then wait` | Fast one-sentence acknowledgement, no drift | acknowledgement shortcut | Covered (soak_behavior_test.go) |
| LS-002 | Tool-use prompt returned silence/refusal | `pick one tool at random and use it` | Concrete tool execution evidence in reply | ExecutionBlockGuard (guards_truthfulness.go) | Covered |
| LS-003 | Introspection claims were contradictory | `use introspection and summarize` | Runtime-tool-backed summary (not fabricated) | execution shortcut introspection path | Covered (soak_behavior_test.go) |
| LS-004 | Delegation metadata leaked to user | Geopolitical/delegation outputs | No `delegated_subagent=`, no `subtask N ->`, no orchestration internals | DelegationMetadataGuard (guards_truthfulness.go) | Covered |
| LS-005 | Filesystem capability denied inconsistently | `look in ~/Downloads folder` / `file distribution in ~` | Actual filesystem execution, path expansion shown | FilesystemDenialGuard (guards_truthfulness.go) | Covered |
| LS-006 | Foreign identity/persona bleed | "As an AI developed by ..." boilerplate | Agent identity preserved; foreign boilerplate stripped | personality integrity guard | Covered (guards_truthfulness.go) |
| LS-007 | Stale geopolitical disclaimers | Current-events sitrep prompts | No stale-memory disclaimer language | current events truth guard | Covered (guards_truthfulness.go) |
| LS-008 | Overbroad literary refusal | `Dune quote for Iran context` | Safe contextual quote/paraphrase, no blanket refusal | literary guard + fallback retry | Covered (soak_behavior_test.go) |
| LS-009 | Contradiction follow-up handled poorly | `That's not true` follow-up | Explicit correction path, no canned stale delta | short followup expansion + non-repetition guard | Covered (guards_quality.go) |

## Pass Criteria

- Every case returns a non-empty response within soak latency budget.
- No internal orchestration metadata leaks into user-visible text.
- No foreign identity boilerplate appears.
- Execution/delegation claims must be backed by tool path evidence or explicit failure wording.

## Ownership

- Runtime behavior controls: `internal/pipeline/guards*.go`
- Soak harness: `internal/pipeline/soak_behavior_test.go`
- Guard fitness: `internal/pipeline/guards_fitness_test.go`

## Coverage

All 9 soak matrix controls are now covered by guards in the Go pipeline.
Tests in `soak_behavior_test.go` enforce hard assertions — no gaps remain.
