// ScriptRunner provides agent-level script execution with interpreter validation,
// path containment, and environment sandboxing.
//
// Ported from Rust: crates/roboticus-agent/src/script_runner.rs
//
// Unlike the plugin ScriptPlugin (which manages tool manifests), ScriptRunner
// is the raw execution facility: validate interpreter, resolve paths safely,
// sandbox the environment, and capture output.

package agent

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"roboticus/internal/core"
)

// ScriptResult captures the output of a script execution.
type ScriptResult struct {
	Stdout     string
	Stderr     string
	ExitCode   int
	DurationMs int64
}

// ScriptRunner executes scripts with security controls.
type ScriptRunner struct {
	skillsRoot          string
	allowedInterpreters []string
	maxOutputBytes      int
	sandboxEnv          bool
	runner              core.ProcessRunner
}

// NewScriptRunner creates a ScriptRunner with the given configuration.
func NewScriptRunner(cfg core.SkillsConfig) *ScriptRunner {
	interpreters := cfg.AllowedInterpreters
	if len(interpreters) == 0 {
		interpreters = []string{"bash", "python3", "node"}
	}
	maxOutput := cfg.ScriptMaxOutputBytes
	if maxOutput <= 0 {
		maxOutput = 10 * 1024 * 1024 // 10MB
	}
	return &ScriptRunner{
		skillsRoot:          cfg.Directory,
		allowedInterpreters: interpreters,
		maxOutputBytes:      maxOutput,
		sandboxEnv:          cfg.SandboxEnv,
		runner:              core.OSProcessRunner{},
	}
}

// Execute runs a script at the given path with arguments and timeout.
func (sr *ScriptRunner) Execute(ctx context.Context, scriptPath string, args []string, timeout time.Duration) (*ScriptResult, error) {
	// Resolve and validate the script path.
	resolved, err := sr.resolveScriptPath(scriptPath)
	if err != nil {
		return nil, fmt.Errorf("script path resolution failed: %w", err)
	}

	// Validate interpreter.
	interpreter, err := sr.checkInterpreter(resolved)
	if err != nil {
		return nil, fmt.Errorf("interpreter validation failed: %w", err)
	}

	// Build execution args: interpreter + script + user args.
	execArgs := append([]string{resolved}, args...)

	// Build sandboxed environment.
	env := sr.buildEnvironment(resolved)

	// Apply timeout.
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	start := time.Now()
	stdout, stderr, runErr := sr.runner.Run(ctx, interpreter, execArgs, filepath.Dir(resolved), env)
	duration := time.Since(start)

	// Truncate output.
	outStr := string(stdout)
	if len(outStr) > sr.maxOutputBytes {
		outStr = outStr[:sr.maxOutputBytes] + "\n...[truncated]"
	}
	errStr := string(stderr)
	if len(errStr) > sr.maxOutputBytes {
		errStr = errStr[:sr.maxOutputBytes] + "\n...[truncated]"
	}

	exitCode := 0
	if runErr != nil {
		if exitErr, ok := runErr.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			return nil, fmt.Errorf("script execution failed: %w", runErr)
		}
	}

	return &ScriptResult{
		Stdout:     outStr,
		Stderr:     errStr,
		ExitCode:   exitCode,
		DurationMs: duration.Milliseconds(),
	}, nil
}

// resolveScriptPath validates and resolves a script path with containment.
func (sr *ScriptRunner) resolveScriptPath(requested string) (string, error) {
	// Reject absolute paths — scripts must be relative to skills root.
	if filepath.IsAbs(requested) {
		return "", fmt.Errorf("absolute script paths are not allowed: %s", requested)
	}

	if sr.skillsRoot == "" {
		return "", fmt.Errorf("skills root directory not configured")
	}

	// Canonicalize the root.
	root, err := filepath.Abs(sr.skillsRoot)
	if err != nil {
		return "", fmt.Errorf("cannot resolve skills root: %w", err)
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return "", fmt.Errorf("cannot resolve skills root symlinks: %w", err)
	}

	// Join and canonicalize.
	resolved := filepath.Join(root, requested)
	resolved, err = filepath.EvalSymlinks(resolved)
	if err != nil {
		return "", fmt.Errorf("cannot resolve script path: %w", err)
	}

	// Containment check — prevent ../ escapes.
	if !strings.HasPrefix(resolved, root+string(filepath.Separator)) && resolved != root {
		return "", fmt.Errorf("script path escapes skills root: %s", requested)
	}

	// Validate file exists and is regular.
	info, err := os.Stat(resolved)
	if err != nil {
		return "", fmt.Errorf("script not found: %s", resolved)
	}
	if info.IsDir() {
		return "", fmt.Errorf("script path is a directory: %s", resolved)
	}

	return resolved, nil
}

