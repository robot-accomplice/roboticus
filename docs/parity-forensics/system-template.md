# System NN: <Name>

## Status

- Owner:
- Audit status: `not started | in progress | remediation in progress | validated`
- Last updated:
- Related release:

## Why This System Matters

Describe why this system is core and what downstream behavior depends on it.

## Scope

List exactly what is in scope and what is out of scope.

## Rust Source Anchors

| Concern | File(s) / function(s) |
|---------|------------------------|
| Example | `src/...` |

## Go Source Anchors

| Concern | File(s) / function(s) |
|---------|------------------------|
| Example | `internal/...` |

## Live Go Path

Describe the actual production entrypoint and the runtime ownership path.

## Artifact Boundary

What concrete runtime artifact proves parity for this system?

Examples:

- final `llm.Request`
- selected tool list
- selected model profile
- verifier input structure
- persisted memory rows

## Success Criteria

This section must be explicit enough that another agent cannot plausibly call
the system "done" based on a superficial implementation.

Required contents:

- `Closure artifact(s)`:
  - Name the exact runtime artifact(s) that must be inspected.
- `Live-path proof`:
  - Name the exact tests, traces, or runtime observations that exercise the
    authoritative path.
- `Blocking conditions`:
  - List the remaining facts that would prevent `validated` status.
- `Accepted deviations`:
  - List any divergences that will remain after closure, with justification.

Example prompts:

- Does the final live artifact prove the new implementation is actually used?
- Would the current evidence catch a helper-only implementation that the live
  path bypasses?
- Have duplicate plausible implementations been removed or clearly demoted?

## Divergence Register

| ID | Priority | Concern | Rust behavior | Go behavior | Classification | Status | Evidence |
|----|----------|---------|---------------|-------------|----------------|--------|----------|
| SYS-NN-001 | P1 | Example | ... | ... | Degradation | Open | file anchors |

## Intentional Deviations

Document any accepted idiomatic shifts or improvements that should not be
treated as regressions.

For every `Improvement`, also answer:

- What complementary strength from Rust, if any, should still be integrated?
- Is the desired end state "keep the Go behavior", "restore Rust behavior", or
  "synthesize both into a stronger combined design"?

## Remediation Notes

Track active implementation work, risks, and acceptance criteria.

Do not use vague phrases like "feature landed" or "parity achieved."
Tie remediation notes back to the success criteria and artifact boundary.

## Downstream Systems Affected

List systems that must be revisited after this one changes.

## Open Questions

- Question 1

## Progress Log

- YYYY-MM-DD: Initialized system document.
