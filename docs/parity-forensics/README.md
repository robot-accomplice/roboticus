# Parity Forensics Program

This directory is the durable working set for the Rust-to-Go parity-forensics
program. It exists because repeated feature-by-feature remediation has still
left us with hidden drift, shadow paths, and "implemented but not live"
failures.

The program is intentionally documentation-first:

1. Define a system boundary.
2. Trace the live Go runtime path.
3. Trace the corresponding Rust path.
4. Catalog every observed divergence.
5. Classify each divergence.
6. Remediate only degradations and missing functionality by default.
7. Re-audit seam changes before moving to the next system.

Important: `Improvement` does not mean "stop thinking about Rust here."
The migration goal is not merely to beat Rust feature-for-feature, but to
retain the strongest properties of both systems. When Go is richer, the audit
must still ask whether the Rust design contributes a complementary strength
worth integrating into a combined architecture.

This directory must remain useful even if the current agent session ends
mid-audit. Every system document should be readable as a handoff artifact.

## Classification Rules

Every divergence must be placed into exactly one bucket:

- `Idiomatic Shift`
  - Go differs from Rust, but the runtime behavior and operator contract are
    equivalent enough that no remediation is required.
- `Improvement`
  - Go differs intentionally and measurably improves the behavior or
    operability without violating the frozen migration baseline.
  - This does **not** exempt the divergence from design review. The follow-up
    question is whether Rust still contributes a complementary property that
    should be integrated rather than discarded.
- `Degradation`
  - Go behaves worse than Rust or violates the stated architecture rules.
  - Default action: remediate.
- `Missing Functionality`
  - Rust behavior exists, but the Go live path does not provide it.
  - Default action: remediate.

## Audit Rules

- "Code exists" does not count as parity.
- Only the live path counts.
- Helper-only tests are not enough when the production request path bypasses
  the helper.
- Duplicate plausible implementations are treated as defects until one is
  clearly authoritative.
- Release docs may not claim parity/completeness if the live path still
  bypasses the parity implementation.
- "Richer than Rust" is not enough on its own. For retrieval, recall,
  verification, and routing especially, classify whether the best end state is
  Go-only, Rust-only parity, or an intentional synthesis of both strengths.

## Closure Rules

No system may be marked `validated` or "complete" unless all of the following
are true:

1. The authoritative live Go path is explicitly identified.
2. Every currently known divergence in that system has been cataloged in the
   divergence register.
3. Every divergence has a classification, and every `Improvement` or
   `Idiomatic Shift` has an explicit justification rather than hand-waving.
4. Every `Degradation` and `Missing Functionality` item is either:
   - remediated and re-audited, or
   - intentionally deferred with a documented reason, risk, and downstream
     impact.
5. The artifact boundary for the system has been checked directly on the live
   path. Helper behavior alone is not sufficient.
6. The document records concrete evidence for closure:
   - runtime artifact proof
   - tests that exercise the live path
   - docs/release-truth updates if operator-facing behavior changed
7. Any duplicate plausible implementations have been resolved or explicitly
   demoted as non-authoritative.

The program defaults to skepticism:

- "Looks wired" is not enough.
- "Tests pass" is not enough unless the tests touch the artifact boundary.
- "Feature exists" is not enough unless the artifact proves the live path is
  using it.

For v1.0.7, [v1.0.7-roadmap.md](./v1.0.7-roadmap.md) is the authoritative
execution inventory for remaining parity work. A remaining gap is not allowed
to exist only as prose in a system document, release note, or old audit log.

## Required Success Criteria Per System

Each system document must include an explicit `Success Criteria` section.

That section should answer, unambiguously:

- What exact artifact proves parity/alignment for this system?
- What exact live-path tests or traces must pass?
- What conditions would still block closure?
- What accepted deviations remain, and why are they safe to keep?

If a system cannot yet state these crisply, it is not ready to be marked
`validated`.

## System Order

The intended audit order is:

1. Request construction and context assembly
2. Tool exposure, pruning, and execution loop
3. Memory retrieval, compaction, and injection
4. Verification, guards, and post-processing
5. Routing and model selection
6. Session continuity, persistence, and learning
7. Install, update, service lifecycle, and config loading
8. MCP and external integrations
9. Admin, dashboard, and observability surfaces

Cross-cutting systems promoted after the initial 9:

10. Security, policy, and sandbox semantics
11. Scheduler, automation, and cron runtime
12. Plugin and script runtime
13. Channel adapter behavior
14. Cache and replay semantics

The order may change if one audit materially reshapes the seam for the next.
When that happens, update `parity-ledger.md`.

## Required Artifacts Per System

Each system document must contain:

- Scope
- Rust source anchors
- Go source anchors
- Live path summary
- Artifact boundary
- Success criteria
- Divergence table
- Classification and remediation status
- Open questions
- Downstream systems affected
- Progress log

Use `system-template.md` for new systems.

## Current Status

- Framework initialized: 2026-04-16
- First seeded system:
  [01-request-construction-and-context-assembly.md](./systems/01-request-construction-and-context-assembly.md)
