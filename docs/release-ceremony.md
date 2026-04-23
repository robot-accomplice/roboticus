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

## Release Control Plane Invariant

The release path is a single control plane, not a loose collection of steps:

1. git tag
2. release gate passes on that exact tag
3. GitHub Release object is published for that tag
4. release assets and `SHA256SUMS.txt` are attached to that Release object
5. `releases/latest` resolves to that published release
6. `roboticus.ai` syncs from the same release truth
7. public installer scripts, install page, changelog, and operator upgrade flows
   all resolve the same version

A tag without a successful GitHub Release object is not a release. A GitHub
Release without site sync is not a complete release. Site installer scripts may
not evolve independently from the source repo's canonical installer scripts.

## v1.0.6 Failure Class

The `v1.0.6` release attempt on 2026-04-19 demonstrated the exact failure mode
this ceremony must prevent:

- the tag existed, but the release workflow failed before publication
- `releases/latest` therefore remained on `v1.0.5`
- the public site installer scripts had drifted from the source repo's
  canonical scripts (`checksums.txt` vs `SHA256SUMS.txt`)
- site release sync was not actually wired from the source repo's release
  event, so no automatic correction happened
- the site sync workflow also assumed source-tree registry files that the
  source repo did not publish on that tag

Future releases must treat this as a release-blocking architecture seam, not as
an operational footnote.

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
- live verification that the tagged release can become GitHub `latest` with the
  expected asset set

No tag may be considered releasable if any of these fail.

## 3. Artifact Build And Validation

Release automation must produce:

- all supported platform binaries
- a canonical checksum file (`SHA256SUMS.txt`)
- release notes
- a matching `CHANGELOG.md` section for the exact released version
- a changelog-aligned GitHub release body

The source tree must contain both:

- `docs/releases/vX.Y.Z-release-notes.md`
- `CHANGELOG.md` section `## [X.Y.Z]`

Those are separate required artifacts. Release notes are not a substitute for
the changelog, and the changelog is not a substitute for the release notes.
Future release automation must fail before publication if either is missing.

Release automation must also verify, on the live published release object:

- `releases/tags/<tag>` resolves
- `releases/latest` resolves to the new tag unless intentionally suppressed
- canonical installer assets are attached with the expected filenames
- canonical checksum filename is `SHA256SUMS.txt`
- raw update-in-place binaries and archive installer assets are both present

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

The site synchronization path must additionally guarantee:

- public `/install.sh` is copied from `scripts/install.sh` in the tagged source
- public `/install.ps1` is copied from `scripts/install.ps1` in the tagged source
- the install page does not advertise a broken fallback path
- the site sync workflow only depends on source artifacts/directories that
  actually exist in the tagged source repo
- any fallback logic for malformed historical tags is explicitly scoped to
  release-repair of already-published bad tags, not normal release operation

## 5. Announcement Readiness

Before announcing a release:

1. Verify the public site is serving the new version metadata.
2. Verify install snippets point to the correct binary/update path.
3. Verify `releases/latest` and `releases/tags/<tag>` both resolve.
4. Verify release downloads and checksums are reachable.
5. Verify the public installer scripts match the tagged source scripts.
6. Verify the changelog page contains the new release.
7. Verify the roadmap/docs do not overclaim unfinished features.

## 6. Post-Release Validation

Immediately after release:

1. Run a fresh install from `roboticus.ai/install.sh`.
2. Run a Windows install from `roboticus.ai/install.ps1`.
3. Run `roboticus update check`.
4. Run `roboticus update all --yes`.
5. Run `roboticus upgrade all --yes`.
6. Verify daemon restart and health after update.
7. Verify registry/provider/skill content remains intact.
8. Verify the site release-sync run and production deploy both succeeded.
9. Verify the live public installer scripts still fetch the same checksum
   filename and asset naming the source release published.

## 7. Rollback Rule

If any of the following are broken, the release is not healthy:

- public install
- public upgrade
- checksum verification
- site deploy
- release notes/changelog mismatch
- broken or missing platform artifact
- `releases/latest` not advancing to the published tag
- source installer scripts and public installer scripts drifting

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
