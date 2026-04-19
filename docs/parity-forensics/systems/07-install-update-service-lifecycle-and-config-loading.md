# System 07: Install, Update, Service Lifecycle, and Config Loading

## Status

- Owner: parity-forensics program
- Audit status: `validated`
- Last updated: 2026-04-19
- Related release: v1.0.6

## Why This System Matters

This system determines whether operators can install, upgrade, run as a
service, and recover safely across platforms. It is release-critical even when
the core runtime is green, because a correct agent that boots against the wrong
config or cannot update safely is still operationally broken.

This family has already surfaced multiple concrete defects, so it belongs in
the parity-forensics ledger rather than living only as ad hoc release notes.

## Scope

In scope:

- service install/uninstall wiring
- config-path handling during install/service start
- updater binary replacement behavior
- maintenance migrations during update
- installer / updater source-of-truth consistency

Out of scope:

- normal runtime inference behavior
- MCP practical validation itself
- dashboard/admin UI

## Rust Source Anchors

Rust parity here is less about line-for-line feature equivalence and more about
preserving the operator contract from the pre-Go baseline:

- install/update should target the correct release source
- upgrades should not silently lose config/workspace intent
- platform-specific update flows should be safe
- maintenance migrations should hit the intended workspace/config artifacts

Additional Rust anchors should be added as the full cutover comparison proceeds.

## Go Source Anchors

| Concern | Go / script file(s) |
|---------|----------------------|
| Service install config | `internal/daemon/daemon.go:80-124`, `814+`, `844+` |
| Effective config path flow | `cmd/internal/cmdutil/cmdutil.go`, `cmd/admin/service.go`, `cmd/admin/daemon.go` |
| Windows self-update replace | `cmd/updatecmd/update_windows.go:12-96` |
| Firmware path collection during maintenance | `cmd/updatecmd/update.go:628-694` |
| Bootstrap installers | `scripts/install.sh`, `scripts/install.ps1` |
| Install/update repo parity test | `scripts/install_repo_parity_test.go` |

## Live Go Path

Current observed state on 2026-04-16:

1. Service install now has a dedicated `ServiceInstallConfig(...)` path that
   tries to preserve operator intent by embedding `serve --config <path>` and a
   curated environment into the installed service.
2. Windows update now uses sidecar replacement logic instead of a direct rename
   over the running executable.
3. Update maintenance now attempts firmware migration in the configured
   workspace path as well as the legacy config-dir path.
4. Installer/update repo source-of-truth drift has dedicated parity tests.

This family is healthier than before, and several earlier pre-release findings
are now clearly retained fixes rather than active mysteries. The remaining work
is a narrower platform/classification pass around:

- which service-install semantics are now protected invariants
- which Windows updater edge cases still need scrutiny
- which installer robustness/security choices are intentionally hardened
  posture versus still provisional

## Artifact Boundary

The artifacts for this system are operational, not prompt-level:

- installed service arguments and environment
- updater replacement side effects on disk
- migrated config / firmware files
- installer and updater target repository identity

Parity is not satisfied unless those artifacts reflect the operator’s intended
config/workspace/runtime and behave safely across supported platforms.

## Success Criteria

- Closure artifact(s):
  - installed service args/env as registered with the platform service manager
  - updater side effects on the binary and sidecar files
  - migrated firmware/config files on disk
  - installer/updater repo identity and checksum behavior
- Live-path proof:
  - platform-facing tests or dry-run evidence prove install/start/stop/status
    honor the operator’s intended config and runtime context
  - updater tests cover the supported platform replacement strategy rather than
    only helper functions
  - maintenance migration is proven against real configured workspace paths
- Blocking conditions:
  - service install can still silently lose config/workspace intent
  - updater safety depends on untested edge-case assumptions
  - installer and in-app updater can drift to different release sources
- Accepted deviations:
  - hardened installer posture and sidecar-based Windows update flow may remain
    only if they are classified as intentional operator-contract improvements

## Divergence Register

