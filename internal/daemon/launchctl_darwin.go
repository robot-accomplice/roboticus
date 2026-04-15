//go:build darwin

// launchctl_darwin.go provides the macOS-specific launchctl invocation
// used by the v1.0.6 daemon-stop fallback. Lives in its own build-
// tagged file so the import of os/exec doesn't pollute non-Darwin
// builds, and so the kardianos `launchctl unload` legacy code path
// stays untouched on non-macOS platforms (Linux uses systemctl,
// Windows uses SCM, both already handled inside kardianos itself).

package daemon

import (
	"fmt"
	"os/exec"
	"strings"
)

// runLaunchctl invokes the launchctl binary with the given args and
// returns a clean error containing the captured stderr on non-zero
// exit. The error message is what isLaunchctlNotLoaded inspects to
// distinguish "service not present in this domain" (a successful
// no-op) from a real failure.
func runLaunchctl(args ...string) error {
	cmd := exec.Command("launchctl", args...)
	out, err := cmd.CombinedOutput()
	if err == nil {
		return nil
	}
	stderr := strings.TrimSpace(string(out))
	if stderr == "" {
		return fmt.Errorf("launchctl %s: %w", strings.Join(args, " "), err)
	}
	return fmt.Errorf("launchctl %s: %s (%w)", strings.Join(args, " "), stderr, err)
}
