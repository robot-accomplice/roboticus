package plugin

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"github.com/rs/zerolog/log"
)

// PluginStatus tracks a plugin's lifecycle state.
type PluginStatus string

const (
	StatusLoaded   PluginStatus = "loaded"
	StatusActive   PluginStatus = "active"
	StatusDisabled PluginStatus = "disabled"
	StatusError    PluginStatus = "error"
)

// ToolDef describes a tool provided by a plugin.
type ToolDef struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
	RiskLevel   string          `json:"risk_level"`
	Permissions []string        `json:"permissions,omitempty"`
}

// ToolResult is the outcome of a tool execution.
type ToolResult struct {
	Success  bool            `json:"success"`
	Output   string          `json:"output"`
	Metadata json.RawMessage `json:"metadata,omitempty"`
}

// Plugin is the interface every plugin must implement.
type Plugin interface {
	Name() string
	Version() string
	Tools() []ToolDef
	Init() error
	ExecuteTool(ctx context.Context, toolName string, input json.RawMessage) (*ToolResult, error)
	Shutdown() error
}

// Manifest describes a plugin's metadata (loaded from TOML/YAML).
type Manifest struct {
	Name            string          `json:"name" yaml:"name"`
	Version         string          `json:"version" yaml:"version"`
	Description     string          `json:"description" yaml:"description"`
	Author          string          `json:"author" yaml:"author"`
	Permissions     []string        `json:"permissions" yaml:"permissions"`
	TimeoutSeconds  int             `json:"timeout_seconds" yaml:"timeout_seconds"`
	Requirements    []Requirement   `json:"requirements" yaml:"requirements"`
	CompanionSkills []string        `json:"companion_skills" yaml:"companion_skills"`
	Tools           []ManifestTool  `json:"tools" yaml:"tools"`
}

// Requirement is an external dependency check.
type Requirement struct {
	Name        string `json:"name" yaml:"name"`
	Command     string `json:"command" yaml:"command"`       // checked via which/where
	InstallHint string `json:"install_hint" yaml:"install_hint"`
	Optional    bool   `json:"optional" yaml:"optional"`
}

// ManifestTool defines a tool in the manifest.
type ManifestTool struct {
	Name            string `json:"name" yaml:"name"`
	Description     string `json:"description" yaml:"description"`
	Dangerous       bool   `json:"dangerous" yaml:"dangerous"`
	Permissions     []string `json:"permissions" yaml:"permissions"`
	ParametersSchema string `json:"parameters_schema" yaml:"parameters_schema"` // JSON string
	PairedSkill     string `json:"paired_skill" yaml:"paired_skill"`
}

// --- Plugin Registry ---

// PermissionPolicy controls what plugins are allowed to do.
type PermissionPolicy struct {
	StrictMode  bool     `json:"strict_mode"`
	Allowed     []string `json:"allowed"`
}

type pluginEntry struct {
	plugin Plugin
	status PluginStatus
	hash   string // SHA-256 of plugin source for change detection
}

// Registry manages plugins with allow/deny lists and permission enforcement.
type Registry struct {
	mu         sync.RWMutex
	plugins    map[string]*pluginEntry
	allowList  map[string]bool
	denyList   map[string]bool
	policy     PermissionPolicy
}

// NewRegistry creates a plugin registry.
func NewRegistry(allowList, denyList []string, policy PermissionPolicy) *Registry {
	allow := make(map[string]bool, len(allowList))
	for _, name := range allowList {
		allow[name] = true
	}
	deny := make(map[string]bool, len(denyList))
	for _, name := range denyList {
		deny[name] = true
	}
	return &Registry{
		plugins:   make(map[string]*pluginEntry),
		allowList: allow,
		denyList:  deny,
		policy:    policy,
	}
}