// checkInterpreter reads the script shebang or infers from extension,
// validates against the allowed interpreter list, and returns the absolute
// interpreter path.
func (sr *ScriptRunner) checkInterpreter(scriptPath string) (string, error) {
	interpreter := ""

	// Try shebang first.
	f, err := os.Open(scriptPath)
	if err == nil {
		scanner := bufio.NewScanner(f)
		if scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "#!") {
				interpreter = parseShebang(line)
			}
		}
		_ = f.Close()
	}

	// Fall back to extension inference.
	if interpreter == "" {
		interpreter = inferInterpreterFromExt(scriptPath)
	}

	if interpreter == "" {
		return "", fmt.Errorf("cannot determine interpreter for %s", scriptPath)
	}

	// Validate against allowed list.
	baseName := filepath.Base(interpreter)
	allowed := false
	for _, a := range sr.allowedInterpreters {
		if baseName == a || interpreter == a {
			allowed = true
			break
		}
	}
	if !allowed {
		return "", fmt.Errorf("interpreter %q not in allowed list %v", baseName, sr.allowedInterpreters)
	}

	// Resolve to absolute path to prevent PATH hijacking.
	return resolveInterpreterAbsolute(interpreter)
}

// parseShebang extracts the interpreter name from a shebang line.
func parseShebang(line string) string {
	line = strings.TrimPrefix(line, "#!")
	line = strings.TrimSpace(line)

	// Handle "#!/usr/bin/env python3" form.
	parts := strings.Fields(line)
	if len(parts) == 0 {
		return ""
	}
	if filepath.Base(parts[0]) == "env" && len(parts) > 1 {
		return parts[1]
	}
	return parts[0]
}

// inferInterpreterFromExt maps file extension to interpreter name.
func inferInterpreterFromExt(path string) string {
	switch filepath.Ext(path) {
	case ".sh":
		return "bash"
	case ".py":
		return "python3"
	case ".rb":
		return "ruby"
	case ".js":
		return "node"
	case ".go":
		return "go"
	default:
		return ""
	}
}

// resolveInterpreterAbsolute finds the absolute path of an interpreter.
func resolveInterpreterAbsolute(name string) (string, error) {
	if filepath.IsAbs(name) {
		resolved, err := filepath.EvalSymlinks(name)
		if err != nil {
			return "", fmt.Errorf("interpreter not found: %s", name)
		}
		return resolved, nil
	}
	// Search PATH.
	path, err := exec.LookPath(name)
	if err != nil {
		return "", fmt.Errorf("interpreter %q not found in PATH", name)
	}
	return filepath.Abs(path)
}

// buildEnvironment creates a sandboxed environment for script execution.
func (sr *ScriptRunner) buildEnvironment(scriptPath string) []string {
	if !sr.sandboxEnv {
		env := os.Environ()
		env = append(env, "ROBOTICUS_SKILLS_DIR="+sr.skillsRoot)
		return env
	}

	// Sandboxed: limited environment to prevent leaking secrets.
	env := []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + os.Getenv("HOME"),
		"LANG=" + os.Getenv("LANG"),
		"TERM=" + os.Getenv("TERM"),
		"ROBOTICUS_SKILLS_DIR=" + sr.skillsRoot,
	}
	if tmp := os.Getenv("TMPDIR"); tmp != "" {
		env = append(env, "TMPDIR="+tmp)
	}
	return env
}
