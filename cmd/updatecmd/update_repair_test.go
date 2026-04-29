package updatecmd

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestRunInstallCleanup_ReconcilesUpdaterState(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	configPath := filepath.Join(home, ".roboticus", "roboticus.toml")
	providersPath := filepath.Join(home, ".roboticus", "providers.toml")
	skillsDir := filepath.Join(home, ".roboticus", "skills")
	if err := os.MkdirAll(skillsDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, []byte(""), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(providersPath, []byte("[providers.openai]\nurl = \"https://api.openai.com\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillsDir, "hello.md"), []byte("# Hello\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	summary, err := RunInstallCleanup(context.Background(), configPath, "v1.0.8")
	if err != nil {
		t.Fatalf("RunInstallCleanup: %v", err)
	}
	if !hasRepairAction(summary, "updater_state", "repaired") {
		t.Fatalf("expected updater_state repaired action, got %#v", summary.Actions)
	}

	second, err := RunInstallCleanup(context.Background(), configPath, "v1.0.8")
	if err != nil {
		t.Fatalf("RunInstallCleanup second pass: %v", err)
	}
	if !hasRepairAction(second, "updater_state", "skipped") {
		t.Fatalf("expected idempotent updater_state skipped action, got %#v", second.Actions)
	}
}

func TestCleanupStaleSidecars_RemovesOldFiles(t *testing.T) {
	dir := t.TempDir()
	execPath := filepath.Join(dir, "roboticus.exe")
	if err := os.WriteFile(execPath, []byte("current"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(execPath+".old", []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(execPath+".old-20260428-120000.000000", []byte("older"), 0o600); err != nil {
		t.Fatal(err)
	}

	var summary RepairSummary
	cleanupStaleSidecars(execPath, &summary)
	if !hasRepairAction(summary, "stale_sidecars", "repaired") {
		t.Fatalf("expected stale_sidecars repaired action, got %#v", summary.Actions)
	}
	if _, err := os.Stat(execPath + ".old"); !os.IsNotExist(err) {
		t.Fatalf("canonical sidecar still exists or unexpected stat error: %v", err)
	}
	if _, err := os.Stat(execPath + ".old-20260428-120000.000000"); !os.IsNotExist(err) {
		t.Fatalf("timestamped sidecar still exists or unexpected stat error: %v", err)
	}
}

func hasRepairAction(summary RepairSummary, name, status string) bool {
	for _, action := range summary.Actions {
		if action.Name == name && action.Status == status {
			return true
		}
	}
	return false
}
