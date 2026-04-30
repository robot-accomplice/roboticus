package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"roboticus/internal/core"
	"roboticus/internal/db"
)

// Tool is the interface every agent tool must implement.
type Tool interface {
	Name() string
	Description() string
	Risk() RiskLevel
	ParameterSchema() json.RawMessage
	Execute(ctx context.Context, params string, tctx *Context) (*Result, error)
}

// RiskLevel classifies how dangerous a tool invocation is.
type RiskLevel int

const (
	RiskSafe      RiskLevel = iota // No side effects
	RiskCaution                    // Reads data, may expose information
	RiskDangerous                  // Writes data, executes code
	RiskForbidden                  // Never allowed without explicit creator override
)

func (r RiskLevel) String() string {
	switch r {
	case RiskSafe:
		return "safe"
	case RiskCaution:
		return "caution"
	case RiskDangerous:
		return "dangerous"
	case RiskForbidden:
		return "forbidden"
	default:
		return "unknown"
	}
}

// Context provides runtime information to tool execution.
type Context struct {
	SessionID              string
	AgentID                string
	AgentName              string
	Workspace              string
	PathAnchor             string // optional per-turn anchor for relative read/inspection paths
	AllowedPaths           []string
	Channel                string
	ProtectedReadOnlyPaths []string
	Store                  *db.Store          // database access; may be nil in tests
	FS                     FileSystem         // file operations; nil defaults to OSFileSystem
	Runner                 core.ProcessRunner // subprocess execution; nil defaults to OSProcessRunner

	// MemoryBudgets holds configured budget percentages per tier (e.g., "working" -> 30).
	MemoryBudgets map[string]float64
}

// ResolveReadPath resolves read/inspection paths. Relative paths normally
// anchor to Workspace, but focused inspection turns may supply PathAnchor so
// follow-up paths such as "docs/" stay under the resolved project/root target.
func (c *Context) ResolveReadPath(path string) (string, error) {
	if c == nil {
		return "", fmt.Errorf("tool context is required")
	}
	root := c.Workspace
	if normalized, ok := c.normalizeAllowedPathAlias(path); ok {
		path = normalized
	}
	if c.PathAnchor != "" && !filepath.IsAbs(path) {
		root = c.PathAnchor
		path = normalizeAnchoredRelativePath(c.PathAnchor, path)
	}
	return ResolvePath(path, root, &ToolSandboxSnapshot{AllowedPaths: c.AllowedPaths})
}

// NormalizeAllowedFilesystemPathAliases canonicalizes operator-facing path
// shorthand before policy evaluation. It does not grant authority: aliases are
// rewritten only when their expanded absolute path is already covered by the
// workspace, active inspection root, or configured allowed_paths.
func (c *Context) NormalizeAllowedFilesystemPathAliases(toolName, args string) (string, bool) {
	if c == nil || !toolAcceptsFilesystemPathAlias(toolName) || strings.TrimSpace(args) == "" {
		return args, false
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(args), &fields); err != nil {
		return args, false
	}
	changed := false
	for _, key := range filesystemPathAliasKeys {
		raw, ok := fields[key]
		if !ok {
			continue
		}
		var value string
		if err := json.Unmarshal(raw, &value); err != nil {
			continue
		}
		normalized, ok := c.normalizeAllowedPathAlias(value)
		if !ok || normalized == value {
			continue
		}
		encoded, err := json.Marshal(normalized)
		if err != nil {
			continue
		}
		fields[key] = encoded
		changed = true
	}
	if !changed {
		return args, false
	}
	encoded, err := json.Marshal(fields)
	if err != nil {
		return args, false
	}
	return string(encoded), true
}

var filesystemPathAliasKeys = []string{"path", "root", "file", "filepath", "filename", "directory", "dir"}

func toolAcceptsFilesystemPathAlias(toolName string) bool {
	switch toolName {
	case "read_file", "write_file", "edit_file", "list_directory", "search_files", "glob_files", "inventory_projects":
		return true
	default:
		return false
	}
}

