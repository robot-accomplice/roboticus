# MCP Release-Blocker Checklist

Use this checklist before any release claims MCP readiness.

This document is intentionally strict. MCP is not "ready" because the generic plumbing exists or because unit tests pass. A release may only claim MCP readiness when real configured servers have been validated end to end in the release environment.

## Validation Targets

Define these before running the checklist:

- Blessed stdio MCP target
  - Exact command:
  - Exact args:
  - Version source:
  - Expected server name/version:
  - Expected tool count:
- Blessed SSE MCP target
  - Exact URL:
  - Version source:
  - Expected server name/version:
  - Expected tool count:

Do not use placeholder examples or stale package names. Validation targets must be currently resolvable and reproducible.

## Blockers

Every item below must pass before a release can claim MCP readiness.

### 1. Generic MCP package tests pass

- [ ] `go test ./internal/mcp/...` passed in the release candidate
- [ ] No MCP package tests were skipped unexpectedly

Evidence:

```
Paste test command and result summary here.
```

### 2. Blessed stdio target is practically validated

- [ ] The exact configured stdio command resolves in the release environment
- [ ] The subprocess launches successfully
- [ ] MCP `initialize` succeeds
- [ ] `tools/list` succeeds
- [ ] At least one `tools/call` completes with an expected result or an expected, interpretable error
- [ ] Returned server name/version matches expectations
- [ ] Tool count is non-zero or otherwise matches expectations

Evidence:

```
Paste exact command, returned server metadata, tool count, and tool-call result here.
```

### 3. Blessed SSE target is practically validated

- [ ] The exact configured SSE endpoint is reachable in the release environment
- [ ] MCP `initialize` succeeds
- [ ] `tools/list` succeeds
- [ ] At least one `tools/call` completes with an expected result or an expected, interpretable error
- [ ] Returned server name/version matches expectations
- [ ] Tool count is non-zero or otherwise matches expectations

Evidence:

```
Paste endpoint, returned server metadata, tool count, and tool-call result here.
```

### 4. Startup failure diagnostics are actionable

- [ ] A broken stdio target was exercised or otherwise verified to produce actionable diagnostics
- [ ] Stdio startup failures do not reduce to unactionable `initialize failed: EOF` without enough context to identify the child-process cause
- [ ] Any child-process startup stderr needed for diagnosis is available to operators

Evidence:

```
Paste failed-target output or explain why existing diagnostics were sufficient.
```

### 5. Runtime configuration is truthful

- [ ] The actual configured MCP targets match the validated targets
- [ ] No release-facing config examples point to dead or obsolete MCP commands
- [ ] No stale package names remain in operator guidance used for validation

Evidence:

```
List the config source reviewed and note any corrections required.
```

### 6. Release docs are honest about confidence level

- [ ] Release notes state exactly which MCP targets were validated
- [ ] Docs do not imply broad MCP compatibility from a single example target
- [ ] Confidence language distinguishes implemented vs real-server validated

Evidence:

```
Link the release note or doc section updated for this release.
```

## Pass/Fail Rule

Pass only if:

- all blockers pass
- the validated targets are named explicitly
- the evidence bundle is attached to the release record

Fail if any of the following are true:

- only unit or mock tests passed
- stdio still fails with opaque `EOF` diagnostics
- validation depended on stale package names or stale commands
- docs imply practical MCP readiness without real-server evidence

## Evidence Bundle

Attach the following to the release record:

- exact stdio command and args
- exact SSE endpoint
- returned server name/version for each target
- discovered tool count for each target
- one tool-call result for each target
- stderr or equivalent diagnostics for any startup failure
- final pass/fail verdict

## Final Release Statement Template

Use this only if every blocker passed:

> MCP readiness validated for this release against one blessed stdio target and one blessed SSE target. Validation covered connect, initialize, tools/list, and one tool call per target in the release environment. The validated targets and evidence bundle are attached to the release record.

If any blocker failed, do not use MCP-ready release language.
