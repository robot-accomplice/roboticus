package update

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestSweepStaleUpdateSidecars_RemovesOldCanonical is the v1.0.6 self-
// audit P1-J regression. When the Windows updater's MoveFileExW
// delete-on-reboot fails (privilege revoked, reboot never happened),
// the `.old` sidecar persists. The sweep removes those on daemon
// startup so the install dir doesn't accumulate dead binaries.
func TestSweepStaleUpdateSidecars_RemovesOldCanonical(t *testing.T) {
	tmp := t.TempDir()
	exe := filepath.Join(tmp, "roboticus")
	if err := os.WriteFile(exe, []byte("exe"), 0o755); err != nil {
		t.Fatalf("seed exe: %v", err)
	}
	stale := exe + ".old"
	if err := os.WriteFile(stale, []byte("stale bytes"), 0o644); err != nil {
		t.Fatalf("seed stale: %v", err)
	}
	// Backdate the stale sidecar so it's older than sidecarSweepMinAge.
	ancient := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(stale, ancient, ancient); err != nil {
		t.Fatalf("backdate stale: %v", err)
	}

	removed := SweepStaleUpdateSidecars(exe)
	if removed != 1 {
		t.Fatalf("expected 1 removal; got %d", removed)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatalf("stale .old should be removed; stat err=%v", err)
	}
	// The exe itself must be untouched.
	if _, err := os.Stat(exe); err != nil {
		t.Fatalf("sweep should NOT touch exe; stat err=%v", err)
	}
}

// TestSweepStaleUpdateSidecars_RemovesTimestampedFallback covers the
// sibling case: the fallback `.old-<timestamp>` form from
// reserveOldSidecar should also be swept when stale.
func TestSweepStaleUpdateSidecars_RemovesTimestampedFallback(t *testing.T) {
	tmp := t.TempDir()
	exe := filepath.Join(tmp, "roboticus.exe")
	if err := os.WriteFile(exe, []byte("exe"), 0o755); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// Two timestamped sidecars from prior updates.
	stale1 := exe + ".old-20260410-120000.000000"
	stale2 := exe + ".old-20260412-083000.000000"
	for _, p := range []string{stale1, stale2} {
		if err := os.WriteFile(p, []byte("stale"), 0o644); err != nil {
			t.Fatalf("seed %s: %v", p, err)
		}
		ancient := time.Now().Add(-72 * time.Hour)
		if err := os.Chtimes(p, ancient, ancient); err != nil {
			t.Fatalf("backdate %s: %v", p, err)
		}
	}

	removed := SweepStaleUpdateSidecars(exe)
	if removed != 2 {
		t.Fatalf("expected 2 removals; got %d", removed)
	}
	for _, p := range []string{stale1, stale2} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Fatalf("%s should be removed; err=%v", p, err)
		}
	}
}

// TestSweepStaleUpdateSidecars_SpairsFreshSidecars protects the
// active-rollback window. A sidecar created less than 24h ago might
// be the outgoing exe from a rollback the operator has not yet
// confirmed is bad. The sweep must leave those alone.
func TestSweepStaleUpdateSidecars_SparesFreshSidecars(t *testing.T) {
	tmp := t.TempDir()
	exe := filepath.Join(tmp, "roboticus")
	if err := os.WriteFile(exe, []byte("exe"), 0o755); err != nil {
		t.Fatalf("seed exe: %v", err)
	}
	fresh := exe + ".old"
	if err := os.WriteFile(fresh, []byte("fresh"), 0o644); err != nil {
		t.Fatalf("seed fresh: %v", err)
	}
	// Leave mtime at ~now (well within the 24h window).

	removed := SweepStaleUpdateSidecars(exe)
	if removed != 0 {
		t.Fatalf("fresh sidecar should be spared; got %d removals", removed)
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Fatalf("fresh sidecar should still exist; err=%v", err)
	}
}

// TestSweepStaleUpdateSidecars_NoSidecarsIsNoop covers the Unix path
// (which never produces .old sidecars) and the clean-install path. The
// sweep must return 0 removals and must not touch the exe itself.
func TestSweepStaleUpdateSidecars_NoSidecarsIsNoop(t *testing.T) {
	tmp := t.TempDir()
	exe := filepath.Join(tmp, "roboticus")
	if err := os.WriteFile(exe, []byte("exe"), 0o755); err != nil {
		t.Fatalf("seed exe: %v", err)
	}
	removed := SweepStaleUpdateSidecars(exe)
	if removed != 0 {
		t.Fatalf("no-op sweep should return 0; got %d", removed)
	}
	if _, err := os.Stat(exe); err != nil {
		t.Fatalf("sweep must never touch the exe; err=%v", err)
	}
}

// TestSweepStaleUpdateSidecars_IgnoresDirectories guards against
// accidental removal of a directory an operator may have created as
// `<exe>.old` for their own bookkeeping. Unusual but not forbidden —
// os.Remove on a directory returns an error anyway, but filtering at
// the stat stage gives a clearer log trail.
func TestSweepStaleUpdateSidecars_IgnoresDirectories(t *testing.T) {
	tmp := t.TempDir()
	exe := filepath.Join(tmp, "roboticus")
	if err := os.WriteFile(exe, []byte("exe"), 0o755); err != nil {
		t.Fatalf("seed exe: %v", err)
	}
	oldDir := exe + ".old"
	if err := os.Mkdir(oldDir, 0o755); err != nil {
		t.Fatalf("mkdir oldDir: %v", err)
	}
	ancient := time.Now().Add(-72 * time.Hour)
	if err := os.Chtimes(oldDir, ancient, ancient); err != nil {
		t.Fatalf("backdate oldDir: %v", err)
	}

	removed := SweepStaleUpdateSidecars(exe)
	if removed != 0 {
		t.Fatalf("directory sidecar should be skipped; got %d removals", removed)
	}
	if _, err := os.Stat(oldDir); err != nil {
		t.Fatalf("directory should still exist; err=%v", err)
	}
}
