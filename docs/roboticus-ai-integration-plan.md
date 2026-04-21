# `roboticus.ai` Integration And Cutover Plan

This document records how Roboticus and `roboticus.ai` currently synchronize,
and what Roboticus must do to take over that role without breaking the public
distribution and documentation surface.

## Current Synchronization Model

The current Roboticus -> site flow is release-driven.

## Canonical Inputs Today

From the Roboticus source repository:

- GitHub release tag and release assets
- `CHANGELOG.md`
- `docs/architecture/*`
- `registry/manifest.json`
- `registry/builtin-skills.json`
- `registry/providers.toml`
- `registry/skills/*.md`
- release checksums and archive sizes
- source-tree-derived metrics

From the site repository:

- `release-sync.yml` checks out the tagged Roboticus release
- sync scripts regenerate site data modules
- `deploy.yml` publishes the updated site to Vercel on push to `main`

## v1.0.6 Postmortem Findings

The 2026-04-19 `v1.0.6` release attempt exposed four real integration defects:

1. The source repo produced a tag but no published GitHub Release object,
   because the tag-gated release workflow failed before asset publication.
2. The source repo did not actively trigger the site sync workflow; the only
   notify workflow in-tree was an example file pointed at a different repo.
3. The site's public installer scripts had drifted from the source repo's
   canonical installer scripts, including checksum filename expectations.
4. The site sync workflow assumed `registry/*` files existed in the source
   repo, but the tagged release tree did not provide them.

That means the old "tag and then sync the site" story was not a real control
plane. It was a collection of partially connected steps.

## What The Site Currently Does

`roboticus-site/.github/workflows/release-sync.yml` performs the following:

1. resolves a release version from dispatch input
2. checks out the exact tagged Roboticus release
3. copies canonical registry files into `public/registry`
4. regenerates `src/lib/registry-data.ts`
5. syncs architecture docs index from the source repo
6. regenerates release entries and changelog content
7. regenerates codebase metrics
8. validates public registry integrity
9. builds the site
10. commits and pushes the synchronized result

For this to be trustworthy, the workflow must also:

11. copy the canonical installer scripts from the tagged source repo
12. avoid hard failures on source-tree paths that are not part of the release
    contract
13. fail loudly if the source release object itself does not exist

Then `deploy.yml` builds and deploys the site from `main`.

## Public Surfaces Affected By Release Sync

The following site surfaces are release-coupled and must remain correct during
the Go cutover:

- `/`
- `/install`
- `/registry`
- `/changelog`
- `/docs`
- `/docs/architecture`
- `/roadmap`
- public installer scripts:
  - `/install.sh`
  - `/install.ps1`

## Transition Requirement

Roboticus must become the canonical release source for `roboticus.ai` without
changing the public operator experience unexpectedly.

That means the site must be able to sync from Roboticus while preserving:

- the `roboticus.ai` domain
- the `roboticus` product name where operator compatibility requires it
- the `roboticus` binary/update/install contract
- registry and checksum validation behavior

## Hard Compatibility Requirements

The cutover is incomplete unless all of the following are true:

1. `roboticus.ai/install.sh` installs the Go-based runtime.
2. `roboticus.ai/install.ps1` installs the Go-based runtime.
3. `roboticus update all` upgrades to the Go-based runtime.
4. `roboticus upgrade all` upgrades to the Go-based runtime.
5. Release checksum validation still works.
6. Registry sync still works.
7. Changelog and release metadata are sourced from the Go release line.

## Required Go-Side Release Outputs

Roboticus releases must publish enough metadata for the site to remain
authoritative.

Required outputs:

- release version
- release date
- per-platform artifacts
- canonical checksum manifest (`SHA256SUMS.txt`)
- release notes
- changelog entry
- registry files or a Go-native equivalent canonical source
- architecture/docs source for site indexing
- metrics source or generation inputs

The source repo must also publish or expose:

- one canonical release event that the site can subscribe to
- a release object whose assets match the installer contract
- a source tree layout that matches what the site sync workflow expects, or a
  site sync workflow that gracefully handles absent optional trees

## Required Site Changes For Cutover

The site sync layer should be generalized so the source runtime repo is a
configuration input instead of being hard-coded to Roboticus.

Required changes:

1. parameterize source repository in release-sync automation
2. parameterize source checkout path assumptions in sync scripts
3. allow release-sync to consume Roboticus release assets
4. preserve public filenames and product labels where compatibility requires it
5. ensure metrics extraction supports Go source layout
6. ensure changelog sync can read Roboticus changelog format
7. ensure architecture/docs sync uses Roboticus docs as canonical after cutover
8. sync public installer scripts directly from the tagged source repo instead
   of maintaining independent copies
9. remove any site copy that still assumes `checksums.txt` when the source
   release contract is `SHA256SUMS.txt`
10. stop advertising `go install github.com/robot-accomplice/roboticus@latest`
    as a supported fallback until the module path contract matches that command

## Distribution Compatibility Decision

Because the public command contract must remain `roboticus update/upgrade all`,
the cutover release needs a compatibility strategy.

Accepted strategy:

- continue distributing a `roboticus` binary for public/operator use
- allow `roboticus` to remain the repository and internal implementation name
- preserve `roboticus`-compatible artifact and installer behavior until a later
  intentional brand/distribution migration

This is the lowest-risk path because it preserves historical installation and
upgrade expectations.

## Release Sync Gate For Cutover

The first Roboticus-backed release must not be announced until:

1. the site sync workflow succeeds from the Roboticus release source
2. the site deploy succeeds
3. installer scripts point at the new Go-backed artifacts
4. release downloads and checksums validate
5. `roboticus update all` and `roboticus upgrade all` succeed against the new
   release line

## Recommended Implementation Sequence

1. Add Roboticus release outputs needed by the site:
   - canonical checksums
   - stable artifact naming
   - release notes/changelog discipline
2. Add CLI compatibility for `roboticus update/upgrade all`.
3. Parameterize `roboticus-site` release sync to support Roboticus as source.
4. Dry-run the sync against a test Roboticus tag.
5. Validate install/upgrade end to end.
6. Cut the production release only after all of the above are green.

## Control Plane Rule

The site must treat the source repo's tagged installer scripts and published
release assets as the only authoritative install contract. If the site carries a
different script body, checksum filename, or archive naming assumption, the
control plane is already broken even before an operator reports it.

## Migration Risk To Avoid

The biggest cutover risk is treating the repository migration as separate from
distribution.

That would break operators in one of these ways:

- install scripts still fetch Rust-era artifacts
- site changelog/docs point at the wrong source repo
- checksums no longer match published archives
- `roboticus update all` upgrades content but not the binary
- `roboticus upgrade all` disappears or drifts

That risk is unacceptable for the transition release and must be treated as
release-blocking.
