package agent

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSkillLoader_LoadFromDir_Empty(t *testing.T) {
	dir := t.TempDir()
	sl := NewSkillLoader()
	skills := sl.LoadFromDir(dir)
	if len(skills) != 0 {
		t.Errorf("empty dir: expected 0 skills, got %d", len(skills))
	}
}

func TestSkillLoader_LoadFromDir_NonexistentDir(t *testing.T) {
	sl := NewSkillLoader()
	skills := sl.LoadFromDir("/nonexistent/path/to/skills")
	if skills != nil {
		t.Errorf("nonexistent dir: expected nil, got %v", skills)
	}
}

func TestSkillLoader_LoadInstruction(t *testing.T) {
	dir := t.TempDir()
	content := `---
name: test-skill
description: A test skill
version: "1.0"
triggers:
  keywords:
    - test
    - demo
priority: 3
---
This is the instruction body for the test skill.

It can contain multiple paragraphs and **markdown**.
`
	path := filepath.Join(dir, "test.md")
	_ = os.WriteFile(path, []byte(content), 0644)

	sl := NewSkillLoader()
	skills := sl.LoadFromDir(dir)
	if len(skills) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(skills))
	}

	s := skills[0]
	if s.Name() != "test-skill" {
		t.Errorf("name = %q, want test-skill", s.Name())
	}
	if s.Manifest.Description != "A test skill" {
		t.Errorf("description = %q", s.Manifest.Description)
	}
	if s.Manifest.Priority != 3 {
		t.Errorf("priority = %d, want 3", s.Manifest.Priority)
	}
	if len(s.Triggers()) != 2 {
		t.Errorf("triggers = %v, want [test demo]", s.Triggers())
	}
	if s.Body == "" {
		t.Error("body should not be empty")
	}
	if s.Hash == "" {
		t.Error("hash should not be empty")
	}
	if s.Type != SkillInstruction {
		t.Errorf("type = %v, want SkillInstruction", s.Type)
	}
}

func TestSkillLoader_LoadStructured(t *testing.T) {
	dir := t.TempDir()
	content := `name: structured-skill
description: A YAML skill
version: "2.0"
triggers:
  keywords:
    - yaml
`
	path := filepath.Join(dir, "structured.yaml")
	_ = os.WriteFile(path, []byte(content), 0644)

	sl := NewSkillLoader()
	skills := sl.LoadFromDir(dir)
	if len(skills) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(skills))
	}
	if skills[0].Type != SkillStructured {
		t.Errorf("type = %v, want SkillStructured", skills[0].Type)
	}
	if skills[0].Name() != "structured-skill" {
		t.Errorf("name = %q", skills[0].Name())
	}
}

func TestSkillLoader_Subdirectories(t *testing.T) {
	dir := t.TempDir()

	// Root level skill.
	_ = os.WriteFile(filepath.Join(dir, "root.md"), []byte("---\nname: root\n---\nRoot body"), 0644)

	// Subdirectory skill.
	subdir := filepath.Join(dir, "custom")
	_ = os.MkdirAll(subdir, 0755)
	_ = os.WriteFile(filepath.Join(subdir, "sub.md"), []byte("---\nname: sub\n---\nSub body"), 0644)

	sl := NewSkillLoader()
	skills := sl.LoadFromDir(dir)
	if len(skills) != 2 {
		t.Fatalf("expected 2 skills (recursive), got %d", len(skills))
	}
}

func TestSkillLoader_UnsupportedExtension(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "readme.txt"), []byte("not a skill"), 0644)
	_ = os.WriteFile(filepath.Join(dir, "data.json"), []byte("{}"), 0644)

	sl := NewSkillLoader()
	skills := sl.LoadFromDir(dir)
	if len(skills) != 0 {
		t.Errorf("expected 0 skills (unsupported extensions), got %d", len(skills))
	}
}

func TestSkillLoader_MissingFrontmatter(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "bad.md"), []byte("No frontmatter here"), 0644)

	sl := NewSkillLoader()
	skills := sl.LoadFromDir(dir)
	if len(skills) != 0 {
		t.Errorf("expected 0 skills (missing frontmatter), got %d", len(skills))
	}
}

func TestSkillLoader_DefaultPriority(t *testing.T) {
	dir := t.TempDir()
	content := "---\nname: no-priority\n---\nBody"
	_ = os.WriteFile(filepath.Join(dir, "skill.md"), []byte(content), 0644)

	sl := NewSkillLoader()
	skills := sl.LoadFromDir(dir)
	if len(skills) != 1 {
		t.Fatal("expected 1 skill")
	}
	if skills[0].Manifest.Priority != 5 {
		t.Errorf("default priority = %d, want 5", skills[0].Manifest.Priority)
	}
}

func TestSkillLoader_HashChangesOnContent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "skill.md")
	sl := NewSkillLoader()

	_ = os.WriteFile(path, []byte("---\nname: v1\n---\nVersion 1"), 0644)
	skills1 := sl.LoadFromDir(dir)

	_ = os.WriteFile(path, []byte("---\nname: v2\n---\nVersion 2"), 0644)
	skills2 := sl.LoadFromDir(dir)

	if skills1[0].Hash == skills2[0].Hash {
		t.Error("hash should change when content changes")
	}
}
