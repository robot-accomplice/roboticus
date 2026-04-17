package tools

import (
	"os"
	"path/filepath"
	"testing"
)

func TestValidatePath(t *testing.T) {
	tmpDir := t.TempDir()

	tests := []struct {
		name      string
		path      string
		workspace string
		snapshot  *ToolSandboxSnapshot
		wantErr   bool
	}{
		{
			name:      "relative path within workspace",
			path:      "subdir/file.txt",
			workspace: tmpDir,
			wantErr:   false,
		},
		{
			name:      "absolute path within workspace",
			path:      filepath.Join(tmpDir, "subdir/file.txt"),
			workspace: tmpDir,
			wantErr:   false,
		},
		{
			name:      "path escapes workspace",
			path:      "../../../etc/passwd",
			workspace: tmpDir,
			wantErr:   true,
		},
		{
			name:      "absolute path outside workspace",
			path:      "/etc/passwd",
			workspace: tmpDir,
			wantErr:   true,
		},
		{
			name:      "workspace itself is valid",
			path:      ".",
			workspace: tmpDir,
			wantErr:   false,
		},
		{
			name:      "empty workspace errors",
			path:      "file.txt",
			workspace: "",
			wantErr:   true,
		},
		{
			name:      "allowed paths constraint - within",
			path:      filepath.Join(tmpDir, "allowed/file.txt"),
			workspace: tmpDir,
			snapshot: &ToolSandboxSnapshot{
				AllowedPaths: []string{filepath.Join(tmpDir, "allowed")},
			},
			wantErr: false,
		},
		{
			name:      "allowed paths constraint - outside",
			path:      filepath.Join(tmpDir, "forbidden/file.txt"),
			workspace: tmpDir,
			snapshot: &ToolSandboxSnapshot{
				AllowedPaths: []string{filepath.Join(tmpDir, "allowed")},
			},
			wantErr: true,
		},
		{
			name:      "no snapshot means no extra restriction",
			path:      filepath.Join(tmpDir, "any/path.txt"),
			workspace: tmpDir,
			snapshot:  nil,
			wantErr:   false,
		},
		{
			name:      "dot-dot segments cleaned still escaping",
			path:      filepath.Join(tmpDir, "sub/../../.."),
			workspace: tmpDir,
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidatePath(tt.path, tt.workspace, tt.snapshot)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidatePath() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestResolvePathAndValidatePath_ShareAllowedAbsoluteSemantics(t *testing.T) {
	tmpDir := t.TempDir()
	allowedDir := filepath.Join(tmpDir, "..", "external-allowed")
	target := filepath.Join(allowedDir, "notes.txt")
	snapshot := &ToolSandboxSnapshot{AllowedPaths: []string{allowedDir}}

	resolved, err := ResolvePath(target, tmpDir, snapshot)
	if err != nil {
		t.Fatalf("ResolvePath: %v", err)
	}
	if resolved != filepath.Clean(target) {
		t.Fatalf("resolved = %q, want %q", resolved, filepath.Clean(target))
	}

	if err := ValidatePath(target, tmpDir, snapshot); err != nil {
		t.Fatalf("ValidatePath: %v", err)
	}
}

func TestNormalizeWorkspaceRelPath(t *testing.T) {
	tmpDir := t.TempDir()

	tests := []struct {
		name      string
		path      string
		workspace string
		want      string
		wantErr   bool
	}{
		{
			name:      "relative path",
			path:      "subdir/file.txt",
			workspace: tmpDir,
			want:      filepath.Join(tmpDir, "subdir/file.txt"),
			wantErr:   false,
		},
		{
			name:      "absolute path in workspace",
			path:      filepath.Join(tmpDir, "file.txt"),
			workspace: tmpDir,
			want:      filepath.Join(tmpDir, "file.txt"),
			wantErr:   false,
		},
		{
			name:      "escaping relative path",
			path:      "../../etc/passwd",
			workspace: tmpDir,
			wantErr:   true,
		},
		{
			name:      "empty workspace",
			path:      "file.txt",
			workspace: "",
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NormalizeWorkspaceRelPath(tt.path, tt.workspace)
			if (err != nil) != tt.wantErr {
				t.Errorf("NormalizeWorkspaceRelPath() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("NormalizeWorkspaceRelPath() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSandboxConstants(t *testing.T) {
	if MaxFileBytes != 1<<20 {
		t.Errorf("MaxFileBytes = %d, want 1 MiB", MaxFileBytes)
	}
	if MaxSearchResults != 100 {
		t.Errorf("MaxSearchResults = %d, want 100", MaxSearchResults)
	}
	if MaxWalkFiles != 5000 {
		t.Errorf("MaxWalkFiles = %d, want 5000", MaxWalkFiles)
	}
}

func TestToolSandboxSnapshotFields(t *testing.T) {
	// Ensure ToolSandboxSnapshot is constructible with all fields.
	dir, _ := os.Getwd()
	s := ToolSandboxSnapshot{
		AllowedPaths:      []string{dir},
		MaxFileBytes:      MaxFileBytes,
		ReadOnly:          true,
		ScriptConfinement: true,
		NetworkAllowed:    false,
	}
	if !s.ReadOnly {
		t.Error("expected ReadOnly = true")
	}
	if s.MaxFileBytes != MaxFileBytes {
		t.Error("MaxFileBytes mismatch")
	}
}
