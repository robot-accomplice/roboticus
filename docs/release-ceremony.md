# Roboticus Release Ceremony

This document defines the mature release ceremony required before Roboticus can
replace Roboticus in production.

It covers:

- release preparation
- blocking validation gates
- artifact publication
- `roboticus.ai` synchronization
- rollout and rollback

This ceremony is normative for post-parity releases and for the final
Rust-to-Go cutover release.

## Release Objectives

Every Roboticus release must guarantee all of the following:

1. The shipped binaries match the version being announced.
2. Release assets are complete across all required platforms.
3. Checksums and provenance are published and internally validated.
4. `roboticus.ai` reflects the new release before public announcement.
5. Operator install and upgrade flows remain intact.
6. Release notes, changelog, and product claims match actual behavior.

## Historical Constraint

Roboticus historically treated the release ceremony as a multi-repo workflow:

- source repo publishes release artifacts and checksums
- `roboticus.ai` syncs registry/docs/changelog/metrics from the tagged source
- the site deploy is the public distribution and documentation surface

Roboticus must preserve that operational maturity.

## Required Public Compatibility Contract

For the Rust-to-Go transition release, the public operator contract remains:

- binary name: `roboticus`
- canonical website: `https://roboticus.ai`
- install entrypoints:
  - `https://roboticus.ai/install.sh`
  - `https://roboticus.ai/install.ps1`
- operator update flows:
  - `roboticus update all`
  - `roboticus upgrade all`
  - `roboticus update binary`

If any of these change, the change must be explicitly versioned, documented,
and migration-assisted. They may not drift accidentally during the cutover.

## Ceremony Stages

## 1. Release Preparation

Before tagging:

1. Confirm release scope against `docs/feature-complete-checklist.md`.
2. Confirm required regression coverage against
   `docs/regression-test-matrix.md`.
3. Confirm README, CLI help, dashboard copy, and docs contain no stale Rust-only
   implementation claims.
4. Confirm `roboticus.ai` content updates are prepared or automatable for the
   target version.
5. Confirm artifact naming, checksum generation, and install/update paths match
   the public compatibility contract.

## 2. Blocking Validation Gates

The release candidate must pass:

- `go test ./...`
- `go test ./internal/api -run Architecture -count=1`
- `go test ./internal/llm ./internal/db ./internal/api -count=1`
- `go test -v -run TestLiveSmokeTest .`
- parity audit against the frozen Rust baseline
- any required soak and regression batteries
- release-specific install/update smoke
- release-specific `roboticus.ai` sync dry run

No tag may be considered releasable if any of these fail.

## 3. Artifact Build And Validation

Release automation must produce:

- all supported platform binaries
- a canonical checksum file (`SHA256SUMS.txt`)
- release notes
- a changelog-aligned GitHub release body

For the transition release, the artifact set must support both:

- Roboticus repository provenance
- Roboticus-compatible consumer expectations

That means the release process must explicitly validate:

- expected filenames are present
- every published archive has a checksum
- install/update scripts can resolve the new release
- binary replacement behavior is correct on Linux, macOS, and Windows

## 4. Site Synchronization

The release is not complete when GitHub assets exist. It is complete only when
`roboticus.ai` has synchronized and deployed successfully.

Required synchronized surfaces:

- install page
- public installer scripts
- changelog
- registry data
- release checksum/download metadata
- docs links and architecture references
- home page/version messaging

## 5. Announcement Readiness

Before announcing a release:

1. Verify the public site is serving the new version metadata.
2. Verify install snippets point to the correct binary/update path.
3. Verify release downloads and checksums are reachable.
4. Verify the changelog page contains the new release.
5. Verify the roadmap/docs do not overclaim unfinished features.

## 6. Post-Release Validation

Immediately after release:

1. Run a fresh install from `roboticus.ai/install.sh`.
2. Run a Windows install from `roboticus.ai/install.ps1`.
3. Run `roboticus update check`.
4. Run `roboticus update all --yes`.
5. Run `roboticus upgrade all --yes`.
6. Verify daemon restart and health after update.
7. Verify registry/provider/skill content remains intact.

## 7. Rollback Rule

If any of the following are broken, the release is not healthy:

- public install
- public upgrade
- checksum verification
- site deploy
- release notes/changelog mismatch
- broken or missing platform artifact

In that case:

1. stop public announcement
2. mark the release unhealthy internally
3. either repair in place with the documented release-repair path or publish a
   superseding corrective release
4. do not claim the release complete until the site and installer surfaces are
   repaired

## Human Sign-Off Checklist

The release lead must explicitly confirm:

- parity or planned feature scope is accurate
- tests and soak gates passed
- `roboticus.ai` sync completed
- install and upgrade surfaces were validated
- checksums and release assets are complete
- user-facing docs and changelog are accurate

## Minimum Automation Expectations

The release ceremony is not mature unless automation covers:

- test gating
- artifact generation
- checksum generation
- release creation
- site synchronization trigger
- site build validation
- installer/update smoke where practical

Manual work should remain only for:

- release approval
- staged announcement
- exceptional rollback decisions
