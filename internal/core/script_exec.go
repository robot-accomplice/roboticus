package core

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

var defaultAllowedScriptInterpreters = []string{"sh", "bash", "python3", "node", "ruby", "perl", "pwsh", "gosh"}

// DefaultAllowedScriptInterpreters returns the shared default interpreter allowlist
// used by both the skills runner and manifest-backed plugin scripts.
func DefaultAllowedScriptInterpreters() []string {
	return append([]string(nil), defaultAllowedScriptInterpreters...)
}

// ScriptExecConfig defines the shared execution contract for repository-owned scripts.
type ScriptExecConfig struct {
	RootDir             string
	AllowAbsolutePath   bool
	AllowedInterpreters []string
	MaxOutputBytes      int
	SandboxEnv          bool
	BaseEnv             map[string]string
	Runner              ProcessRunner
}

// ScriptExecResult captures the outcome of shared script execution.
type ScriptExecResult struct {
	Stdout     string
	Stderr     string
	ExitCode   int
	DurationMs int64
}

// ExecuteScript runs a script under the shared containment/interpreter/env contract.
func ExecuteScript(ctx context.Context, scriptPath string, args []string, timeout time.Duration, cfg ScriptExecConfig) (*ScriptExecResult, error) {
	resolved, err := resolveScriptPath(scriptPath, cfg.RootDir, cfg.AllowAbsolutePath)
	if err != nil {
		return nil, fmt.Errorf("script path resolution failed: %w", err)
	}

	interpreter, err := checkInterpreter(resolved, cfg.AllowedInterpreters)
	if err != nil {
		return nil, fmt.Errorf("interpreter validation failed: %w", err)
	}

	if cfg.MaxOutputBytes <= 0 {
		cfg.MaxOutputBytes = 10 * 1024 * 1024
	}
	if cfg.Runner == nil {
		cfg.Runner = OSProcessRunner{}
	}

	execArgs := append([]string{resolved}, args...)
	env := buildScriptEnvironment(cfg.RootDir, cfg.SandboxEnv, cfg.BaseEnv)

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	start := time.Now()
	stdout, stderr, runErr := cfg.Runner.Run(ctx, interpreter, execArgs, filepath.Dir(resolved), env)
	duration := time.Since(start)

	outStr := truncateScriptOutput(string(stdout), cfg.MaxOutputBytes)
	errStr := truncateScriptOutput(string(stderr), cfg.MaxOutputBytes)

	exitCode := 0
	if runErr != nil {
		if exitErr, ok := runErr.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			if errStr != "" {
				return nil, fmt.Errorf("script execution failed: %w\nstderr: %s", runErr, errStr)
			}
			return nil, fmt.Errorf("script execution failed: %w", runErr)
		}
	}

	return &ScriptExecResult{
		Stdout:     outStr,
		Stderr:     errStr,
		ExitCode:   exitCode,
		DurationMs: duration.Milliseconds(),
	}, nil
}

func truncateScriptOutput(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}
	return s[:maxBytes] + "\n...[truncated]"
}

func resolveScriptPath(requested, rootDir string, allowAbsolute bool) (string, error) {
	if strings.TrimSpace(rootDir) == "" {
		return "", fmt.Errorf("script root directory not configured")
	}

	root, err := filepath.Abs(rootDir)
	if err != nil {
		return "", fmt.Errorf("cannot resolve script root: %w", err)
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return "", fmt.Errorf("cannot resolve script root symlinks: %w", err)
	}

	var candidate string
	switch {
	case filepath.IsAbs(requested) && !allowAbsolute:
		return "", fmt.Errorf("absolute script paths are not allowed: %s", requested)
	case filepath.IsAbs(requested):
		candidate = requested
	default:
		candidate = filepath.Join(root, requested)
	}

	resolved, err := filepath.EvalSymlinks(candidate)
	if err != nil {
		return "", fmt.Errorf("cannot resolve script path: %w", err)
	}
	if !hasPathPrefix(resolved, root) {
		return "", fmt.Errorf("script path escapes root: %s", requested)
	}

	info, err := os.Stat(resolved)
	if err != nil {
		return "", fmt.Errorf("script not found: %s", resolved)
	}
	if info.IsDir() {
		return "", fmt.Errorf("script path is a directory: %s", resolved)
	}
	return resolved, nil
}

func checkInterpreter(scriptPath string, allowed []string) (string, error) {
	interpreter := ""

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

	if interpreter == "" {
		interpreter = inferInterpreterFromExt(scriptPath)
	}
	if interpreter == "" {
		return "", fmt.Errorf("cannot determine interpreter for %s", scriptPath)
	}

	if len(allowed) == 0 {
		allowed = DefaultAllowedScriptInterpreters()
	}
	baseName := filepath.Base(interpreter)
	for _, candidate := range allowed {
		if baseName == candidate || interpreter == candidate {
			return resolveInterpreterAbsolute(interpreter)
		}
	}
	return "", fmt.Errorf("interpreter %q not in allowed list %v", baseName, allowed)
}

func parseShebang(line string) string {
	line = strings.TrimPrefix(line, "#!")
	line = strings.TrimSpace(line)
	parts := strings.Fields(line)
	if len(parts) == 0 {
		return ""
	}
	if filepath.Base(parts[0]) == "env" && len(parts) > 1 {
		return parts[1]
	}
	return parts[0]
}

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

func resolveInterpreterAbsolute(name string) (string, error) {
	if filepath.IsAbs(name) {
		resolved, err := filepath.EvalSymlinks(name)
		if err != nil {
			return "", fmt.Errorf("interpreter not found: %s", name)
		}
		return resolved, nil
	}
	path, err := exec.LookPath(name)
	if err != nil {
		return "", fmt.Errorf("interpreter %q not found in PATH", name)
	}
	return filepath.Abs(path)
}

func buildScriptEnvironment(root string, sandbox bool, base map[string]string) []string {
	if !sandbox {
		env := append([]string(nil), os.Environ()...)
		return appendEnvMap(env, base)
	}

	env := []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + os.Getenv("HOME"),
		"LANG=" + os.Getenv("LANG"),
		"TERM=" + os.Getenv("TERM"),
	}
	if tmp := os.Getenv("TMPDIR"); tmp != "" {
		env = append(env, "TMPDIR="+tmp)
	}
	return appendEnvMap(env, base)
}

func appendEnvMap(env []string, values map[string]string) []string {
	if len(values) == 0 {
		return env
	}
	for k, v := range values {
		env = append(env, k+"="+v)
	}
	return env
}

func hasPathPrefix(path, root string) bool {
	return path == root || strings.HasPrefix(path, root+string(filepath.Separator))
}