func (c *Context) normalizeAllowedPathAlias(path string) (string, bool) {
	trimmed := strings.TrimSpace(path)
	if !strings.HasPrefix(trimmed, "~") {
		return path, false
	}
	expanded, ok := expandHomePath(trimmed)
	if !ok || !c.pathAllowedForFilesystemAlias(expanded) {
		return path, false
	}
	return filepath.Clean(expanded), true
}

func (c *Context) pathAllowedForFilesystemAlias(path string) bool {
	if c == nil {
		return false
	}
	if c.PathAnchor != "" && pathWithinRoot(path, c.PathAnchor) {
		return true
	}
	if c.Workspace != "" && pathWithinRoot(path, c.Workspace) {
		return true
	}
	for _, allowed := range c.AllowedPaths {
		if pathWithinRoot(path, allowed) {
			return true
		}
	}
	return false
}

func expandHomePath(path string) (string, bool) {
	clean := strings.TrimSpace(path)
	if !strings.HasPrefix(clean, "~") {
		return clean, true
	}
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return "", false
	}
	if clean == "~" {
		return filepath.Clean(home), true
	}
	if strings.HasPrefix(clean, "~/") {
		return filepath.Clean(filepath.Join(home, strings.TrimPrefix(clean, "~/"))), true
	}
	return "", false
}

func pathWithinRoot(path, root string) bool {
	path = filepath.Clean(path)
	root = filepath.Clean(root)
	if path == "" || root == "" {
		return false
	}
	if strings.EqualFold(path, root) {
		return true
	}
	return strings.HasPrefix(strings.ToLower(path), strings.ToLower(root)+string(filepath.Separator))
}

func normalizeAnchoredRelativePath(anchor, path string) string {
	if strings.TrimSpace(anchor) == "" || strings.TrimSpace(path) == "" || filepath.IsAbs(path) {
		return path
	}
	anchorParts := pathParts(anchor)
	relParts := pathParts(path)
	if len(anchorParts) == 0 || len(relParts) == 0 {
		return path
	}
	maxSuffix := len(anchorParts)
	if len(relParts) < maxSuffix {
		maxSuffix = len(relParts)
	}
	for suffixLen := maxSuffix; suffixLen > 0; suffixLen-- {
		if !samePathParts(anchorParts[len(anchorParts)-suffixLen:], relParts[:suffixLen]) {
			continue
		}
		remaining := relParts[suffixLen:]
		if len(remaining) == 0 {
			return "."
		}
		return filepath.Join(remaining...)
	}
	return path
}

func pathParts(path string) []string {
	cleaned := filepath.Clean(filepath.FromSlash(strings.TrimSpace(path)))
	volume := filepath.VolumeName(cleaned)
	if volume != "" {
		cleaned = strings.TrimPrefix(cleaned, volume)
	}
	cleaned = strings.Trim(cleaned, string(filepath.Separator))
	if cleaned == "" || cleaned == "." {
		return nil
	}
	parts := strings.Split(cleaned, string(filepath.Separator))
	out := parts[:0]
	for _, part := range parts {
		if part != "" && part != "." {
			out = append(out, part)
		}
	}
	return out
}

func samePathParts(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// GetFS returns the filesystem, defaulting to real OS operations.
func (c *Context) GetFS() FileSystem {
	if c.FS != nil {
		return c.FS
	}
	return OSFileSystem{}
}

// GetRunner returns the process runner, defaulting to real OS execution.
func (c *Context) GetRunner() core.ProcessRunner {
	if c.Runner != nil {
		return c.Runner
	}
	return core.OSProcessRunner{}
}

// Result holds the output of a tool execution.
type Result struct {
	Output   string
	Metadata json.RawMessage // optional structured data
	Source   string          // "builtin", "plugin", "mcp" — for flight recorder
}
