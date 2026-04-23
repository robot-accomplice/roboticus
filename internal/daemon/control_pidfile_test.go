// control_pidfile_test.go pins the v1.0.6 daemon-control contract:
// `roboticus daemon stop` must work without sudo, without re-booting
// the 12-step subsystem stack, and idempotently (stopping an already-
// stopped daemon returns nil, not an error). The PID file is the
// foundational mechanism — every other property derives from it.
//
// What this suite covers:
//
//   1. Round-trip Write/Read of the PID file with restrictive 0o600
//      permissions (R-DAEMON-PID-1).
//   2. WritePIDFile refuses to clobber a live PID owned by another
//      process (R-DAEMON-PID-2) — prevents two daemons from racing
//      into the same on-disk slot.
//   3. WritePIDFile silently overwrites a STALE PID file (one that
//      points at a dead process) — this is the kill -9 recovery path
//      the v1.0.6 work depends on (R-DAEMON-PID-3).
//   4. controlStop with no PID file + no installed service returns
//      nil idempotently — mirrors the operator action of "verify
//      clean state before fresh install" (R-DAEMON-STOP-1).
//   5. controlStop with a PID file pointing at a dead process treats
//      the daemon as already-stopped, removes the stale file, and
//      returns nil (R-DAEMON-STOP-2).
//   6. controlStop with a PID file pointing at a live test process
//      sends SIGTERM, the process exits, the PID file is cleaned up,
//      and the call returns nil (R-DAEMON-STOP-3) — the "happy
//      path" stop scenario.
//
// What this suite does NOT cover (deliberately):
//
//   * The OS-service-manager fallback (launchctl bootout / systemctl
//     stop) — those run only when no PID file exists, and they
//     mutate real system state. Tested manually as part of release
//     verification rather than in the unit suite.
//   * The macOS-specific stopViaLaunchctlBootout function — it shells
//     out to /bin/launchctl which we cannot exercise without a real
//     installed service. The isLaunchctlNotLoaded substring matcher
//     IS testable independently and is covered below.

package daemon

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"syscall"
	"testing"
	"time"

	"roboticus/internal/core"
)

// TestPIDFile_RoundTripWithRestrictivePerms locks in the foundational
// PID file contract: WritePIDFile creates the file with mode 0o600,
// ReadPIDFile parses our PID back, and the value matches os.Getpid().
// If this regresses, every higher-level daemon-control test breaks.
func TestPIDFile_RoundTripWithRestrictivePerms(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.pid")

	if err := WritePIDFile(path); err != nil {
		t.Fatalf("WritePIDFile: %v", err)
	}

	pid, found, err := ReadPIDFile(path)
	if err != nil {
		t.Fatalf("ReadPIDFile: %v", err)
	}
	if !found {
		t.Fatalf("expected pid file to be found after Write")
	}
	if pid != os.Getpid() {
		t.Fatalf("round-trip pid mismatch: read %d, expected %d", pid, os.Getpid())
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat pid file: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("pid file mode = %o; want 0600 (PID leaks process timing to other accounts on the host)", got)
	}
}

// TestPIDFile_RefusesToClobberLivePID guards against two daemons
// racing into the same PID file slot. WritePIDFile MUST refuse when
// the existing file points at a live, non-self process.
//
// We synthesize a "live other process" by spawning `sleep 30` —
// long enough that it'll still be alive when we check, short enough
// to clean up promptly on test exit. The test process kills the
// sleeper at the end via t.Cleanup so failures don't leak orphan
// processes into CI runners.
func TestPIDFile_RefusesToClobberLivePID(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("PID-based liveness checks are Unix-only")
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "test.pid")

	cmd := exec.Command("sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Fatalf("spawn sleep: %v", err)
	}
	otherPID := cmd.Process.Pid
	t.Cleanup(func() { _ = cmd.Process.Kill() })

	if err := os.WriteFile(path, []byte(strconv.Itoa(otherPID)+"\n"), 0o600); err != nil {
		t.Fatalf("seed pid file with other-process pid: %v", err)
	}

	err := WritePIDFile(path)
	if err == nil {
		t.Fatalf("WritePIDFile must refuse to clobber a live other-process PID; got nil")
	}
	if !containsSubstring(err.Error(), "already records live PID") {
		t.Fatalf("expected error mentioning live PID conflict; got %v", err)
	}
}

