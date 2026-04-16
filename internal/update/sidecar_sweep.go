// Package update is the neutral home for update-related utilities that
// need to be called from BOTH cmd/updatecmd (the user-facing `roboticus
// upgrade` command) and internal/daemon (boot-time sidecar sweep).
// Putting the utility in cmd/updatecmd would require internal/daemon to
// import cmd/*, which inverts the standard Go project layout (cmd/
// should depend on internal/, not the other way around).
package update

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
)

// sidecarSweepMinAge is how long a <exe>.old* sidecar must exist before
// a sweep removes it. The buffer exists so an actively-running updater
// can't have its own .old deleted mid-update by a separate invocation.
// 24h covers the longest plausible manual rollback window an operator
// might want — anything older is stale by any definition.
const sidecarSweepMinAge = 24 * time.Hour

// SweepStaleUpdateSidecars removes `<runningExe>.old` and
// `<runningExe>.old-<timestamp>` files older than sidecarSweepMinAge.
//
// Why this exists: the Windows self-update path (update_windows.go)
// schedules sidecars for delete-on-reboot via MoveFileExW with
// MOVEFILE_DELAY_UNTIL_REBOOT. That call requires
// SE_CREATE_PAGEFILE_NAME privilege, which standard user accounts
// usually have — BUT group-policy-locked corporate machines frequently
// revoke it. When the reboot-scheduled delete silently fails, the
// sidecar persists forever. The v1.0.6 timestamped-fallback in
// reserveOldSidecar means updates keep SUCCEEDING even when every
// previous update's sidecar is still on disk, but the install dir
// accumulates dead binaries — a disk-space leak and a source of
// operator confusion ("which one is real?").
//
// The sweep is called once at daemon boot as a best-effort cleanup.
// Failures (permission denied, file still locked) are logged at DEBUG
// and do not block boot — the next sweep will try again. Platform-
// agnostic by design: Unix updates don't produce .old sidecars, so
// the glob returns empty and the function is a harmless no-op.
//
// Returns the number of files removed (useful for tests and metrics);
// errors from individual removes are logged, not returned.
func SweepStaleUpdateSidecars(execPath string) int {
	// Be defensive against symlinks — resolve to the real path so the
	// glob targets the dir the bytes actually live in.
	if resolved, err := filepath.EvalSymlinks(execPath); err == nil {
		execPath = resolved
	}

	// Two glob patterns: canonical and timestamped fallback.
	candidates := map[string]struct{}{}
	for _, pattern := range []string{execPath + ".old", execPath + ".old-*"} {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			// Glob syntax error would be a bug, not a runtime concern —
			// log and move on.
			log.Debug().Err(err).Str("pattern", pattern).Msg("sidecar sweep: glob failed")
			continue
		}
		for _, m := range matches {
			candidates[m] = struct{}{}
		}
	}

	if len(candidates) == 0 {
		return 0
	}

	cutoff := time.Now().Add(-sidecarSweepMinAge)
	removed := 0
	for path := range candidates {
		info, err := os.Stat(path)
		if err != nil {
			log.Debug().Err(err).Str("path", path).Msg("sidecar sweep: stat failed")
			continue
		}
		if info.IsDir() {
			// Don't touch dirs — the glob shouldn't match any, but be
			// paranoid in case an operator manually created an `.old`
			// directory for their own bookkeeping.
			continue
		}
		if info.ModTime().After(cutoff) {
			// Too fresh — could be an in-flight rollback the operator
			// hasn't confirmed yet.
			continue
		}
		if err := os.Remove(path); err != nil {
			log.Debug().Err(err).Str("path", path).Msg("sidecar sweep: remove failed (file may still be handle-locked)")
			continue
		}
		log.Info().Str("path", path).Dur("age", time.Since(info.ModTime())).Msg("sidecar sweep: removed stale update sidecar")
		removed++
	}
	return removed
}

// SweepStaleUpdateSidecarsAuto looks up the current executable path and
// invokes SweepStaleUpdateSidecars against it. Wraps os.Executable and
// logs rather than propagating — caller is boot code that should never
// fail on a best-effort cleanup.
func SweepStaleUpdateSidecarsAuto() {
	execPath, err := os.Executable()
	if err != nil {
		log.Debug().Err(err).Msg("sidecar sweep: os.Executable failed; skipping")
		return
	}
	// On Linux, os.Executable can return `/proc/self/exe` — resolve
	// that to the real path so the glob matches installed binaries.
	if strings.HasPrefix(execPath, "/proc/") {
		if resolved, err := filepath.EvalSymlinks(execPath); err == nil {
			execPath = resolved
		}
	}
	SweepStaleUpdateSidecars(execPath)
}
