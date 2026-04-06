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