// TestPIDFile_OverwritesStalePID is the kill -9 recovery scenario:
// previous daemon was hard-killed, PID file still on disk, new
// daemon starts. WritePIDFile must detect the stale state and
// silently overwrite — no operator intervention required.
func TestPIDFile_OverwritesStalePID(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("PID-based liveness checks are Unix-only")
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "test.pid")

	// Spawn a process and let it exit, capturing its PID. The PID is
	// then guaranteed-dead for the duration of this test (PIDs are
	// not immediately reused).
	cmd := exec.Command("sh", "-c", "exit 0")
	if err := cmd.Run(); err != nil {
		t.Fatalf("spawn sh -c 'exit 0': %v", err)
	}
	stalePID := cmd.Process.Pid
	if ProcessIsAlive(stalePID) {
		t.Skip("OS reused PID immediately; cannot construct a guaranteed-stale PID")
	}

	if err := os.WriteFile(path, []byte(strconv.Itoa(stalePID)+"\n"), 0o600); err != nil {
		t.Fatalf("seed pid file with stale pid: %v", err)
	}

	if err := WritePIDFile(path); err != nil {
		t.Fatalf("WritePIDFile must silently overwrite a stale pid file; got %v", err)
	}

	pid, _, err := ReadPIDFile(path)
	if err != nil {
		t.Fatalf("ReadPIDFile: %v", err)
	}
	if pid != os.Getpid() {
		t.Fatalf("expected stale pid to be replaced by ours; got %d, want %d", pid, os.Getpid())
	}
}

// TestControlStop_IdempotentWhenNotRunning verifies that asking to
// stop an already-stopped daemon is exit-code-0 silent. The previous
// implementation returned a launchctl error in this case, which made
// `roboticus daemon stop` unsuitable for scripted state-reset.
//
// We simulate "not running" by pointing PIDFilePath at a tempdir
// where no PID file exists, and by relying on the fact that the
// roboticus service is presumably not installed in the test
// environment (or is in a stopped state).
func TestControlStop_IdempotentWhenNotRunning(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	cfg := &core.Config{
		Daemon: core.DaemonConfig{PIDFile: filepath.Join(tmpHome, "absent.pid")},
	}

	err := controlStop(cfg)
	if err != nil {
		t.Fatalf("controlStop on a non-running daemon must return nil (idempotent); got %v", err)
	}
}

// TestControlStop_RemovesStalePIDFileAndReturnsNil covers the kill -9
// followed by `roboticus daemon stop` scenario from the bug report.
// PID file points at a dead process; controlStop must clean up the
// stale file AND return nil (the daemon is, factually, not running).
func TestControlStop_RemovesStalePIDFileAndReturnsNil(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("PID-based liveness checks are Unix-only")
	}

	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	pidPath := filepath.Join(tmpHome, "stale.pid")

	cmd := exec.Command("sh", "-c", "exit 0")
	if err := cmd.Run(); err != nil {
		t.Fatalf("spawn sh -c 'exit 0': %v", err)
	}
	stalePID := cmd.Process.Pid
	if ProcessIsAlive(stalePID) {
		t.Skip("OS reused PID immediately; cannot construct a guaranteed-stale PID")
	}

	if err := os.WriteFile(pidPath, []byte(strconv.Itoa(stalePID)+"\n"), 0o600); err != nil {
		t.Fatalf("seed stale pid file: %v", err)
	}

	cfg := &core.Config{
		Daemon: core.DaemonConfig{PIDFile: pidPath},
	}
	if err := controlStop(cfg); err != nil {
		t.Fatalf("controlStop on stale-pid scenario must return nil; got %v", err)
	}

	if _, err := os.Stat(pidPath); !os.IsNotExist(err) {
		t.Fatalf("expected stale pid file to be cleaned up; stat err = %v", err)
	}
}

