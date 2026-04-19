// pidfile.go is the v1.0.6 PID-file surface: roboticus daemon control
// verbs (stop, status, restart) need a way to find a running daemon
// without re-booting the entire 12-step subsystem stack just to query
// state. The pre-v1.0.6 path went through kardianos/service →
// `launchctl unload` (or systemctl) which has three problems:
//
//   1. Only finds launchd-managed instances. A `roboticus serve`
//      foreground process is invisible to `launchctl print`, so
//      `roboticus daemon stop` returns the legacy and unhelpful
//      "Failed to stop ... launchctl ... Input/output error" instead
//      of actually stopping the running daemon.
//   2. Requires sudo on macOS even when the user just wants to
//      manage their own runtime. The user mental model is that
//      `roboticus daemon stop` is THEIR tool — only OS service
//      management (launchctl bootstrap / systemctl) needs root.
//   3. The pre-v1.0.6 Control() implementation runs the entire
//      daemon boot just to construct a service object, which under
//      sudo creates root-owned files in ~/.roboticus that lock the
//      user out of subsequent unprivileged invocations.
//
// The PID file fixes all three. `roboticus serve` writes the PID
// file at the moment the HTTP server is ready; `roboticus daemon
// stop` reads it, sends SIGTERM, waits for graceful shutdown, and
// returns idempotent success when the daemon was already stopped.
// No sudo, no full boot, no chown side effects.
//
// The PID file lives at DaemonConfig.PIDFile when set, otherwise at
// `<roboticus-home>/roboticus.pid`. The `roboticus.pid` default sits
// alongside the DB and shares its 0o600-on-write tightening so the
// PID is not exposed to other accounts on the host (a PID leak isn't
// catastrophic on its own, but advertising the daemon's existence
// and timing to other users is gratuitous).

package daemon

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/rs/zerolog/log"

	"roboticus/internal/core"
)

// DefaultPIDFileName is the basename used when DaemonConfig.PIDFile is
// empty. Lives next to the DB under the roboticus home directory.
const DefaultPIDFileName = "roboticus.pid"

// PIDFilePath resolves the configured (or default) PID file path. When
// the configured path is relative or unset, it's resolved against the
// roboticus home directory derived from $HOME, matching how the
// database and wallet paths are derived.
func PIDFilePath(cfg *core.Config) string {
	if cfg != nil && cfg.Daemon.PIDFile != "" {
		if filepath.IsAbs(cfg.Daemon.PIDFile) {
			return cfg.Daemon.PIDFile
		}
		return filepath.Join(roboticusHomeDir(), cfg.Daemon.PIDFile)
	}
	return filepath.Join(roboticusHomeDir(), DefaultPIDFileName)
}

// WritePIDFile records the current process's PID to the resolved PID
// file path with restrictive permissions (0o600). Fails atomically:
// either the file exists with this process's PID after the call, or
// the call returns an error and the file is unchanged.
//
// Stale-PID handling: if a PID file already exists pointing at a
// running process that ISN'T us, WritePIDFile refuses to overwrite
// and returns an error indicating the conflict. This prevents two
// daemons from racing each other into the same on-disk slot. If the
// PID file points at a dead PID (the previous daemon was kill -9'd
// or crashed), WritePIDFile silently overwrites — that's the
// recovery path the v1.0.6 daemon-stop work depends on.
func WritePIDFile(path string) error {
	if path == "" {
		return errors.New("pid file path is empty")
	}

	if existing, err := readPIDFromFile(path); err == nil && existing > 0 && existing != os.Getpid() {
		if processIsAlive(existing) {
			return fmt.Errorf("pid file %s already records live PID %d; refusing to overwrite (is another daemon running?)", path, existing)
		}
		// Existing PID is dead — treat as stale and overwrite.
		log.Info().
			Int("stale_pid", existing).
			Str("path", path).
			Msg("removing stale pid file (recorded process no longer alive)")
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create pid file directory: %w", err)
	}

	// Write to a temp file in the same directory then rename so the
	// PID file appears atomically — readers never see a partial PID.
	tmp, err := os.CreateTemp(filepath.Dir(path), ".roboticus.pid.*")
	if err != nil {
		return fmt.Errorf("create pid temp file: %w", err)
	}
	tmpName := tmp.Name()

	pidStr := strconv.Itoa(os.Getpid()) + "\n"
	if _, err := tmp.WriteString(pidStr); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return fmt.Errorf("write pid temp file: %w", err)
	}
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return fmt.Errorf("chmod pid temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("close pid temp file: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("rename pid file into place: %w", err)
	}
	return nil
}

