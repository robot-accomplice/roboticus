package daemon

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"roboticus/internal/agent/skills"
	"roboticus/internal/core"
	"roboticus/testutil"
)

func TestSkillInventoryReloadMakesNewSkillMatchable(t *testing.T) {
	store := testutil.TempStore(t)
	dir := t.TempDir()
	cfg := core.DefaultConfig()
	cfg.Agent.Workspace = filepath.Join(t.TempDir(), "workspace")
	cfg.Skills.Directory = dir

	matcher := skills.NewMatcher(nil)
	inventory := newSkillInventory(&cfg, store, skills.NewLoader(), matcher)
	if _, err := inventory.Reload(context.Background()); err != nil {
		t.Fatalf("initial reload: %v", err)
	}
	if got := matcher.Match("use fresh-skill now"); got != nil {
		t.Fatalf("unexpected match before file exists: %s", got.Name())
	}

	writeSkillFile(t, filepath.Join(dir, "fresh.md"), "fresh-skill", "fresh-skill")
	if _, err := inventory.Reload(context.Background()); err != nil {
		t.Fatalf("second reload: %v", err)
	}

	if got := matcher.Match("use fresh-skill now"); got == nil || got.Name() != "fresh-skill" {
		t.Fatalf("new skill was not matchable after reload: %#v", got)
	}

	var enabled bool
	if err := store.QueryRowContext(context.Background(), `SELECT enabled FROM skills WHERE name = 'fresh-skill'`).Scan(&enabled); err != nil {
		t.Fatalf("discovered skill not inserted into DB: %v", err)
	}
	if !enabled {
		t.Fatal("newly discovered file-backed skill should default enabled")
	}
}

func TestSkillInventoryReloadHonorsDisabledDBRows(t *testing.T) {
	store := testutil.TempStore(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "muted.md")
	writeSkillFile(t, path, "muted-skill", "muted")

	if _, err := store.ExecContext(context.Background(),
		`INSERT INTO skills (id, name, kind, source_path, content_hash, enabled, version, risk_level)
		 VALUES ('sk-muted', 'muted-skill', 'instruction', ?, 'old', 0, '1.0.0', 'Caution')`, path); err != nil {
		t.Fatalf("insert disabled skill: %v", err)
	}

	cfg := core.DefaultConfig()
	cfg.Agent.Workspace = filepath.Join(t.TempDir(), "workspace")
	cfg.Skills.Directory = dir

	matcher := skills.NewMatcher(nil)
	inventory := newSkillInventory(&cfg, store, skills.NewLoader(), matcher)
	if _, err := inventory.Reload(context.Background()); err != nil {
		t.Fatalf("reload: %v", err)
	}

	if got := matcher.Match("please use muted"); got != nil {
		t.Fatalf("disabled DB skill should not match after reload: %s", got.Name())
	}
	names := inventory.Names()
	for _, name := range names {
		if name == "muted-skill" {
			t.Fatal("disabled DB skill leaked into prompt-facing runtime names")
		}
	}
}

func writeSkillFile(t *testing.T, path, name, keyword string) {
	t.Helper()
	body := "---\nname: " + name + "\ntriggers:\n  keywords: [\"" + keyword + "\"]\n---\nSkill body."
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write skill file: %v", err)
	}
}