var validPluginName = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// Register adds a plugin to the registry.
func (r *Registry) Register(p Plugin) error {
	name := p.Name()

	if !validPluginName.MatchString(name) || len(name) > 128 {
		return fmt.Errorf("plugin: invalid name: %s", name)
	}
	if r.denyList[name] {
		return fmt.Errorf("plugin: %s is denied", name)
	}
	if len(r.allowList) > 0 && !r.allowList[name] {
		return fmt.Errorf("plugin: %s not in allow list", name)
	}

	// Validate permissions in strict mode.
	if r.policy.StrictMode {
		for _, tool := range p.Tools() {
			for _, perm := range tool.Permissions {
				if !r.isPermissionAllowed(perm) {
					return fmt.Errorf("plugin: %s requires undeclared permission: %s", name, perm)
				}
			}
		}
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	r.plugins[name] = &pluginEntry{
		plugin: p,
		status: StatusLoaded,
	}
	log.Info().Str("plugin", name).Str("version", p.Version()).Msg("plugin registered")
	return nil
}

// InitAll initializes all registered plugins.
func (r *Registry) InitAll() []error {
	r.mu.Lock()
	defer r.mu.Unlock()

	var errs []error
	for name, entry := range r.plugins {
		if err := entry.plugin.Init(); err != nil {
			entry.status = StatusError
			errs = append(errs, fmt.Errorf("%s: %w", name, err))
			log.Warn().Str("plugin", name).Err(err).Msg("plugin init failed")
		} else {
			entry.status = StatusActive
		}
	}
	return errs
}

// ExecuteTool finds and executes a tool across all active plugins.
func (r *Registry) ExecuteTool(ctx context.Context, toolName string, input json.RawMessage) (*ToolResult, error) {
	r.mu.RLock()
	for _, entry := range r.plugins {
		if entry.status != StatusActive {
			continue
		}
		for _, tool := range entry.plugin.Tools() {
			if tool.Name == toolName {
				p := entry.plugin
				r.mu.RUnlock()
				return p.ExecuteTool(ctx, toolName, input)
			}
		}
	}
	r.mu.RUnlock()
	return nil, fmt.Errorf("plugin: tool %q not found", toolName)
}

// PluginInfo describes a registered plugin.
type PluginInfo struct {
	Name    string       `json:"name"`
	Version string       `json:"version"`
	Status  PluginStatus `json:"status"`
	Tools   []string     `json:"tools"`
}

// List returns info for all registered plugins.
func (r *Registry) List() []PluginInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var result []PluginInfo
	for _, entry := range r.plugins {
		info := PluginInfo{
			Name:    entry.plugin.Name(),
			Version: entry.plugin.Version(),
			Status:  entry.status,
		}
		for _, t := range entry.plugin.Tools() {
			info.Tools = append(info.Tools, t.Name)
		}
		result = append(result, info)
	}
	return result
}

// AllTools returns tool definitions from all active plugins.
func (r *Registry) AllTools() []ToolDef {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var tools []ToolDef
	for _, entry := range r.plugins {
		if entry.status == StatusActive {
			tools = append(tools, entry.plugin.Tools()...)
		}
	}
	return tools
}

// Enable activates a disabled plugin.
func (r *Registry) Enable(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	entry, ok := r.plugins[name]
	if !ok {
		return fmt.Errorf("plugin: %s not found", name)
	}
	entry.status = StatusActive
	return nil
}

// Disable deactivates a plugin.
func (r *Registry) Disable(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	entry, ok := r.plugins[name]
	if !ok {
		return fmt.Errorf("plugin: %s not found", name)
	}
	entry.status = StatusDisabled
	return nil
}

func (r *Registry) isPermissionAllowed(perm string) bool {
	lower := strings.ToLower(perm)
	for _, allowed := range r.policy.Allowed {
		if strings.ToLower(allowed) == lower {
			return true
		}
	}
	return false
}

// --- File hash for hot-reload ---

// FileHash returns the SHA-256 hex digest of a file.
func FileHash(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:]), nil
}

// DirHash returns a combined hash of all files in a directory.
func DirHash(dir string) (string, error) {
	h := sha256.New()
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		h.Write([]byte(path))
		h.Write(data)
		return nil
	})
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
