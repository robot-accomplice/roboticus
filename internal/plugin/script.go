package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"goboticus/internal/core"
)

// ScriptPlugin implements Plugin by executing external scripts.
type ScriptPlugin struct {
	manifest Manifest
	dir      string
	scripts  map[string]string // tool name → script path
	timeout  time.Duration
	env      map[string]string
	runner   core.ProcessRunner // nil defaults to OSProcessRunner
}

// NewScriptPlugin creates a script-based plugin with default OS process runner.
func NewScriptPlugin(manifest Manifest, dir string) *ScriptPlugin {
	return NewScriptPluginWithRunner(manifest, dir, nil)
}

// NewScriptPluginWithRunner creates a plugin with an injected process runner.
func NewScriptPluginWithRunner(manifest Manifest, dir string, runner core.ProcessRunner) *ScriptPlugin {
	timeout := 30 * time.Second
	if manifest.TimeoutSeconds > 0 {
		timeout = time.Duration(manifest.TimeoutSeconds) * time.Second
	}
	sp := &ScriptPlugin{
		manifest: manifest,
		dir:      dir,
		scripts:  make(map[string]string),
		timeout:  timeout,
		env:      make(map[string]string),
		runner:   runner,
	}
	sp.discoverScripts()
	return sp
}

func (sp *ScriptPlugin) Name() string    { return sp.manifest.Name }
func (sp *ScriptPlugin) Version() string { return sp.manifest.Version }

func (sp *ScriptPlugin) Tools() []ToolDef {
	var tools []ToolDef
	for _, mt := range sp.manifest.Tools {
		td := ToolDef{
			Name:        mt.Name,
			Description: mt.Description,
			Permissions: mt.Permissions,
		}
		if mt.Dangerous {
			td.RiskLevel = "dangerous"
		} else {
			td.RiskLevel = "safe"
		}
		if mt.ParametersSchema != "" {
			td.Parameters = json.RawMessage(mt.ParametersSchema)
		} else {
			td.Parameters = defaultParameterSchema()
		}
		tools = append(tools, td)
	}
	return tools
}

func (sp *ScriptPlugin) Init() error {
	// Check requirements.
	for _, req := range sp.manifest.Requirements {
		if req.Command == "" {
			continue
		}
		_, err := exec.LookPath(req.Command)
		if err != nil && !req.Optional {
			return fmt.Errorf("missing requirement %s: %s", req.Name, req.InstallHint)
		}
	}
	return nil
}

func (sp *ScriptPlugin) ExecuteTool(ctx context.Context, toolName string, input json.RawMessage) (*ToolResult, error) {
	scriptPath, ok := sp.scripts[toolName]
	if !ok {
		return nil, fmt.Errorf("script plugin %s: tool %q not found", sp.manifest.Name, toolName)
	}

	ctx, cancel := context.WithTimeout(ctx, sp.timeout)
	defer cancel()

	// Build environment: inherit OS env + plugin env + input.
	env := os.Environ()
	env = append(env, fmt.Sprintf("GOBOTICUS_INPUT=%s", string(input)))
	for k, v := range sp.env {
		env = append(env, k+"="+v)
	}

	// Use the injected ProcessRunner if available, otherwise fall back to OS runner.
	runner := sp.runner
	if runner == nil {
		runner = core.OSProcessRunner{}
	}

	stdout, stderr, err := runner.Run(ctx, scriptPath, nil, sp.dir, env)

	// Truncate output at 10MB.
	const maxOutput = 10 * 1024 * 1024
	output := string(stdout)
	if len(output) > maxOutput {
		output = output[:maxOutput] + "\n...[truncated]"
	}

	if err != nil {
		return &ToolResult{
			Success: false,
			Output:  fmt.Sprintf("error: %v\nstderr: %s", err, string(stderr)),
		}, nil
	}

	return &ToolResult{
		Success: true,
		Output:  output,
	}, nil
}

func (sp *ScriptPlugin) Shutdown() error { return nil }

// WithTimeout sets a custom execution timeout.
func (sp *ScriptPlugin) WithTimeout(timeout time.Duration) *ScriptPlugin {
	sp.timeout = timeout
	return sp
}

// WithEnv adds environment variables for script execution.
func (sp *ScriptPlugin) WithEnv(env map[string]string) *ScriptPlugin {
	for k, v := range env {
		sp.env[k] = v
	}
	return sp
}

var scriptExtensions = []string{".sh", ".py", ".rb", ".js", ".go", ""}

func (sp *ScriptPlugin) discoverScripts() {
	for _, tool := range sp.manifest.Tools {
		if path := findScript(sp.dir, tool.Name); path != "" {
			sp.scripts[tool.Name] = path
		}
	}
}

func findScript(dir, toolName string) string {
	for _, ext := range scriptExtensions {
		path := filepath.Join(dir, toolName+ext)
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return path
		}
	}
	return ""
}

func defaultParameterSchema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"prompt": {"type": "string", "description": "Primary task"},
			"working_dir": {"type": "string", "description": "Working directory"},
			"task": {"type": "string", "description": "Alternate instruction field"}
		},
		"required": ["prompt"]
	}`)
}

// Hash returns the SHA-256 of the plugin directory for change detection.
func (sp *ScriptPlugin) Hash() (string, error) {
	return DirHash(sp.dir)
}

// --- Script validation ---

// ValidateManifest checks a manifest for common errors.
func ValidateManifest(m *Manifest) error {
	if m.Name == "" || !validPluginName.MatchString(m.Name) || len(m.Name) > 128 {
		return fmt.Errorf("invalid plugin name: %q", m.Name)
	}
	if m.Version == "" {
		return fmt.Errorf("plugin %s: missing version", m.Name)
	}
	if strings.Contains(m.Name, "..") || strings.Contains(m.Name, "/") {
		return fmt.Errorf("plugin name contains path traversal: %q", m.Name)
	}
	for _, tool := range m.Tools {
		if tool.Name == "" || !validPluginName.MatchString(tool.Name) {
			return fmt.Errorf("plugin %s: invalid tool name: %q", m.Name, tool.Name)
		}
	}
	return nil
}
