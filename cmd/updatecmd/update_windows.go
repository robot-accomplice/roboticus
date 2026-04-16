//go:build windows

package updatecmd

import (
	"fmt"
	"os"
	"syscall"
	"unsafe"
)

// replaceRunningBinary installs stagePath at execPath on Windows.
//
// Why this needs to be more complex than Unix: a running .exe on Windows
// holds an exclusive write lock via its open image handle, so a direct
// os.Rename(stagePath, execPath) will fail with ERROR_SHARING_VIOLATION
// ("The process cannot access the file because it is being used by another
// process.") for the currently running updater.
//
// What Windows *does* allow: renaming the running .exe itself. The lock is
// keyed to the open handle, not to the directory entry — once the entry
// moves, the open handle keeps pointing at the same inode, and a new entry
// at the original name becomes legal. So the standard dance is:
//
//   1. Pick a sidecar path for the outgoing exe — prefer `<exe>.old`,
//      but if a previous update left that entry lingering and we can't
//      remove it (another updater running, Defender holding a handle,
//      user has it open in Explorer Properties), fall back to a
//      timestamped name so this update never wedges.
//   2. Rename exe → sidecar  (allowed — we only touch the dir entry)
//   3. Rename stage → exe    (the original name is now free)
//   4. Best-effort: schedule the sidecar for delete-on-reboot via
//      MoveFileExW with MOVEFILE_DELAY_UNTIL_REBOOT. If this fails
//      it's not fatal; the sidecar is harmless stale bytes the
//      operator can delete by hand.
//
// Rollback discipline: if step 3 fails, we try to rename sidecar back
// into exe so the installed version continues to work. If the rollback
// itself fails the user is in a bad state, but that's the same state a
// crashing install would produce — we log and surface a loud error.
func replaceRunningBinary(stagePath, execPath string) error {
	sidecarPath, err := reserveOldSidecar(execPath)
	if err != nil {
		return fmt.Errorf("windows self-update: cannot reserve sidecar path for running exe: %w", err)
	}

	// Step 2: move the running exe aside.
	if err := os.Rename(execPath, sidecarPath); err != nil {
		return fmt.Errorf("windows self-update: cannot move running exe aside to %s: %w", sidecarPath, err)
	}

	// Step 3: install the new binary at the original path.
	if err := os.Rename(stagePath, execPath); err != nil {
		// Roll back so the operator is left with a working installation.
		if rbErr := os.Rename(sidecarPath, execPath); rbErr != nil {
			return fmt.Errorf("windows self-update: failed to install new binary (%v) AND rollback failed (%v) — manual recovery required (sidecar at %s)", err, rbErr, sidecarPath)
		}
		return fmt.Errorf("windows self-update: failed to install new binary (rolled back): %w", err)
	}

	// Step 4: best-effort schedule delete-on-reboot for the old binary.
	if err := scheduleDeleteOnReboot(sidecarPath); err != nil {
		// Non-fatal. Log-equivalent: stderr print so operators notice.
		fmt.Fprintf(os.Stderr, "windows self-update: old binary at %s will persist until manually deleted (MoveFileExW failed: %v)\n", sidecarPath, err)
	}

	return nil
}

// reserveOldSidecar moved to sidecar_reservation.go so its pure-FS
// fallback logic can be unit-tested on non-Windows hosts too.

// scheduleDeleteOnReboot calls Windows' MoveFileExW with
// MOVEFILE_DELAY_UNTIL_REBOOT so the given path is removed on next boot.
// Requires SE_CREATE_PAGEFILE_NAME privilege (most user accounts have this
// for files the user owns).
func scheduleDeleteOnReboot(path string) error {
	const MOVEFILE_DELAY_UNTIL_REBOOT = 0x4

	utf16Path, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return err
	}

	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	moveFileEx := kernel32.NewProc("MoveFileExW")
	ret, _, callErr := moveFileEx.Call(
		uintptr(unsafe.Pointer(utf16Path)),
		0, // lpNewFileName = NULL means "remove on reboot"
		uintptr(MOVEFILE_DELAY_UNTIL_REBOOT),
	)
	if ret == 0 {
		return callErr
	}
	return nil
}
