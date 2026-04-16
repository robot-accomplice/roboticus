package updatecmd

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestReserveOldSidecar_PrefersCanonical is the baseline: with nothing
// at `<exe>.old`, reservation returns exactly `<exe>.old`.
func TestReserveOldSidecar_PrefersCanonical(t *testing.T) {
	tmp := t.TempDir()
	exe := filepath.Join(tmp, "roboticus")
	if err := os.WriteFile(exe, []byte("exe"), 0o755); err != nil {
		t.Fatalf("seed exe: %v", err)
	}
	got, err := reserveOldSidecar(exe)
	if err != nil {
		t.Fatalf("reserve: %v", err)
	}
	if got != exe+".old" {
		t.Fatalf("want canonical %s; got %s", exe+".old", got)
	}
}

// TestReserveOldSidecar_RemovesStaleCanonical: a prior update left a
// `.old` sidecar but it's a regular file we can still remove. Reservation
// should remove it and return the canonical name — this is the "happy
// path for the repeat-updater" case.
func TestReserveOldSidecar_RemovesStaleCanonical(t *testing.T) {
	tmp := t.TempDir()
	exe := filepath.Join(tmp, "roboticus")
	if err := os.WriteFile(exe, []byte("exe"), 0o755); err != nil {
		t.Fatalf("seed exe: %v", err)
	}
	// Seed a stale .old from a previous update.
	staleOld := exe + ".old"
	if err := os.WriteFile(staleOld, []byte("stale"), 0o644); err != nil {
		t.Fatalf("seed stale: %v", err)
	}

	got, err := reserveOldSidecar(exe)
	if err != nil {
		t.Fatalf("reserve: %v", err)
	}
	if got != staleOld {
		t.Fatalf("expected canonical after removing stale; got %s", got)
	}
	if _, err := os.Stat(staleOld); !os.IsNotExist(err) {
		t.Fatalf("stale file should have been removed; stat err=%v", err)
	}
}

// TestReserveOldSidecar_FallsBackWhenCanonicalLocked is the v1.0.6 P1-F
// regression: a prior `.old` sidecar exists AND cannot be removed
// (simulating a Windows handle-lock, Defender scan, or "Explorer has it
// open in Properties" state). Pre-fix, the Windows updater's
// best-effort os.Remove would quietly fail and the subsequent
// os.Rename(exe, exe+".old") would hit "file already exists" and block
// every future update forever. v1.0.6 P1-F required the timestamped
// fallback the comment had always promised.
//
// We simulate an unremovable canonical by wrapping the temp dir in a
// RO parent directory — removing a file requires write permission on
// its PARENT dir on POSIX, so chmod the tmp to 0o555 after seeding.
func TestReserveOldSidecar_FallsBackWhenCanonicalLocked(t *testing.T) {
	if runtime.GOOS == "windows" {
		// Windows permission model is different; the chmod trick
		// doesn't reliably make a remove fail. On Windows CI this
		// test would need a real handle-lock; skip here. The
		// behavior is covered symbolically by the path-comparison
		// assertions below, which only care about the return value.
		t.Skip("POSIX chmod-based unremovable-file simulation; Windows needs a real handle lock")
	}
	parent := t.TempDir()
	exeDir := filepath.Join(parent, "install")
	if err := os.Mkdir(exeDir, 0o755); err != nil {
		t.Fatalf("mkdir exeDir: %v", err)
	}
	exe := filepath.Join(exeDir, "roboticus")
	if err := os.WriteFile(exe, []byte("exe"), 0o755); err != nil {
		t.Fatalf("seed exe: %v", err)
	}
	stuckOld := exe + ".old"
	if err := os.WriteFile(stuckOld, []byte("stuck"), 0o644); err != nil {
		t.Fatalf("seed stuck old: %v", err)
	}
	// Make exeDir read-only so os.Remove(stuckOld) fails.
	if err := os.Chmod(exeDir, 0o555); err != nil {
		t.Fatalf("chmod dir RO: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(exeDir, 0o755) }) // restore for cleanup

	got, err := reserveOldSidecar(exe)
	if err != nil {
		t.Fatalf("reserve under RO dir: %v", err)
	}

	// Reservation MUST NOT return the canonical name here — the
	// locked file is still present and a subsequent os.Rename would
	// fail with "file exists."
	if got == stuckOld {
		t.Fatalf("expected timestamped fallback when canonical is unremovable; got canonical %s", got)
	}
	// The fallback should have the expected shape: `<exe>.old-<stamp>`.
	if !strings.HasPrefix(got, exe+".old-") {
		t.Fatalf("fallback shape wrong: want prefix %s; got %s", exe+".old-", got)
	}
	// The stuck file should still be intact (we tried to remove and
	// failed, but we didn't break the filesystem either).
	if _, err := os.Stat(stuckOld); err != nil {
		t.Fatalf("stuck file should still exist; got err=%v", err)
	}
}

// TestReserveOldSidecar_IsCallableRepeatedly confirms two back-to-back
// reservations each get usable (distinct, non-colliding) paths when the
// canonical is unremovable. The second reservation shouldn't error just
// because the first one produced a timestamped candidate — they must
// each have microsecond-level distinct timestamps.
func TestReserveOldSidecar_IsCallableRepeatedly(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX chmod-based simulation; see TestReserveOldSidecar_FallsBackWhenCanonicalLocked")
	}
	parent := t.TempDir()
	exeDir := filepath.Join(parent, "install")
	if err := os.Mkdir(exeDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	exe := filepath.Join(exeDir, "roboticus")
	if err := os.WriteFile(exe, []byte("x"), 0o755); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := os.WriteFile(exe+".old", []byte("stuck"), 0o644); err != nil {
		t.Fatalf("seed stuck: %v", err)
	}
	if err := os.Chmod(exeDir, 0o555); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(exeDir, 0o755) })

	got1, err := reserveOldSidecar(exe)
	if err != nil {
		t.Fatalf("first reserve: %v", err)
	}
	// Microsecond-precision timestamp means back-to-back calls are
	// essentially guaranteed distinct. If clock regressions ever make
	// them collide, the second call errors cleanly — either outcome
	// is acceptable (call is safe to retry). What we refuse is silent
	// path reuse.
	got2, err2 := reserveOldSidecar(exe)
	if err2 != nil {
		// Acceptable: explicit collision error.
		if !strings.Contains(err2.Error(), "collision") {
			t.Fatalf("unexpected error from repeat reserve: %v", err2)
		}
		return
	}
	if got1 == got2 {
		t.Fatalf("back-to-back reservations produced identical paths: %s", got1)
	}
}