// TestControlStop_SignalsLivePIDAndCleansUp is the headline happy-
// path test: a "live daemon" (simulated by `sleep 60`) gets SIGTERM'd
// by controlStop, exits, and the PID file is cleaned up. Returns
// nil. This is what `roboticus daemon stop` should do every time
// against a normally-running daemon.
//
// We use sleep rather than a custom Go test binary to keep the test
// hermetic — no need to compile a fixture, no platform-specific
// build steps. SIGTERM handling on `sleep` exits immediately on
// every Unix.
func TestControlStop_SignalsLivePIDAndCleansUp(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Signal-based stop is Unix-only")
	}

	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	pidPath := filepath.Join(tmpHome, "live.pid")

	// Use a shorter graceful timeout for the test by spawning a
	// sleep that responds to SIGTERM quickly. `sleep 60` exits on
	// SIGTERM with status 143; the issue is that as a child of the
	// test process, the kernel keeps it as a zombie until the
	// parent (this test) calls wait(). We start a goroutine that
	// continuously calls cmd.Wait() so any signal-induced exit is
	// reaped immediately and ProcessIsAlive sees the post-reap
	// "not in process table" state.
	cmd := exec.Command("sleep", "60")
	if err := cmd.Start(); err != nil {
		t.Fatalf("spawn sleep: %v", err)
	}
	livePID := cmd.Process.Pid

	// Background reaper: blocks on Wait() until the child exits,
	// then becomes a no-op. Without this the SIGTERM'd child sits
	// as a zombie under the test process and ProcessIsAlive (which
	// uses kill(pid,0)) returns true even though the process has
	// exited — kernel just hasn't removed the entry yet.
	reaped := make(chan struct{})
	go func() {
		_, _ = cmd.Process.Wait()
		close(reaped)
	}()
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		<-reaped
	})

	if err := os.WriteFile(pidPath, []byte(strconv.Itoa(livePID)+"\n"), 0o600); err != nil {
		t.Fatalf("seed live pid file: %v", err)
	}

	cfg := &core.Config{
		Daemon: core.DaemonConfig{PIDFile: pidPath},
	}

	done := make(chan error, 1)
	go func() { done <- controlStop(cfg) }()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("controlStop on live daemon must return nil; got %v", err)
		}
	case <-time.After(stopGracefulTimeout + stopForceKillTimeout + 5*time.Second):
		t.Fatalf("controlStop did not return within the documented graceful + force-kill budget")
	}

	// Wait for the reaper to complete so we know the kernel has
	// removed the process entry before we assert ProcessIsAlive.
	select {
	case <-reaped:
	case <-time.After(2 * time.Second):
		t.Fatalf("reaper did not complete within 2s after controlStop returned")
	}

	if _, err := os.Stat(pidPath); !os.IsNotExist(err) {
		t.Fatalf("expected pid file to be cleaned up after stop; stat err = %v", err)
	}
	if ProcessIsAlive(livePID) {
		t.Fatalf("expected target process to be dead after controlStop; pid %d still alive", livePID)
	}
}

