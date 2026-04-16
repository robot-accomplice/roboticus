package configcmd

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestInitCmd_CreatesWorkspaceDir is the v1.0.6 P2-E regression. The
// default config TOML writes `workspace = "~/.roboticus/workspace"`,
// but pre-fix init only created skills/plugins/data subdirs — the
// workspace directory the config referenced didn't exist on disk after
// init completed. A first-run operator would see a config file claim
// a workspace that wasn't actually there until the personality-setup
// flow lazily mkdir'd it.
//
// This test runs the init command's RunE against an isolated $HOME so
// it doesn't touch the operator's real ~/.roboticus. After init
// completes it asserts that every subdir the default TOML references
// (plus the base dirs) exists on disk.
func TestInitCmd_CreatesWorkspaceDir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("init command uses HOME/.roboticus; Windows uses USERPROFILE — covered by homeDir() but skipped here to avoid cross-platform drift in a single test")
	}

	// Isolate $HOME so core.ConfigDir() points into t.TempDir().
	// homeDir() prefers $HOME on non-Windows.
	isolated := t.TempDir()
	t.Setenv("HOME", isolated)
	// Some CI runners also set XDG_* — unset to keep the test
	// deterministic if any upstream library starts honoring them.
	t.Setenv("XDG_CONFIG_HOME", "")

	// Execute init's RunE directly. The command struct's RunE is the
	// same entry point Cobra dispatches.
	if err := initCmd.RunE(initCmd, []string{}); err != nil {
		t.Fatalf("init RunE: %v", err)
	}

	// Every dir we expect must exist — including the workspace dir
	// that pre-v1.0.6 was silently missing.
	expectedDirs := []string{
		filepath.Join(isolated, ".roboticus"),
		filepath.Join(isolated, ".roboticus", "skills"),
		filepath.Join(isolated, ".roboticus", "plugins"),
		filepath.Join(isolated, ".roboticus", "data"),
		filepath.Join(isolated, ".roboticus", "workspace"), // the regression target
	}
	for _, d := range expectedDirs {
		info, err := os.Stat(d)
		if err != nil {
			t.Fatalf("expected %s to exist after init; got err=%v", d, err)
		}
		if !info.IsDir() {
			t.Fatalf("expected %s to be a directory; got mode=%v", d, info.Mode())
		}
	}

	// Config file must exist and reference the workspace path that
	// init actually created. Drift here would mean the TOML and
	// init-subdir list diverged again.
	configPath := filepath.Join(isolated, ".roboticus", "roboticus.toml")
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("expected config at %s; got err=%v", configPath, err)
	}
	if !strings.Contains(string(data), "workspace = \"~/.roboticus/workspace\"") {
		t.Fatalf("config TOML references a different workspace path than init creates; TOML content:\n%s", string(data))
	}
}

// TestInitCmd_IsIdempotent confirms running init twice doesn't error
// and doesn't overwrite an existing config file. This is a secondary
// safety property — operators sometimes re-run init after edits and
// the first-run protections should not destroy their changes.
func TestInitCmd_IsIdempotent(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("see TestInitCmd_CreatesWorkspaceDir for platform rationale")
	}
	isolated := t.TempDir()
	t.Setenv("HOME", isolated)
	t.Setenv("XDG_CONFIG_HOME", "")

	if err := initCmd.RunE(initCmd, []string{}); err != nil {
		t.Fatalf("first init: %v", err)
	}

	// Seed a local edit to the config.
	configPath := filepath.Join(isolated, ".roboticus", "roboticus.toml")
	const marker = "# operator_local_marker"
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if err := os.WriteFile(configPath, append(data, []byte("\n"+marker+"\n")...), 0o600); err != nil {
		t.Fatalf("edit config: %v", err)
	}

	// Second init should succeed AND preserve the local edit.
	if err := initCmd.RunE(initCmd, []string{}); err != nil {
		t.Fatalf("second init: %v", err)
	}
	after, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("re-read config: %v", err)
	}
	if !strings.Contains(string(after), marker) {
		t.Fatalf("second init overwrote operator edits; marker %q missing from post-init config", marker)
	}
}
