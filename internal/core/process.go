package core

import (
	"bytes"
	"context"
	"os/exec"
)

// ProcessRunner abstracts subprocess execution. Enables mock injection
// for testing script plugins and browser launching without real processes.
type ProcessRunner interface {
	Run(ctx context.Context, name string, args []string, dir string, env []string) (stdout []byte, stderr []byte, err error)
}

// OSProcessRunner is the real implementation using exec.CommandContext.
type OSProcessRunner struct{}

// Run executes a subprocess and captures its output.
func (OSProcessRunner) Run(ctx context.Context, name string, args []string, dir string, env []string) ([]byte, []byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	if len(env) > 0 {
		cmd.Env = env
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stdout.Bytes(), stderr.Bytes(), err
}
