// control.go implements the v1.0.6 daemon-control verbs. The previous
// implementation collapsed start/stop/restart into a single
// `service.Control(svc, action)` call that:
//
//   * required sudo on macOS even for user-mode operation
//   * ran the entire 12-step daemon boot just to construct the service
//     handle (which under sudo created root-owned files in
//     ~/.roboticus, locking subsequent unprivileged invocations out)
//   * relied on kardianos/service's `launchctl unload` path which
//     returns an uninformative "Input/output error" when the service
//     isn't loaded (the kill -9 recovery scenario, and any case
//     where the daemon was started via `roboticus serve` rather than
//     bootstrapped by launchd)
//
// The new design routes every verb through the PID file first, so
// foreground `roboticus serve` invocations and user-mode daemons are
// reachable without sudo and without launchctl. Only when no PID file
// exists do we fall back to the OS service manager — the path for
// system-installed services where launchd / systemd actually owns
// the lifecycle.

package daemon

import (
	"errors"
	"fmt"
	"os"
	"runtime"
	"syscall"
	"time"

	"github.com/kardianos/service"
	"github.com/rs/zerolog/log"

	"roboticus/internal/core"
)


// stopGracefulTimeout is the budget for SIGTERM-based graceful
// shutdown before we escalate to SIGKILL. Mirrors the daemon's own
// internal shutdown timeout (15s for goroutines, 5s for working
// memory persistence — see Daemon.Stop) plus enough margin for the
// HTTP server to drain in-flight requests.
const stopGracefulTimeout = 30 * time.Second

// stopForceKillTimeout is how long we give the OS to actually reap a
// SIGKILL'd process before giving up. SIGKILL cannot be caught, so
// process death after SIGKILL should be near-instant; this is just
// defensive against a kernel that's slow to clean up.
const stopForceKillTimeout = 5 * time.Second

// controlStop sends a graceful shutdown to a running daemon. The path
// is idempotent: stopping a daemon that isn't running returns nil
// (not an error) so `roboticus daemon stop` can be safely scripted
// as part of a state-reset sequence.
//
// Resolution order:
//
//   1. PID file. If it exists and points at a live process, signal
//      SIGTERM and wait up to stopGracefulTimeout. If the process is
//      still alive after the timeout, escalate to SIGKILL. Remove
//      the PID file once the process exits.
//
//   2. OS service manager fallback (launchctl / systemctl / SCM).
//      Only reached when no PID file exists, which corresponds to
//      "the daemon was installed as a system service and the OS
//      manages its lifecycle." On macOS we use `launchctl bootout`
//      directly rather than kardianos's legacy `unload` so the
//      stale-state error path is informative.
//
//   3. Idempotent fallback: if neither (1) nor (2) found a running
//      daemon, return nil and log "not running."
func controlStop(cfg *core.Config) error {
	pidPath := PIDFilePath(cfg)
	pid, found, err := ReadPIDFile(pidPath)
	if err != nil && found {
		// PID file exists but is malformed. Remove it and proceed to
		// the OS-service fallback — leaving a corrupt PID file in
		// place would just make the next stop fail the same way.
		log.Warn().Err(err).Str("path", pidPath).Msg("removing malformed pid file")
		_ = RemovePIDFile(pidPath)
	}

	if found && pid > 0 && ProcessIsAlive(pid) {
		return stopByPID(pid, pidPath)
	}

	// PID file absent OR points at a dead process — clean up if
	// stale, then try the OS service manager fallback.
	if found {
		log.Info().Int("stale_pid", pid).Str("path", pidPath).Msg("removing stale pid file before fallback to OS service manager")
		_ = RemovePIDFile(pidPath)
	}

	if err := stopViaServiceManager(cfg); err != nil {
		// stopViaServiceManager returns nil for the "not installed /
		// not running" case (idempotent); a real error here is
		// something operators need to see.
		return err
	}
	return nil
}

