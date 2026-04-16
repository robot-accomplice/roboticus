package updatecmd

import (
	"os"
	"path/filepath"
	"testing"
)

// TestCollectFirmwarePaths_PrefersWorkspaceOverConfigDir is the regression
// for the v1.0.6 audit finding that maintenance migration looked for
// FIRMWARE.toml in filepath.Dir(configPath) instead of cfg.Agent.Workspace.
// The personality setup flow (cmd/configcmd/setup.go) writes FIRMWARE.toml
// to cfg.Agent.Workspace — if the operator has a custom workspace, the
// pre-fix maintenance code silently skipped migration because the firmware
// file didn't exist at filepath.Dir(configPath).
//
// This test asserts that when a config with a custom workspace is provided,
// collectFirmwarePaths returns the workspace path FIRST. The legacy
// parent-of-config path remains as a secondary fallback so pre-workspace
// installs keep migrating.
func TestCollectFirmwarePaths_PrefersWorkspaceOverConfigDir(t *testing.T) {
	tmp := t.TempDir()
	configDir := filepath.Join(tmp, "config-home")
	workspaceDir := filepath.Join(tmp, "custom-workspace")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("mkdir configDir: %v", err)
	}
	if err := os.MkdirAll(workspaceDir, 0o755); err != nil {
		t.Fatalf("mkdir workspaceDir: %v", err)
	}

	configPath := filepath.Join(configDir, "roboticus.toml")
	// Minimal TOML that carries a non-default workspace.
	cfgContent := `
[agent]
id = "test-agent"
workspace = "` + workspaceDir + `"
`
	if err := os.WriteFile(configPath, []byte(cfgContent), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	paths := collectFirmwarePaths(configPath)
	if len(paths) < 1 {
		t.Fatalf("collectFirmwarePaths returned no candidates")
	}

	// Primary MUST be the workspace (where setup.go writes firmware).
	wantPrimary := filepath.Join(workspaceDir, "FIRMWARE.toml")
	if paths[0] != wantPrimary {
		t.Fatalf("primary firmware path = %q; want %q (workspace), got full list %v", paths[0], wantPrimary, paths)
	}

	// Legacy parent-of-config path should also appear as a secondary —
	// pre-workspace installs still get migrated.
	wantLegacy := filepath.Join(configDir, "FIRMWARE.toml")
	found := false
	for _, p := range paths {
		if p == wantLegacy {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("legacy firmware path %q missing from candidates %v; pre-workspace installs won't migrate", wantLegacy, paths)
	}
}

// TestCollectFirmwarePaths_DefaultWorkspaceNotDuplicated covers the case
// where workspace == filepath.Dir(configPath) — no duplicate candidates.
func TestCollectFirmwarePaths_DefaultWorkspaceNotDuplicated(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "roboticus.toml")
	// Workspace == config dir.
	cfgContent := `
[agent]
id = "test-agent"
workspace = "` + tmp + `"
`
	if err := os.WriteFile(configPath, []byte(cfgContent), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	paths := collectFirmwarePaths(configPath)
	seen := map[string]int{}
	for _, p := range paths {
		seen[p]++
	}
	for p, n := range seen {
		if n > 1 {
			t.Fatalf("duplicate firmware path in candidates: %s appears %d times", p, n)
		}
	}
}

// TestCollectFirmwarePaths_ConfigLoadFailureFallsBack ensures that if the
// config can't be parsed (e.g., first-run before init, or corrupt TOML),
// maintenance still finds firmware in the legacy location.
func TestCollectFirmwarePaths_ConfigLoadFailureFallsBack(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "roboticus.toml")
	// Intentionally do NOT create the config file.

	paths := collectFirmwarePaths(configPath)
	if len(paths) == 0 {
		t.Fatalf("expected fallback candidate even with missing config; got empty")
	}
	wantLegacy := filepath.Join(tmp, "FIRMWARE.toml")
	if paths[0] != wantLegacy {
		// Accept any position, but the legacy path MUST be present.
		found := false
		for _, p := range paths {
			if p == wantLegacy {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("legacy firmware path %q missing from fallback candidates %v", wantLegacy, paths)
		}
	}
}
