// ScriptRunner provides agent-level script execution with interpreter validation,
// path containment, and environment sandboxing.
//
// Ported from Rust: crates/roboticus-agent/src/script_runner.rs
//
// Unlike the plugin ScriptPlugin (which manages tool manifests), ScriptRunner
// is the raw execution facility. It now delegates to the shared core script
// execution contract so skills and plugin scripts cannot drift independently.

package agent

import (
	"context"
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
		interpreters = core.DefaultAllowedScriptInterpreters()
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
	result, err := core.ExecuteScript(ctx, scriptPath, args, timeout, core.ScriptExecConfig{
		RootDir:             sr.skillsRoot,
		AllowAbsolutePath:   false,
		AllowedInterpreters: sr.allowedInterpreters,
		MaxOutputBytes:      sr.maxOutputBytes,
		SandboxEnv:          sr.sandboxEnv,
		BaseEnv: map[string]string{
			"ROBOTICUS_SKILLS_DIR": sr.skillsRoot,
		},
		Runner: sr.runner,
	})
	if err != nil {
		return nil, err
	}
	return &ScriptResult{
		Stdout:     result.Stdout,
		Stderr:     result.Stderr,
		ExitCode:   result.ExitCode,
		DurationMs: result.DurationMs,
	}, nil
}