// RemovePIDFile deletes the PID file. Best-effort: missing file is
// not an error (idempotent on repeated shutdown attempts), but other
// I/O errors are returned so the caller can log them.
func RemovePIDFile(path string) error {
	if path == "" {
		return nil
	}
	err := os.Remove(path)
	if err == nil || errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// ReadPIDFile returns (pid, found, error). `found=false` with no
// error means the PID file does not exist — the caller should treat
// that as "no daemon known to be running via this mechanism" and
// proceed with the fallback path (e.g., launchctl).
//
// `found=true, pid=N, err=nil` means the file exists and parsed
// cleanly. The caller still needs to decide whether N is a live
// process via ProcessIsAlive.
//
// `found=true, pid=0, err=...` means the file exists but is
// malformed. Callers can choose to remove the file and retry.
func ReadPIDFile(path string) (int, bool, error) {
	if path == "" {
		return 0, false, errors.New("pid file path is empty")
	}
	pid, err := readPIDFromFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return 0, false, nil
	}
	if err != nil {
		return 0, true, err
	}
	return pid, true, nil
}

// ProcessIsAlive returns true when a process with the given PID
// exists and is reachable by the current user. On Unix this uses
// kill(pid, 0): returns nil for live, ESRCH for dead, EPERM if the
// PID is alive but owned by a different user (which we still report
// as "alive" — the daemon is running, we just can't signal it from
// here, which is itself meaningful state).
func ProcessIsAlive(pid int) bool {
	return processIsAlive(pid)
}

// SignalProcess sends a Unix signal to the given PID. Wraps the
// platform-specific syscall behind a clear API so the daemon-stop
// path can call SignalProcess(pid, syscall.SIGTERM) without
// reaching into syscall directly from business logic.
func SignalProcess(pid int, sig syscall.Signal) error {
	if pid <= 0 {
		return fmt.Errorf("invalid pid %d", pid)
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("find process %d: %w", pid, err)
	}
	return proc.Signal(sig)
}

// WaitForExit polls every pollInterval until the process is no
// longer alive or the timeout elapses. Returns true if the process
// exited within the budget, false on timeout. Used by the daemon
// stop path after sending SIGTERM, before deciding whether to
// escalate to SIGKILL.
func WaitForExit(pid int, timeout, pollInterval time.Duration) bool {
	deadline := time.Now().Add(timeout)
	if pollInterval <= 0 {
		pollInterval = 100 * time.Millisecond
	}
	for {
		if !processIsAlive(pid) {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(pollInterval)
	}
}

// ── internal helpers ──────────────────────────────────────────────────

func readPIDFromFile(path string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" {
		return 0, fmt.Errorf("pid file %s is empty", path)
	}
	pid, err := strconv.Atoi(trimmed)
	if err != nil {
		return 0, fmt.Errorf("pid file %s contains malformed pid %q: %w", path, trimmed, err)
	}
	if pid <= 0 {
		return 0, fmt.Errorf("pid file %s contains non-positive pid %d", path, pid)
	}
	return pid, nil
}

func processIsAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	// Unix: kill(pid, 0) is the standard liveness check. Returns nil
	// when the process exists and we can signal it; ESRCH when the
	// PID isn't taken; EPERM when the PID is alive but we lack
	// signaling permission (which still means the process is running,
	// so we report alive=true). os.FindProcess on Unix never returns
	// an error — only the Signal call does.
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = proc.Signal(syscall.Signal(0))
	if err == nil {
		return true
	}
	// EPERM means the PID is alive but in another security context.
	if errors.Is(err, syscall.EPERM) {
		return true
	}
	return false
}

// roboticusHomeDir returns the canonical roboticus data directory,
// defaulting to ~/.roboticus when $HOME is set. We deliberately do
// NOT use core.ConfigDir() here to avoid a dependency cycle and to
// keep this helper self-contained — the PID file path is the same
// for every roboticus instance regardless of profile.
func roboticusHomeDir() string {
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		return filepath.Join(home, ".roboticus")
	}
	// Fallback to the current directory if HOME isn't resolvable —
	// extreme edge case (CI containers with no HOME), but better
	// than panicking.
	return ".roboticus"
}