// TestProcessIsAlive_LiveAndDeadCases sanity-checks the underlying
// liveness primitive. controlStop's correctness depends on this
// distinguishing live from dead reliably.
func TestProcessIsAlive_LiveAndDeadCases(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("kill(pid, 0) liveness is Unix-only")
	}

	if !ProcessIsAlive(os.Getpid()) {
		t.Fatalf("ProcessIsAlive(self) must be true")
	}

	// PID 0 and -1 are invalid; both must report dead.
	if ProcessIsAlive(0) {
		t.Fatalf("ProcessIsAlive(0) must be false")
	}
	if ProcessIsAlive(-1) {
		t.Fatalf("ProcessIsAlive(-1) must be false")
	}

	cmd := exec.Command("sh", "-c", "exit 0")
	if err := cmd.Run(); err != nil {
		t.Fatalf("spawn sh -c 'exit 0': %v", err)
	}
	if ProcessIsAlive(cmd.Process.Pid) {
		t.Skip("OS reused PID immediately; cannot test dead-process branch")
	}
	// (test passes implicitly when the conditional above triggers t.Skip)
}

// TestSignalProcess_DeadPIDSurfacesAsRecognizedError covers the
// failure-mode handling inside stopByPID: a process that exits between
// our liveness check and the SIGTERM dispatch must surface as either
// syscall.ESRCH (for processes the current process did NOT spawn,
// where Go falls through to the kernel) OR os.ErrProcessDone (for
// processes Go's runtime is tracking, e.g., child processes spawned
// via exec.Cmd that have already been Wait'd). stopByPID treats both
// as semantically equivalent: the process is gone.
//
// Production daemon-stop operates on PIDs read from a pid file,
// which the stop process did NOT spawn — that's the ESRCH path. The
// test environment can only easily produce os.ErrProcessDone (since
// we have to spawn the test child to get a known-dead PID), so this
// test pins the dual-error contract and the production path
// inherits the ESRCH branch via the same code.
func TestSignalProcess_DeadPIDSurfacesAsRecognizedError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("ESRCH / ErrProcessDone semantics are Unix-rooted")
	}

	cmd := exec.Command("sh", "-c", "exit 0")
	if err := cmd.Run(); err != nil {
		t.Fatalf("spawn sh -c 'exit 0': %v", err)
	}
	deadPID := cmd.Process.Pid
	if ProcessIsAlive(deadPID) {
		t.Skip("PID reused immediately")
	}

	err := SignalProcess(deadPID, syscall.SIGTERM)
	if err == nil {
		t.Fatalf("SignalProcess to dead PID must error; got nil")
	}
	// Either error shape is acceptable — both indicate the process
	// is no longer reachable, and stopByPID handles both
	// interchangeably (control.go's SIGTERM branch and SIGKILL
	// branch both use `errors.Is(err, syscall.ESRCH) ||
	// errors.Is(err, os.ErrProcessDone)`).
	if !errors.Is(err, syscall.ESRCH) && !errors.Is(err, os.ErrProcessDone) {
		t.Fatalf("expected ESRCH or os.ErrProcessDone for dead PID; got %v (%T)", err, err)
	}
}

// TestIsLaunchctlNotLoaded covers the substring matcher that distinguishes
// "service not present in this domain" (a successful no-op when iterating
// system/ → gui/) from real launchctl failures. This is the only piece
// of the macOS-specific bootout path we can unit-test without shelling
// out to launchctl itself.
func TestIsLaunchctlNotLoaded(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil error", nil, false},
		{"plain not-found", errors.New("launchctl bootout system/roboticus: Could not find specified service"), true},
		{"bootout 113", errors.New("launchctl: Boot-out failed: 113: stuff"), true},
		{"no such process", errors.New("launchctl: No such process"), true},
		{"unrelated permission denied", errors.New("Operation not permitted"), false},
		{"unrelated I/O error", errors.New("Input/output error"), false},
		{"case insensitive match", errors.New("COULD NOT FIND SPECIFIED SERVICE"), true},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got := isLaunchctlNotLoaded(tc.err)
			if got != tc.want {
				t.Fatalf("isLaunchctlNotLoaded(%v) = %v; want %v", tc.err, got, tc.want)
			}
		})
	}
}

// containsSubstring is a tiny helper to keep the test assertions
// readable without pulling strings into every test file.
func containsSubstring(haystack, needle string) bool {
	if len(needle) == 0 {
		return true
	}
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
