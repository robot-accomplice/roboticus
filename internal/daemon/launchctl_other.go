//go:build !darwin

// launchctl_other.go is the non-Darwin shim for runLaunchctl. The
// macOS bootout path in control.go is gated on runtime.GOOS=="darwin"
// so this function is unreachable at runtime on Linux/Windows — it
// exists purely so control.go compiles on every platform without
// build tags of its own (which would fragment the control logic
// across multiple files for a single OS-specific code path).

package daemon

import (
	"errors"
	"runtime"
)

func runLaunchctl(args ...string) error {
	return errors.New("launchctl is not available on " + runtime.GOOS)
}