// controlStart instructs the OS service manager to start the daemon.
// There is no PID-file path here — starting a daemon means asking
// launchd / systemd / SCM to bootstrap it, which is inherently a
// service-manager operation. For foreground development use,
// operators run `roboticus serve` directly rather than `daemon
// start`.
//
// Idempotent: if the daemon is already running (per PID file or per
// service manager status), returns nil with a friendly log.
func controlStart(cfg *core.Config) error {
	// Already-running short-circuit: check both signals (PID file
	// + service manager status) so we don't accidentally try to
	// double-start a foreground `serve` invocation.
	if pidPath := PIDFilePath(cfg); pidPath != "" {
		if pid, found, _ := ReadPIDFile(pidPath); found && pid > 0 && ProcessIsAlive(pid) {
			log.Info().Int("pid", pid).Msg("roboticus already running (per pid file); start is a no-op")
			return nil
		}
	}

	svc, _, err := NewServiceOnly(cfg)
	if err != nil {
		return err
	}
	status, sErr := svc.Status()
	if sErr == nil && status == service.StatusRunning {
		log.Info().Msg("roboticus already running (per service manager); start is a no-op")
		return nil
	}

	return service.Control(svc, "start")
}

// stopByPID sends SIGTERM, waits for graceful shutdown, escalates to
// SIGKILL if the process is still alive after the budget, removes the
// PID file once the process is reaped. The function returns nil on
// successful stop (including post-SIGKILL) and only returns an error
// for cases that indicate something genuinely broken (e.g., the
// signal can't be delivered for non-EPERM reasons).
func stopByPID(pid int, pidPath string) error {
	log.Info().
		Int("pid", pid).
		Dur("graceful_timeout", stopGracefulTimeout).
		Msg("stopping roboticus via SIGTERM (pid file)")

	if err := SignalProcess(pid, syscall.SIGTERM); err != nil {
		// EPERM: the process is alive but in another security
		// context. Tell the operator clearly rather than burying the
		// signal failure.
		if errors.Is(err, syscall.EPERM) {
			return fmt.Errorf("cannot signal pid %d (insufficient permissions; check process owner)", pid)
		}
		// ESRCH or os.ErrProcessDone: process disappeared between
		// our liveness check and the signal dispatch. Both have the
		// same semantic meaning ("the process is no longer there");
		// they differ only because Go's runtime intercepts Signal
		// for processes the current process started (returns
		// ErrProcessDone), versus the kernel returning ESRCH for
		// processes we didn't start. controlStop shouldn't care
		// which path produced the signal.
		if errors.Is(err, syscall.ESRCH) || errors.Is(err, os.ErrProcessDone) {
			_ = RemovePIDFile(pidPath)
			log.Info().Int("pid", pid).Msg("process already exited; pid file removed")
			return nil
		}
		return fmt.Errorf("signal SIGTERM to pid %d: %w", pid, err)
	}

	if WaitForExit(pid, stopGracefulTimeout, 100*time.Millisecond) {
		_ = RemovePIDFile(pidPath)
		log.Info().Int("pid", pid).Msg("graceful shutdown complete; pid file removed")
		return nil
	}

	// Graceful shutdown timed out — escalate. Loud warning so the
	// operator knows the daemon didn't shut down cleanly.
	log.Warn().
		Int("pid", pid).
		Dur("waited", stopGracefulTimeout).
		Msg("SIGTERM did not stop daemon within graceful timeout; escalating to SIGKILL")

	if err := SignalProcess(pid, syscall.SIGKILL); err != nil {
		if errors.Is(err, syscall.ESRCH) || errors.Is(err, os.ErrProcessDone) {
			// Raced with self-exit between SIGTERM timeout and
			// SIGKILL; treat as success.
			_ = RemovePIDFile(pidPath)
			return nil
		}
		return fmt.Errorf("signal SIGKILL to pid %d: %w", pid, err)
	}
	if !WaitForExit(pid, stopForceKillTimeout, 50*time.Millisecond) {
		return fmt.Errorf("daemon pid %d did not exit after SIGKILL within %s — kernel reap delay or zombie state", pid, stopForceKillTimeout)
	}
	_ = RemovePIDFile(pidPath)
	log.Info().Int("pid", pid).Msg("daemon force-killed; pid file removed")
	return nil
}

