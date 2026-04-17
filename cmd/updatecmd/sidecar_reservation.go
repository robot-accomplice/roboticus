package updatecmd

import (
	"fmt"
	"os"
	"time"
)

// reserveOldSidecar returns a path we can rename execPath to, atomically
// swapping in the fresh binary afterwards. The logic is platform-agnostic
// (pure os/time API calls) even though the ONLY production caller is the
// Windows self-update path in update_windows.go — extracting it here lets
// the fallback behavior be unit-tested on non-Windows CI hosts.
//
// Preference order:
//
//  1. `<execPath>.old` — the canonical name. If nothing currently
//     occupies it, or if we can successfully remove an existing stale
//     file there, this is what we return.
//
//  2. `<execPath>.old-<compact timestamp>` — the fallback for when
//     `.old` is occupied and can't be removed (another updater holding
//     it, Defender scanning it, user opened Explorer Properties). This
//     is what the v1.0.6 P1-F audit finding required: the comment in
//     update_windows.go had always promised this fallback but the
//     original implementation never actually had one — a lingering
//     `.old` would permanently wedge all future updates for that
//     operator.
//
// Returns an error only if BOTH the canonical and a freshly-timestamped
// fallback are somehow unusable — a state that would mean the exe's
// directory is fundamentally unwritable and the install would fail
// anyway.
func reserveOldSidecar(execPath string) (string, error) {
	canonical := execPath + ".old"

	// Path (1): canonical name is available if:
	//   (a) it doesn't exist, or
	//   (b) it exists but we can remove it.
	// Both cases leave the path ready for a subsequent os.Rename.
	if _, statErr := os.Stat(canonical); os.IsNotExist(statErr) {
		return canonical, nil
	} else if statErr == nil {
		if removeErr := os.Remove(canonical); removeErr == nil {
			return canonical, nil
		}
		// Remove failed (file locked, permission denied, antivirus
		// holding handle, etc.). Fall through to the timestamped
		// fallback rather than error out.
	}

	// Path (2): timestamped fallback. Compact numeric format keeps
	// the path valid on every filesystem (NTFS in particular forbids
	// colons, which rules out time.RFC3339). Microsecond precision
	// makes collisions essentially impossible under any real clock.
	stamp := time.Now().UTC().Format("20060102-150405.000000")
	fallback := execPath + ".old-" + stamp
	if _, statErr := os.Stat(fallback); statErr == nil {
		// Collision at a microsecond-precision timestamp is astronomically
		// unlikely under a sane clock — but if it happens we'd rather
		// error than silently overwrite.
		return "", fmt.Errorf("timestamped sidecar collision at %s (clock jump?)", fallback)
	}
	return fallback, nil
}
