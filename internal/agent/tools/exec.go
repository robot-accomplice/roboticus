package tools

import (
	"context"
	"os/exec"
	"runtime"
)

// execCommand creates a platform-appropriate shell command.
// Retained for backward compatibility; prefer tctx.GetRunner().Run().
func execCommand(ctx context.Context, command string) *exec.Cmd {
	if runtime.GOOS == "windows" {
		return exec.CommandContext(ctx, "cmd", "/C", command)
	}
	return exec.CommandContext(ctx, "sh", "-c", command)
}

// shellCommand returns the platform shell name and args.
func shellCommand(command string) (string, []string) {
	if runtime.GOOS == "windows" {
		return "cmd", []string{"/C", command}
	}
	return "sh", []string{"-c", command}
}
