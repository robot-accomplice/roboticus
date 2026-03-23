package tools

import (
	"context"
	"os/exec"
	"runtime"
)

// execCommand creates a platform-appropriate shell command.
func execCommand(ctx context.Context, command string) *exec.Cmd {
	if runtime.GOOS == "windows" {
		return exec.CommandContext(ctx, "cmd", "/C", command)
	}
	return exec.CommandContext(ctx, "sh", "-c", command)
}
