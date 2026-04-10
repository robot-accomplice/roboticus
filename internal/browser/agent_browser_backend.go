// External agent-browser CLI backend.
//
// Executes browser actions by spawning the agent-browser CLI with --json
// mode and parsing structured output. Preserves policy controls and
// provenance from the Roboticus side.
//
// Ported from Rust: crates/roboticus-browser/src/agent_browser_backend.rs

package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"time"

	"github.com/rs/zerolog/log"
)

// AgentBrowserConfig configures the external agent-browser backend.
type AgentBrowserConfig struct {
	BinaryPath     string // path to agent-browser binary (resolved via PATH if relative)
	TimeoutSeconds int    // timeout per CLI invocation (default 30)
}

// AgentBrowserBackend delegates browser actions to an external agent-browser CLI.
type AgentBrowserBackend struct {
	cfg       AgentBrowserConfig
	available bool
}

// NewAgentBrowserBackend creates a backend, checking if the binary is available on PATH.
func NewAgentBrowserBackend(cfg AgentBrowserConfig) *AgentBrowserBackend {
	if cfg.BinaryPath == "" {
		cfg.BinaryPath = "agent-browser"
	}
	if cfg.TimeoutSeconds == 0 {
		cfg.TimeoutSeconds = 30
	}

	_, err := exec.LookPath(cfg.BinaryPath)
	available := err == nil
	if !available {
		log.Warn().Str("binary", cfg.BinaryPath).Msg("agent-browser binary not found — backend will be unavailable")
	}

	return &AgentBrowserBackend{cfg: cfg, available: available}
}

func (ab *AgentBrowserBackend) Execute(ctx context.Context, action *BrowserAction) ActionResult {
	if !ab.available {
		return ActionResult{Action: action.Kind, Error: "agent-browser backend not available"}
	}

	args := actionToArgs(action)
	args = append([]string{"--json"}, args...)

	timeout := time.Duration(ab.cfg.TimeoutSeconds) * time.Second
	execCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(execCtx, ab.cfg.BinaryPath, args...)
	output, err := cmd.Output()
	if err != nil {
		return ActionResult{
			Action: action.Kind,
			Error:  fmt.Sprintf("agent-browser execution failed: %v", err),
		}
	}

	var result ActionResult
	if err := json.Unmarshal(output, &result); err != nil {
		return ActionResult{
			Action:  action.Kind,
			Success: true,
			Data:    output,
		}
	}
	return result
}

func (ab *AgentBrowserBackend) Name() string { return "agent-browser" }

func (ab *AgentBrowserBackend) IsAvailable() bool { return ab.available }

// actionToArgs maps a BrowserAction to agent-browser CLI arguments.
func actionToArgs(action *BrowserAction) []string {
	switch action.Kind {
	case ActionNavigate:
		return []string{"navigate", action.URL}
	case ActionClick:
		return []string{"click", action.Selector}
	case ActionType:
		return []string{"type", action.Selector, action.Text}
	case ActionScreenshot:
		return []string{"screenshot"}
	case ActionPDF:
		return []string{"pdf"}
	case ActionEvaluate:
		return []string{"evaluate", action.Script}
	case ActionGetCookies:
		return []string{"get-cookies"}
	case ActionClearCookies:
		return []string{"clear-cookies"}
	case ActionReadPage:
		return []string{"read-page"}
	case ActionGoBack:
		return []string{"go-back"}
	case ActionGoForward:
		return []string{"go-forward"}
	case ActionReload:
		return []string{"reload"}
	default:
		return []string{string(action.Kind)}
	}
}
