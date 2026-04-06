# Roboticus Migration And Release Policy

This document defines the transition policy from Roboticus (Rust) to Roboticus
(Go).

It is normative for release planning, parity work, and release gating.

## 1. Transition Decision

The transition plan is:

1. **Roboticus `v0.11.4` is the final Rust-based release.**
2. That release becomes the frozen functional baseline for Roboticus parity.
3. Roboticus must become a functionally exact replica of Roboticus `v0.11.4`
   before Roboticus begins shipping the next net-new feature wave.
4. Net-new post-`v0.11.4` product work ships only from Roboticus.

## 2. Frozen Baseline Rule

For migration purposes, the parity target is:

- the exact Roboticus `v0.11.4` release tag/commit
- plus its release docs and shipped integration/regression expectations

After that baseline is frozen:

- parity work flows one way only: **Roboticus `v0.11.4` -> Roboticus**
- Roboticus is no longer treated as an evolving implementation peer
- Roboticus may not declare migration complete based only on audit confidence

## 3. Definition Of "Perfect Replica"

For this transition, "perfect replica" means:

1. Roboticus implements every required `v0.11.4` user-visible and
   operator-visible capability.
2. Roboticus preserves the same functional behavior for those capabilities,
   subject to normalization of genuinely nondeterministic values.
3. No advertised Roboticus feature behaves differently from `v0.11.4` unless
   the difference is:
   - explicitly documented,
   - intentionally accepted,
   - and excluded from the parity-complete claim.
4. No required feature is backed by placeholder behavior, fake success,
   silent degraded fallback, or undocumented omission.
5. Public distribution and upgrade behavior remain intact through the existing
   operator contract unless an explicit migration is announced.

## 4. Evidence Requirement

Parity is not accepted based only on code review, code audit, or feature-name
matching.

Parity must be supported by evidence from:

- code audit
- feature-complete checklist review
- unit tests
- integration tests
- route/API contract tests
- end-to-end tests
- smoke tests
- soak/stability tests
- regression tests
- efficacy/fitness tests where the feature claim is behavioral rather than
  purely mechanical

## 5. Blocking Release Gates

Roboticus may not release the next post-`v0.11.4` feature wave until all of the
following are true:

1. The baseline is frozen to Roboticus `v0.11.4`.
2. `docs/feature-complete-checklist.md` is fully satisfied for all required
   items.
3. `docs/regression-test-matrix.md` has required coverage in place.
4. The parity audit reports no required gaps against the frozen Rust baseline.
5. The full test battery passes, including:
   - `go test ./...`
   - architecture fitness tests
   - integration and route suites
   - live smoke tests
   - any defined soak/regression batteries
6. No advertised Roboticus feature is still relying on:
   - placeholder output
   - fake-complete API responses
   - dashboard controls with dead behavior
   - CLI commands that only narrate unavailable functionality
7. Public install, release, and upgrade surfaces are transition-safe, including:
   - `roboticus.ai`
   - public installer scripts
   - release checksums and artifacts
   - `roboticus update all`
   - `roboticus upgrade all`

## 6. Implementation Freeze Rule

Until Roboticus is accepted as the `v0.11.4` replica:

- Roboticus must prioritize parity completion and regression confidence over
  net-new feature work.
- New Roboticus-only features must not be merged if they materially complicate
  proving parity against the frozen baseline.
- Architecture changes are allowed when they improve parity delivery or protect
  future roadmap flexibility, but they must not change required `v0.11.4`
  behavior.

## 7. Post-Parity Release Rule

After Roboticus is accepted as the `v0.11.4` replica:

- Roboticus becomes the sole implementation line for future releases.
- The next Roboticus release is the first release allowed to contain the next
  planned feature set that would otherwise have landed after Roboticus
  `v0.11.4`.
- From that point onward, regression protection in Roboticus becomes the
  primary product guarantee; permanent cross-implementation parity maintenance
  is no longer required.

## 8. Governing Documents

This policy works together with:

- `docs/feature-complete-checklist.md`
- `docs/regression-test-matrix.md`
- `docs/roadmap-architecture-intake.md`
- `docs/release-ceremony.md`
- `docs/roboticus-ai-integration-plan.md`

Roles of each document:

- `migration-release-policy.md`: defines the transition and release rules
- `feature-complete-checklist.md`: defines what parity-complete means
- `regression-test-matrix.md`: defines the minimum evidence required
- `roadmap-architecture-intake.md`: defines future roadmap pressure that should
  shape architecture during the parity push
- `release-ceremony.md`: defines mature release and rollout requirements
- `roboticus-ai-integration-plan.md`: defines site, installer, and upgrade
  compatibility requirements

## 9. Operating Principle

The sequence is:

1. Release Roboticus `v0.11.4`
2. Freeze it as the final Rust baseline
3. Make Roboticus a provable replica
4. Release future functionality only from Roboticus

That sequence is mandatory for the transition strategy.
