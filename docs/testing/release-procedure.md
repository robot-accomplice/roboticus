# Release Procedure

This is the canonical Roboticus release ceremony.

Review this document every time a release is prepared. Do not improvise the
sequence. Do not tag early. Do not create a GitHub release before `main` is
merged and verified.

## Preconditions

Before the ceremony starts, all of the following must already be true on the
release branch:

1. architecture changes are complete
2. documentation changes are complete
3. code changes are complete
4. unit, integration, e2e, smoke, soak, and behavioral soak gates are green
5. code audit is complete
6. deployment / upgrade audit is complete
7. release notes, architecture docs, regression matrix, and site changelog are updated

If any of those are still open, stop. The ceremony has not started yet.

## Required Order

Always use this order:

1. Create or update the release branch PR into `develop`.
2. Wait for PR CI to go green.
3. Merge the release branch into `develop`.
4. Audit `develop` after merge.
5. Wait for `develop` CI to go green.
6. Create the PR from `develop` into `main`.
7. Wait for that PR CI to go green.
8. Merge `develop` into `main`.
9. Only then create/push the release tag.
10. Only then create/publish the GitHub release object.
11. Monitor release execution:
    - release artifact builds
    - site synchronization
    - checksums / fingerprinting
    - installer / upgrade behavior

## Branch / PR Rules

- The release branch PR goes to `develop`, not `main`.
- The `develop -> main` PR is created only after `develop` is merged, audited,
  and green.
- Do not create duplicate PRs for the same release path.
- Do not retarget release PRs casually; if retargeting is required, verify the
  new base immediately.

## Tagging Rules

- Tagging is the very last source-control mutation in the flow.
- Never push the release tag before `main` contains the final merged release commit.
- Never create a GitHub release draft before the tag exists on the merged `main`
  commit.

## Monitoring Checklist

After tagging and release creation, actively verify:

1. release workflow started on the expected tag
2. every expected artifact was built
3. checksums were generated and attached correctly
4. site synchronization completed against the intended release metadata
5. the published binary reports the intended version, not `dev`
6. install / upgrade paths still work against the published release
7. if the initial tag push did not enqueue release execution, the canonical
   release workflow can be manually dispatched against the existing tag without
   changing release content or creating a new tag
8. rerun publication derives release name, tag, prerelease gating, asset upload,
   and site-sync behavior from the explicit requested tag, not implicit branch
   or ref-name context
9. active release notifications and publication steps use explicit first-party
   CLI/API calls where critical control flow is involved; do not depend on
   third-party action context for tag authority or dispatch semantics
10. security/vulnerability tooling used in CI is pinned to an explicit version;
    do not float `latest` in release-critical workflow paths
11. the release workflow performs a post-publication self-evaluation against the
    live release object and emits a success/failure report instead of assuming a
    green publish step proves end-to-end release correctness

## Failure Rules

- If CI is red, stop and fix the underlying issue. Do not merge through it.
- If `develop` audit fails, stop and fix `develop` before opening `main` PR.
- If release artifacts, fingerprinting, or site sync are wrong, the release is
  not complete even if merges and tags succeeded.
- If the self-evaluation report says the published release is incomplete or
  inconsistent, the release is not complete even if all upstream jobs were
  green.

## Operator Reminder

The point of this procedure is release truth:

- the right code must land in the right branches in the right order
- the published version must match the actual binaries
- the published artifacts must be installable
- the site and GitHub release metadata must describe the same release