// stopViaServiceManager is the fallback when no PID file is present
// (i.e., the daemon was bootstrapped by the OS service manager, not
// by `roboticus serve`). On macOS we bypass kardianos's legacy
// `launchctl unload` path in favor of the modern `launchctl bootout
// system/<name>` command, which returns informative diagnostics
// instead of "Input/output error" when the service is in a stale
// state.
//
// Idempotent: returns nil for "service not installed" and "service
// not running" — both are operator-facing successes (the daemon
// is, in fact, not running).
func stopViaServiceManager(cfg *core.Config) error {
	svc, svcCfg, err := NewServiceOnly(cfg)
	if err != nil {
		return err
	}

	status, statusErr := svc.Status()
	if errors.Is(statusErr, service.ErrNotInstalled) {
		log.Info().Msg("roboticus is not installed as a system service; stop is a no-op")
		return nil
	}
	if statusErr == nil && status == service.StatusStopped {
		log.Info().Msg("roboticus service already stopped (per service manager)")
		return nil
	}

	if runtime.GOOS == "darwin" {
		return stopViaLaunchctlBootout(svcCfg.Name)
	}
	return service.Control(svc, "stop")
}

// stopViaLaunchctlBootout invokes `launchctl bootout <domain>/<name>`
// directly, bypassing kardianos's legacy `unload` path. Bootout was
// introduced in macOS 10.10 and returns informative errors instead
// of the cryptic "Input/output error" `unload` returns when the
// service is in a stale state.
//
// Domain selection: we try `system/<name>` first (matches kardianos's
// default install path: /Library/LaunchDaemons), then fall back to
// `gui/<uid>/<name>` for user-installed services
// (~/Library/LaunchAgents). Either succeeding constitutes a clean
// stop; both failing with "not loaded"-style errors is also a
// success (the service genuinely isn't running).
func stopViaLaunchctlBootout(serviceName string) error {
	uid := os.Geteuid()
	domains := []string{
		"system/" + serviceName,
		fmt.Sprintf("gui/%d/%s", uid, serviceName),
	}

	var lastErr error
	for _, domain := range domains {
		if err := runLaunchctl("bootout", domain); err == nil {
			log.Info().Str("domain", domain).Msg("daemon stopped via launchctl bootout")
			return nil
		} else if isLaunchctlNotLoaded(err) {
			// Domain doesn't have the service loaded — try next domain.
			continue
		} else {
			lastErr = err
		}
	}

	// If every domain reported "not loaded" we treat that as
	// success — the service is, definitively, not running.
	if lastErr == nil {
		log.Info().Str("service", serviceName).Msg("launchctl reports service not loaded in any domain; stop is a no-op")
		return nil
	}
	return fmt.Errorf("launchctl bootout failed for %s: %w (try `sudo launchctl print system/%s` for richer diagnostics)", serviceName, lastErr, serviceName)
}

// isLaunchctlNotLoaded inspects a launchctl error to decide whether
// it indicates "service not present in this domain" (which we treat
// as success when iterating domains) versus a real failure. Bootout
// returns exit code 113 ("Could not find specified service") with
// stderr containing "Boot-out failed: 113: Could not find specified
// service" or similar. We match on substring rather than exit code
// because the parsed error message already carries the diagnostic
// text and exit-code parsing varies between libraries.
func isLaunchctlNotLoaded(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	for _, marker := range []string{
		"Could not find specified service",
		"Boot-out failed: 113",
		"No such process",
		"service not loaded",
	} {
		if containsCaseInsensitive(msg, marker) {
			return true
		}
	}
	return false
}

// containsCaseInsensitive is a tiny helper to avoid pulling
// strings.EqualFold semantics into substring matching. Matches "Foo"
// inside "I see foo here" regardless of case.
func containsCaseInsensitive(haystack, needle string) bool {
	if needle == "" {
		return true
	}
	hl := []byte(haystack)
	nl := []byte(needle)
	if len(nl) > len(hl) {
		return false
	}
	for i := 0; i+len(nl) <= len(hl); i++ {
		match := true
		for j := 0; j < len(nl); j++ {
			a := hl[i+j]
			b := nl[j]
			if 'A' <= a && a <= 'Z' {
				a += 32
			}
			if 'A' <= b && b <= 'Z' {
				b += 32
			}
			if a != b {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}
