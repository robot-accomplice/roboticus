package tools

import (
	"fmt"
	"path/filepath"
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

// ValidatePath checks that the given path is within the workspace and within
// any allowed paths defined by the sandbox snapshot. Returns an error if the
// path escapes confinement.
func ValidatePath(path, workspace string, snapshot *ToolSandboxSnapshot) error {
	if workspace == "" {
		return fmt.Errorf("workspace must not be empty")
	}

	cleaned, err := NormalizeWorkspaceRelPath(path, workspace)
	if err != nil {
		return err
	}

	// Must be within workspace.
	absWorkspace, err := filepath.Abs(workspace)
	if err != nil {
		return fmt.Errorf("invalid workspace: %w", err)
	}

	if !strings.HasPrefix(cleaned, absWorkspace+string(filepath.Separator)) && cleaned != absWorkspace {
		return fmt.Errorf("path %q escapes workspace %q", path, workspace)
	}

	// If snapshot has allowed paths, enforce them.
	if snapshot != nil && len(snapshot.AllowedPaths) > 0 {
		allowed := false
		for _, ap := range snapshot.AllowedPaths {
			absAP, err := filepath.Abs(ap)
			if err != nil {
				continue
			}
			if strings.HasPrefix(cleaned, absAP+string(filepath.Separator)) || cleaned == absAP {
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
