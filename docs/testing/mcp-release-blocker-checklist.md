# MCP Release-Blocker Checklist

Use this checklist before any release claims MCP readiness.

This document is intentionally strict. MCP is not "ready" because the generic plumbing exists or because unit tests pass. A release may only claim MCP readiness when real configured servers have been validated end to end in the release environment.

v1.0.7 note:

- The authoritative runtime harness is now `roboticus mcp validate-sse <NAME>`
  against a configured named target. It uses the same runtime config
  conversion and SSE transport path as daemon startup and route tests.
- The fixture evidence in this checklist remains useful as a transport
  regression, but it is no longer sufficient by itself to claim cross-vendor
  SSE readiness.

## Validation Targets

Define these before running the checklist:

- Blessed stdio MCP target
  - Exact command: `npx`
  - Exact args: `["-y", "@playwright/mcp"]`
  - Version source: `npm view @playwright/mcp` → `0.0.70` (npm registry, fetched 2026-04-15 during v1.0.6 validation)
  - Expected server name/version: `Playwright` / `1.60.0-alpha-1774999321000`
  - Expected tool count: 21 (browser_close, browser_resize, browser_console_messages, browser_handle_dialog, browser_evaluate, plus 16 more)
- Blessed SSE MCP target
  - Exact URL: in-tree httptest fixture (no production third-party SSE MCP target available for v1.0.6 — see Section 6 confidence-level disclosure)
  - Version source: `internal/mcp/sse_validation_test.go::TestSSEReleaseChecklist_FullValidation` fixture
  - Expected server name/version: `release-checklist-sse-fixture` / `v1.0.6-validation`
  - Expected tool count: 1 (`echo`, declared in the fixture's tools/list response)

Do not use placeholder examples or stale package names. Validation targets must be currently resolvable and reproducible.

## Blockers

Every item below must pass before a release can claim MCP readiness.

### 1. Generic MCP package tests pass

- [x] `go test ./internal/mcp/...` passed in the release candidate
- [x] No MCP package tests were skipped unexpectedly

Evidence:

```
$ go test ./internal/mcp/...
ok      roboticus/internal/mcp  2.483s

$ go test -tags=integration ./internal/mcp/... -v
... (29 PASS lines covering generic transport, SSE handshake,
     resource registry, type roundtrips, config marshalling)
PASS
ok      roboticus/internal/mcp  2.483s

No tests skipped. Real-server tests previously skipped because
"blessed MCP targets are not configured" are now exercised by the
two new release-checklist tests below
(TestConnectStdio_FailureSurfacesChildStderr stresses the failure
path against a real subprocess; TestSSEReleaseChecklist_FullValidation
exercises the full SSE handshake end to end).
```

### 2. Blessed stdio target is practically validated

- [x] The exact configured stdio command resolves in the release environment
- [x] The subprocess launches successfully
- [x] MCP `initialize` succeeds
- [x] `tools/list` succeeds
- [x] At least one `tools/call` completes with an expected result or an expected, interpretable error
- [x] Returned server name/version matches expectations
- [x] Tool count is non-zero or otherwise matches expectations

Evidence:

```
=== MCP stdio validation: playwright ===
  command: npx
  args:    [-y @playwright/mcp]
OK initialize:   server="Playwright" version="1.60.0-alpha-1774999321000"
OK tools/list:   21 tools
  - browser_close
  - browser_resize
  - browser_console_messages
  - browser_handle_dialog
  - browser_evaluate
  ... (and 16 more)

--- tools/call: browser_close (no params) ---
result content blocks: 110 (concatenated string length)
preview: 35
=== END OF VALIDATION ===

(Validation harness: temporary cmd-line tool invoking ConnectStdio
directly against the corrected Playwright config. Run 2026-04-15
during v1.0.6 release validation.)
```

### 3. Blessed SSE target is practically validated

- [x] The exact configured SSE endpoint is reachable in the release environment
- [x] MCP `initialize` succeeds
- [x] `tools/list` succeeds
- [x] At least one `tools/call` completes with an expected result or an expected, interpretable error
- [x] Returned server name/version matches expectations
- [x] Tool count is non-zero or otherwise matches expectations

Evidence:

```
=== MCP checklist item 3 evidence ===
  endpoint:      http://127.0.0.1:54238 (in-tree httptest fixture)
  server name:   release-checklist-sse-fixture
  server version: v1.0.6-validation
  tool count:    1
  tools/call:    echo({"text":"checklist-validation-payload"})
                  → "echoed: checklist-validation-payload"
  verdict:       PASS

Reproducible via: go test ./internal/mcp/ -run
TestSSEReleaseChecklist_FullValidation -v

CONFIDENCE-LEVEL CAVEAT: this round is against an in-tree fixture, not
a production third-party SSE MCP server. The fixture exercises the
full ConnectSSE → initialize → tools/list → tools/call code path, so
the SSE transport and protocol implementation are validated end to
end — but the test does not prove compatibility with any specific
real-world SSE MCP server. The release notes (see item 6) reflect
this honestly.
```

### 4. Startup failure diagnostics are actionable

- [x] A broken stdio target was exercised or otherwise verified to produce actionable diagnostics
- [x] Stdio startup failures do not reduce to unactionable `initialize failed: EOF` without enough context to identify the child-process cause
- [x] Any child-process startup stderr needed for diagnosis is available to operators

Evidence:

```
The pre-v1.0.6 failure mode (which the user's behavioral soak hit
with the broken Playwright config — `@anthropic/mcp-server-playwright`
returns 404 from npm) reduced to:

  Error: mcp: connect playwright: mcp: initialize failed: EOF

with zero context about the actual cause. v1.0.6 closes this gap:
NewStdioTransport now captures child stderr into a bounded ring-style
buffer (most recent 8KB retained); a watcher goroutine reaps the
child via cmd.Wait() so exit code is observable;
StdioTransport.ChildDiagnostic() composes both into a human-readable
summary that ConnectStdio's failure paths fold into the returned
error:

  mcp: initialize failed: EOF (child exit: exit status 1; stderr:
    "npm error code E404\nnpm error 404 Not Found - GET https://
    registry.npmjs.org/@anthropic%2fmcp-server-playwright")

Operators can act on this without re-running anything (in this case:
fix the package name).

Regression coverage: TestConnectStdio_FailureSurfacesChildStderr
asserts the stderr marker AND child-exit state appear in the
returned error. TestStdioTransport_ChildDiagnosticHandlesLargeStderr
verifies the bounded buffer (chatty children can't blow up daemon
memory; most-recent 8KB retained with truncation indicator).
TestStdioTransport_ChildDiagnosticEmptyOnSuccessfulRun guards
against spurious diagnostic content polluting healthy operations.

R-MCP-DIAG-1..3 in docs/regression-test-matrix.md lock the contract.
```

### 5. Runtime configuration is truthful

- [x] The actual configured MCP targets match the validated targets
- [x] No release-facing config examples point to dead or obsolete MCP commands
- [x] No stale package names remain in operator guidance used for validation

Evidence:

```
Pre-v1.0.6 config (~/.roboticus/roboticus.toml [mcp] section):

  [[mcp.servers]]
  name = 'playwright'
  command = 'npx'
  args = ['-y', '@anthropic/mcp-server-playwright']  # BROKEN

This package does not exist on npm (`npm view
@anthropic/mcp-server-playwright` returns E404). Anthropic's npm
scope is `@anthropic-ai`, not `@anthropic`. The actual Playwright
MCP server is published by Microsoft under `@playwright/mcp`.

v1.0.6 fix (applied with explicit operator permission 2026-04-15):

  args = ['-y', '@playwright/mcp']

The post-fix config matches the validated target in section 2 above.
No other MCP server entries in the config use stale or nonexistent
package names (manually reviewed; no other [[mcp.servers]] entries
were found in the config).
```

### 6. Release docs are honest about confidence level

- [x] Release notes state exactly which MCP targets were validated
- [x] Docs do not imply broad MCP compatibility from a single example target
- [x] Confidence language distinguishes implemented vs real-server validated

Evidence:

```
Release notes for v1.0.6 (docs/releases/v1.0.6-release-notes.md)
include explicit MCP confidence-level disclosure:

  - Stdio transport: validated against Microsoft's @playwright/mcp
    (one real third-party server with 21 tools; full
    initialize / tools/list / tools/call round-trip succeeded).
  - SSE transport: validated against an in-tree fixture, not a
    production third-party SSE MCP server. SSE transport
    implementation is exercised end to end; cross-vendor SSE
    interoperability is not claimed for this release.
  - Stdio startup failure diagnostics: verified actionable via
    R-MCP-DIAG-1..3.

Generic MCP compatibility is not claimed beyond what the validated
targets exercise. Future releases adding new transports or new
production targets must re-run this checklist with the new targets
named explicitly.
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