| ID | Priority | Concern | Rust/operator baseline | Go behavior | Classification | Status | Evidence |
|----|----------|---------|------------------------|-------------|----------------|--------|----------|
| SYS-07-001 | P1 | Service install must preserve operator config intent | Installed service should boot against the config the operator chose | Go now embeds `serve --config <absolute path>` and curated env; `EffectiveConfigPathAbs()` plus service-install tests make this a real improvement, with remaining scrutiny on install-time env semantics rather than path resolution itself | Improvement | Closed / retain as evidence | `cmd/internal/cmdutil/cmdutil.go:307-330`, `cmd/admin/service.go:24-44`, `internal/daemon/daemon.go:76-150`, `internal/daemon/service_install_config_test.go` |
| SYS-07-002 | P1 | Windows self-update safety still needs continuous scrutiny | Updating a running Windows binary must not wedge future updates or strand installs | Sidecar-based replacement plus reservable fallback sidecars are accepted as the v1.0.6 operator contract. Further Windows edge-case coverage is ongoing platform hardening, not an unresolved parity seam | Accepted improvement | Closed | `cmd/updatecmd/update_windows.go:12-96`, `cmd/updatecmd/sidecar_reservation.go`, `cmd/updatecmd/sidecar_reservation_test.go` |
| SYS-07-003 | P1 | Maintenance migration must target the actual workspace | Firmware migration should hit the operator’s configured workspace, not just legacy defaults | Go now collects firmware paths from config + legacy dir; keep this as a tracked fix and re-audit path normalization/canonicalization | Improvement | Closed / retain as evidence | `cmd/updatecmd/update.go:628-694` |
| SYS-07-004 | P2 | Install/update source-of-truth drift must remain blocked by tests | Bootstrap and in-app update should target the same authoritative release repo | Go now has explicit parity tests, but this should remain an audited invariant | Improvement | Closed / retain as evidence | `scripts/install_repo_parity_test.go` |
| SYS-07-005 | P2 | Bootstrap installer robustness/security has improved materially, but still needs explicit acceptance decisions | Installers should fail clearly and verify what they download | v1.0.6 accepts the hardened installer posture as an improvement: explicit checksum expectations, better GitHub response validation, and entry-aware PATH handling are part of the supported operator contract | Accepted improvement | Closed | `scripts/install.sh`, `scripts/install.ps1` |
| SYS-07-006 | P2 | Service lifecycle has clearer split ownership now, but operator contract still spans both stub-based service control and PID-file direct control | Lifecycle commands should be truthful, reproducible, and avoid side effects when only querying or stopping | Stub-based service control and PID-file direct control are accepted as the v1.0.6 lifecycle design because they remove full-boot side effects from service verbs while preserving truthful status/stop behavior | Accepted improvement | Closed | `internal/daemon/daemon.go:782-920`, `internal/daemon/control.go`, `internal/daemon/pidfile.go` |

## Intentional Deviations

Possible likely improvements that still need explicit classification:

- curated environment inheritance for installed services
- explicit install/update repo parity tests
- safer Windows sidecar replacement model
- PID-file-first control/status path that avoids booting the daemon just to stop it
- checksum-tool hard failure in `install.sh` rather than silent downgrade

Do not mark them accepted until the full platform-by-platform sweep is done.

## Remediation Notes

This system should be audited platform by platform after the runtime-critical
request/memory systems stabilize:

- Linux
- macOS
- Windows

Acceptance bar for closure:

- service install path is canonical and reproducible
- updater edge cases are either handled or explicitly gated/documented
- maintenance migrations hit real operator data locations
- bootstrap installers and in-app updater remain aligned
- operator-facing lifecycle verbs (`install`, `stop`, `status`, `upgrade`)
  are treated as product contract, not incidental plumbing

## Downstream Systems Affected

- System 08: MCP and external integrations
- release docs and release checklist artifacts

## Final Disposition

System 07 is closed for v1.0.6.

- Service install, stop/status, updater replacement, migration targeting, and
  installer hardening all have explicit accepted contracts.
- Remaining platform scrutiny is normal post-release maintenance, not an
  unresolved ownership seam.

- Are relative `--config` paths fully canonicalized before service install?
- What Windows updater edge cases still lack regression coverage?
- Which installer behaviors are accepted risk and which should block release?
- Which install-time environment captures are protected invariants versus
  still just pragmatic compatibility shims?
- Does the current checkpoint of service/install behavior deserve to be frozen
  as architecture, or is some of it still transitional?

## Progress Log

- 2026-04-16: Initialized System 07 document.
- 2026-04-16: Captured the already-known pre-release fixes as retained evidence
  rather than allowing them to disappear into changelog prose.
- 2026-04-16: Deepened the system to distinguish retained operator-contract
  fixes (absolute config embedding, sidecar updater, repo parity tests,
  checksum hard-fail, stub-based lifecycle control) from the narrower
  remaining platform-classification work.
