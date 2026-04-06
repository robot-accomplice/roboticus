package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"roboticus/internal/core"
)

func TestExportWorkspaceWritesManifestAndArtifacts(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cfgDir := core.ConfigDir()
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}

	cfg := core.DefaultConfig()
	cfg.Database.Path = filepath.Join(home, "data", "roboticus.db")
	cfg.Skills.Directory = filepath.Join(home, "skills")
	cfg.Personality.OSPath = filepath.Join(home, "personality", "os.md")
	cfg.Personality.FirmwarePath = filepath.Join(home, "personality", "firmware.md")
	cfg.Personality.OperatorPath = filepath.Join(home, "personality", "operator.md")

	mustWriteFile(t, core.ConfigFilePath(), []byte("name = 'roboticus'\n"))
	mustWriteFile(t, filepath.Join(cfgDir, "providers.toml"), []byte("[providers]\n"))
	mustWriteFile(t, filepath.Join(cfgDir, "keystore.enc"), []byte("encrypted"))
	mustWriteFile(t, cfg.Database.Path, []byte("sqlite-data"))
	mustWriteFile(t, filepath.Join(cfg.Skills.Directory, "greet.toml"), []byte("name='greet'\n"))
	mustWriteFile(t, cfg.Personality.OSPath, []byte("os"))
	mustWriteFile(t, cfg.Personality.FirmwarePath, []byte("firmware"))
	mustWriteFile(t, cfg.Personality.OperatorPath, []byte("operator"))

	target := filepath.Join(home, "export")
	if err := exportWorkspace(cfg, target, []string{"config", "personality", "skills", "sessions"}); err != nil {
		t.Fatalf("exportWorkspace: %v", err)
	}

	assertFileExists(t, filepath.Join(target, "roboticus.toml"))
	assertFileExists(t, filepath.Join(target, "providers.toml"))
	assertFileExists(t, filepath.Join(target, "keystore.enc"))
	assertFileExists(t, filepath.Join(target, "skills", "greet.toml"))
	assertFileExists(t, filepath.Join(target, "roboticus.db"))
	assertFileExists(t, filepath.Join(target, "personality", "os.md"))

	data, err := os.ReadFile(filepath.Join(target, "migration_manifest.json"))
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	var manifest migrationManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatalf("decode manifest: %v", err)
	}
	if len(manifest.Files) == 0 {
		t.Fatal("expected exported files in manifest")
	}
}

func TestImportWorkspaceCopiesArtifacts(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cfg := core.DefaultConfig()
	cfg.Database.Path = filepath.Join(home, "runtime", "roboticus.db")
	cfg.Skills.Directory = filepath.Join(home, "runtime-skills")
	cfg.Personality.OSPath = filepath.Join(home, "runtime", "os.md")
	cfg.Personality.FirmwarePath = filepath.Join(home, "runtime", "firmware.md")
	cfg.Personality.OperatorPath = filepath.Join(home, "runtime", "operator.md")

	source := filepath.Join(home, "import-source")
	mustWriteFile(t, filepath.Join(source, "roboticus.toml"), []byte("name = 'imported'\n"))
	mustWriteFile(t, filepath.Join(source, "providers.toml"), []byte("[providers]\n"))
	mustWriteFile(t, filepath.Join(source, "keystore.enc"), []byte("encrypted"))
	mustWriteFile(t, filepath.Join(source, "roboticus.db"), []byte("sqlite-data"))
	mustWriteFile(t, filepath.Join(source, "skills", "greet.toml"), []byte("name='greet'\n"))
	mustWriteFile(t, filepath.Join(source, "personality", "os.md"), []byte("os"))
	mustWriteFile(t, filepath.Join(source, "personality", "firmware.md"), []byte("firmware"))
	mustWriteFile(t, filepath.Join(source, "personality", "operator.md"), []byte("operator"))

	if err := importWorkspace(source, cfg, []string{"config", "personality", "skills", "sessions"}, true); err != nil {
		t.Fatalf("importWorkspace: %v", err)
	}

	assertFileExists(t, core.ConfigFilePath())
	assertFileExists(t, filepath.Join(core.ConfigDir(), "providers.toml"))
	assertFileExists(t, filepath.Join(core.ConfigDir(), "keystore.enc"))
	assertFileExists(t, cfg.Database.Path)
	assertFileExists(t, filepath.Join(cfg.Skills.Directory, "greet.toml"))
	assertFileExists(t, cfg.Personality.OSPath)
}

func TestImportWorkspaceRespectsOverwriteSafety(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cfg := core.DefaultConfig()
	cfg.Database.Path = filepath.Join(home, "runtime", "roboticus.db")
	source := filepath.Join(home, "import-source")
	mustWriteFile(t, filepath.Join(source, "roboticus.db"), []byte("sqlite-data"))
	mustWriteFile(t, cfg.Database.Path, []byte("existing"))

	err := importWorkspace(source, cfg, []string{"sessions"}, false)
	if err == nil {
		t.Fatal("expected overwrite safety error")
	}
}

func mustWriteFile(t *testing.T, path string, content []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func assertFileExists(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
}
