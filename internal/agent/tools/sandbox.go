package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// ToolSandboxSnapshot captures the sandbox constraints at the time a tool
// is invoked. Tools should consult this to enforce confinement.
type ToolSandboxSnapshot struct {
	AllowedPaths      []string
	MaxFileBytes      int64
	ReadOnly          bool
	ScriptConfinement bool
	NetworkAllowed    bool
}

// Sandbox limits.
const (
	MaxFileBytes     = 1 << 20 // 1 MiB
	MaxSearchResults = 100
	MaxWalkFiles     = 5000
)

// ResolvePath resolves a tool path against the workspace and optional allowed
// paths. Relative paths are anchored to the workspace. Absolute paths are only
// allowed when they fall under an explicitly allowed path. Home shortcuts are
// rejected to avoid shell/user-environment dependent expansion.
func ResolvePath(path, workspace string, snapshot *ToolSandboxSnapshot) (string, error) {
	if strings.HasPrefix(path, "~") {
		return "", fmt.Errorf("home-directory shortcuts are not allowed; use a workspace-relative path or an explicitly allowed absolute path")
	}

	if filepath.IsAbs(path) {
		cleanPath := canonicalSandboxPath(path)
		absWorkspace, err := filepath.Abs(workspace)
		if err != nil {
			return "", fmt.Errorf("invalid workspace: %w", err)
		}
		absWorkspace = canonicalSandboxPath(absWorkspace)
		if pathWithinSandboxRoot(cleanPath, absWorkspace) {
			return cleanPath, nil
		}
		if snapshot != nil {
			for _, allowed := range snapshot.AllowedPaths {
				cleanAllowed := canonicalSandboxPath(allowed)
				if pathWithinSandboxRoot(cleanPath, cleanAllowed) {
					return cleanPath, nil
				}
			}
		}
		return "", fmt.Errorf("absolute paths must be in allowed_paths list")
	}

	return NormalizeWorkspaceRelPath(path, workspace)
}

// ValidatePath checks that the given path is within the workspace and within
// any allowed paths defined by the sandbox snapshot. Returns an error if the
// path escapes confinement.
func ValidatePath(path, workspace string, snapshot *ToolSandboxSnapshot) error {
	if workspace == "" {
		return fmt.Errorf("workspace must not be empty")
	}

	cleaned, err := ResolvePath(path, workspace, snapshot)
	if err != nil {
		return err
	}

	// Absolute paths are already validated against the allowlist in ResolvePath.
	if filepath.IsAbs(path) {
		return nil
	}

	// Relative paths must stay within workspace.
	absWorkspace, err := filepath.Abs(workspace)
	if err != nil {
		return fmt.Errorf("invalid workspace: %w", err)
	}

	if !strings.HasPrefix(cleaned, absWorkspace+string(filepath.Separator)) && cleaned != absWorkspace {
		return fmt.Errorf("path %q escapes workspace %q", path, workspace)
	}

	if snapshot != nil && len(snapshot.AllowedPaths) > 0 {
		allowed := false
		for _, ap := range snapshot.AllowedPaths {
			absAP, err := filepath.Abs(ap)
			if err != nil {
				continue
			}
			absAP = canonicalSandboxPath(absAP)
			if pathWithinSandboxRoot(cleaned, absAP) {
				allowed = true
				break
			}
		}
		if !allowed {
			return fmt.Errorf("path %q not in allowed paths", path)
		}
	}

	return nil
}

// NormalizeWorkspaceRelPath resolves a path (absolute or relative) to an
// absolute clean path anchored within the workspace. Returns an error if the
// result would escape the workspace (e.g. via "../").
func NormalizeWorkspaceRelPath(path, workspace string) (string, error) {
	if workspace == "" {
		return "", fmt.Errorf("workspace must not be empty")
	}

	absWorkspace, err := filepath.Abs(workspace)
	if err != nil {
		return "", fmt.Errorf("invalid workspace: %w", err)
	}

	var abs string
	if filepath.IsAbs(path) {
		abs = filepath.Clean(path)
	} else {
		abs = filepath.Clean(filepath.Join(absWorkspace, path))
	}

	// Verify the resolved path doesn't escape the workspace.
	if !strings.HasPrefix(abs, absWorkspace+string(filepath.Separator)) && abs != absWorkspace {
		return "", fmt.Errorf("path %q resolves outside workspace %q", path, workspace)
	}

	return abs, nil
}

func canonicalSandboxPath(path string) string {
	cleaned := filepath.Clean(path)
	if abs, err := filepath.Abs(cleaned); err == nil {
		cleaned = abs
	}
	cleaned = canonicalizeSandboxAncestor(cleaned)
	return filepath.Clean(cleaned)
}

func canonicalizeSandboxAncestor(path string) string {
	cleaned := filepath.Clean(path)
	current := cleaned
	var suffix []string
	for {
		if resolved, err := filepath.EvalSymlinks(current); err == nil {
			canonical := filepath.Clean(resolved)
			for i := len(suffix) - 1; i >= 0; i-- {
				canonical = filepath.Join(canonical, suffix[i])
			}
			return canonical
		} else if !os.IsNotExist(err) {
			return cleaned
		}
		parent := filepath.Dir(current)
		if parent == current {
			return cleaned
		}
		suffix = append(suffix, filepath.Base(current))
		current = parent
	}
}

func pathWithinSandboxRoot(path, root string) bool {
	path = canonicalSandboxPath(path)
	root = canonicalSandboxPath(root)
	if sameSandboxPath(path, root) {
		return true
	}
	sepRoot := root + string(filepath.Separator)
	if sandboxPathCaseInsensitive() {
		return strings.HasPrefix(strings.ToLower(path), strings.ToLower(sepRoot))
	}
	return strings.HasPrefix(path, sepRoot)
}

func sameSandboxPath(a, b string) bool {
	if sandboxPathCaseInsensitive() {
		return strings.EqualFold(a, b)
	}
	return a == b
}

func sandboxPathCaseInsensitive() bool {
	switch runtime.GOOS {
	case "darwin", "windows":
		return true
	default:
		return false
	}
}
