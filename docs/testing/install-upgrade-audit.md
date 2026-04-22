# Install / Upgrade Audit

## Local Incident: 2026-04-19

### Observed State

- Installed binary path: `/Users/jmachen/.local/bin/roboticus`
- Installed binary reported `roboticus dev`
- Config path existed: `/Users/jmachen/.roboticus/roboticus.toml`
- Provider config existed: `/Users/jmachen/.roboticus/providers.toml`
- Skills directory existed: `/Users/jmachen/.roboticus/skills`
- Updater state file existed: `/Users/jmachen/.roboticus/update_state.json`
- Existing updater state was incomplete/stale for the current install and needed reconciliation, not bootstrap-from-zero treatment

### User-Visible Failure

`roboticus upgrade all` completed the binary replacement step, then failed in the post-binary provider refresh step:

- binary download succeeded
- binary checksum verification succeeded
- binary replacement succeeded
- provider pack refresh failed on registry checksum mismatch
- command exited non-zero even though the binary update had already succeeded

This was the wrong boundary. Default upgrade should not present a successful binary swap as a failed upgrade just because registry-published provider content is stale or mismatched.

## Root Cause

The default `upgrade all` path still allowed provider/skills refresh to abort the command when:

- the local install had updater bookkeeping that could not be trusted as authoritative for the current machine state
- the orchestration path decided provider/skills content should be refreshed
- the registry manifest SHA for provider content was stale or incorrect

That meant a secondary content-refresh failure could poison the primary upgrade result.

## Code Fix

`runUpdateAll(...)` now treats provider/skills refresh as:

- hard-fail only when the user explicitly passes `--refresh-config`
- warning-only in the default upgrade path
- startup reconciliation for updater state is automatic: missing, legacy-named, or incomplete state is repaired from the local install before upgrade decisions are made

This preserves the correct contract:

- binary upgrade is authoritative
- provider/skills refresh is opportunistic by default
- explicit config refresh remains strict and checksum-verified
- manual state repair is fallback-only for exceptional/operator-directed recovery

## Local Remediation Performed

1. Verified local install state:
   - binary at `/Users/jmachen/.local/bin/roboticus`
   - config present
   - provider config present
   - updater state file present at `update_state.json`
   - stored updater metadata did not fully describe the current install
2. Replaced the installed binary with the current source build:
   - `go build -o /Users/jmachen/.local/bin/roboticus .`
3. Verified the installed binary still runs:
   - `/Users/jmachen/.local/bin/roboticus version`
4. Added orchestration + reconciliation regression coverage so the default upgrade path cannot fail this way again.

## Regression Coverage Added

- default `runUpdateAll(...)` does not fail when provider refresh encounters a stale checksum and the user did not request `--refresh-config`
- explicit `--refresh-config` still fails hard on checksum mismatch
- reconciliation repairs incomplete updater state from existing local provider/skill content
- legacy `update-state.json` naming is accepted as compatibility input, but the canonical state file remains `update_state.json`

## Audit Implications

The install / upgrade procedure must be audited on these exact axes:

1. **Binary replacement must be isolated from optional content refresh**
   - a successful binary upgrade must remain successful
   - provider/skills refresh must not poison the result unless explicitly requested

2. **Existing installs with missing updater state must still be safe**
   - missing or incomplete `update_state.json` cannot be treated as a fresh machine
   - local `providers.toml` presence matters more than updater bookkeeping

3. **Registry publication must be validated separately**
   - manifest SHA must match served provider/skill content
   - stale registry publication is a release-pipeline defect
   - it must not block default binary upgrades

4. **PATH visibility should be checked explicitly**
   - the installed binary may exist at `~/.local/bin/roboticus`
   - non-login shells may not expose that path
   - install verification should check both binary existence and command discovery

5. **Release-build version stamping must target the real runtime symbols**
   - release-shaped builds must stamp the CLI version into `roboticus/cmd/internal/cmdutil.Version`
   - startup banners must stamp the daemon version into `roboticus/internal/daemon.version`
   - CI, release packaging, and local release-helper build paths must all use the same symbols
   - a binary that still prints `dev` after a release-shaped build is a release blocker, not cosmetic drift

## Required Install / Upgrade Audit Checklist

Before calling install/upgrade healthy, verify:

1. `roboticus version` works from the intended installed location
2. `~/.roboticus/roboticus.toml` exists or the installer created it intentionally
3. `~/.roboticus/providers.toml` preservation works across upgrade
4. missing, legacy-named, or incomplete updater state does not cause a destructive or failing default upgrade
5. default `upgrade all` tolerates stale provider/skills registry content with warnings only
6. explicit `--refresh-config` still fails on checksum mismatch
7. release publication validates GitHub release assets and registry manifest coherence independently
8. release-shaped binaries report the expected non-`dev` version from both `roboticus version` and startup surfaces

## Follow-On Procedure Audit Work

This incident should be folded into the broader install/upgrade audit for `v1.0.7`:

- exercise existing-install upgrades with and without updater state
- exercise existing-install upgrades with incomplete updater state
- exercise existing-install upgrades with customized `providers.toml`
- exercise stale registry manifest scenarios
- verify binary replacement, provider preservation, skills preservation, and error narration independently
