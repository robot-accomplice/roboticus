package skills

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoader_LoadFromDir_Empty(t *testing.T) {
	dir := t.TempDir()
	sl := NewLoader()
	skills := sl.LoadFromDir(dir)
	if len(skills) != 0 {
		t.Errorf("empty dir: expected 0 skills, got %d", len(skills))
	}
}

func TestLoader_LoadFromDir_NonexistentDir(t *testing.T) {
	sl := NewLoader()
	skills := sl.LoadFromDir("/nonexistent/path/to/skills")
	if skills != nil {
		t.Errorf("nonexistent dir: expected nil, got %v", skills)
	}
}

func TestLoader_LoadInstruction(t *testing.T) {
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

	sl := NewLoader()
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
	if s.Type != Instruction {
		t.Errorf("type = %v, want Instruction", s.Type)
	}
}

func TestLoader_LoadStructured(t *testing.T) {
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

	sl := NewLoader()
	skills := sl.LoadFromDir(dir)
	if len(skills) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(skills))
	}
	if skills[0].Type != Structured {
		t.Errorf("type = %v, want Structured", skills[0].Type)
	}
	if skills[0].Name() != "structured-skill" {
		t.Errorf("name = %q", skills[0].Name())
	}
}

func TestLoader_Subdirectories(t *testing.T) {
	dir := t.TempDir()

	// Root level skill.
	_ = os.WriteFile(filepath.Join(dir, "root.md"), []byte("---\nname: root\n---\nRoot body"), 0644)

	// Subdirectory skill.
	subdir := filepath.Join(dir, "custom")
	_ = os.MkdirAll(subdir, 0755)
	_ = os.WriteFile(filepath.Join(subdir, "sub.md"), []byte("---\nname: sub\n---\nSub body"), 0644)

	sl := NewLoader()
	skills := sl.LoadFromDir(dir)
	if len(skills) != 2 {
		t.Fatalf("expected 2 skills (recursive), got %d", len(skills))
	}
}

func TestLoader_UnsupportedExtension(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "readme.txt"), []byte("not a skill"), 0644)
	_ = os.WriteFile(filepath.Join(dir, "data.json"), []byte("{}"), 0644)

	sl := NewLoader()
	skills := sl.LoadFromDir(dir)
	if len(skills) != 0 {
		t.Errorf("expected 0 skills (unsupported extensions), got %d", len(skills))
	}
}

func TestLoader_MissingFrontmatter(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "bad.md"), []byte("No frontmatter here"), 0644)

	sl := NewLoader()
	skills := sl.LoadFromDir(dir)
	if len(skills) != 0 {
		t.Errorf("expected 0 skills (missing frontmatter), got %d", len(skills))
	}
}

func TestLoader_DefaultPriority(t *testing.T) {
	dir := t.TempDir()
	content := "---\nname: no-priority\n---\nBody"
	_ = os.WriteFile(filepath.Join(dir, "skill.md"), []byte(content), 0644)

	sl := NewLoader()
	skills := sl.LoadFromDir(dir)
	if len(skills) != 1 {
		t.Fatal("expected 1 skill")
	}
	if skills[0].Manifest.Priority != 5 {
		t.Errorf("default priority = %d, want 5", skills[0].Manifest.Priority)
	}
}

func TestLoader_HashChangesOnContent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "skill.md")
	sl := NewLoader()

	_ = os.WriteFile(path, []byte("---\nname: v1\n---\nVersion 1"), 0644)
	skills1 := sl.LoadFromDir(dir)

	_ = os.WriteFile(path, []byte("---\nname: v2\n---\nVersion 2"), 0644)
	skills2 := sl.LoadFromDir(dir)

	if skills1[0].Hash == skills2[0].Hash {
		t.Error("hash should change when content changes")
	}
}

func TestLoader_LoadFromPaths(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "good.md")
	bad := filepath.Join(dir, "bad.txt")
	_ = os.WriteFile(good, []byte("---\nname: loaded-from-path\n---\nBody"), 0644)
	_ = os.WriteFile(bad, []byte("not a skill"), 0644)

	sl := NewLoader()
	skills := sl.LoadFromPaths([]string{good, bad, good})
	if len(skills) != 1 {
		t.Fatalf("expected 1 loaded skill, got %d", len(skills))
	}
	if skills[0].Name() != "loaded-from-path" {
		t.Fatalf("name = %q, want loaded-from-path", skills[0].Name())
	}
}

func TestHashSkillContent(t *testing.T) {
	hash1 := HashSkillContent([]byte("hello world"))
	hash2 := HashSkillContent([]byte("hello world"))
	hash3 := HashSkillContent([]byte("different content"))

	if hash1 != hash2 {
		t.Error("same content should produce same hash")
	}
	if hash1 == hash3 {
		t.Error("different content should produce different hash")
	}
	if len(hash1) != 64 {
		t.Errorf("SHA-256 hex should be 64 chars, got %d", len(hash1))
	}
}

func TestLoadedSkillKind(t *testing.T) {
	if LoadedSkillStructured != 0 {
		t.Errorf("LoadedSkillStructured = %d, want 0", LoadedSkillStructured)
	}
	if LoadedSkillInstruction != 1 {
		t.Errorf("LoadedSkillInstruction = %d, want 1", LoadedSkillInstruction)
	}
}

func TestLoadedSkillFields(t *testing.T) {
	ls := LoadedSkill{
		Kind: LoadedSkillInstruction,
		Hash: "abc123",
		Path: "/tmp/skill.md",
	}
	if ls.Kind != LoadedSkillInstruction {
		t.Error("kind mismatch")
	}
	if ls.Hash != "abc123" {
		t.Error("hash mismatch")
	}
}

func TestLoadRecursive(t *testing.T) {
	dir1 := t.TempDir()
	dir2 := t.TempDir()

	_ = os.WriteFile(filepath.Join(dir1, "s1.md"), []byte("---\nname: skill1\n---\nBody1"), 0644)
	_ = os.WriteFile(filepath.Join(dir2, "s2.md"), []byte("---\nname: skill2\n---\nBody2"), 0644)

	skills := LoadRecursive(dir1, dir2)
	if len(skills) != 2 {
		t.Errorf("expected 2 skills from LoadRecursive, got %d", len(skills))
	}
}

func TestLoadRecursive_EmptyDirs(t *testing.T) {
	dir := t.TempDir()
	skills := LoadRecursive(dir)
	if len(skills) != 0 {
		t.Errorf("expected 0 skills, got %d", len(skills))
	}
}

func TestNewSkillLoader_LoadSkills(t *testing.T) {
	dir := t.TempDir()
	content := "---\nname: parity-skill\n---\nBody"
	_ = os.WriteFile(filepath.Join(dir, "skill.md"), []byte(content), 0644)

	loader := NewSkillLoader()
	if loader == nil {
		t.Fatal("expected loader")
	}

	skills := LoadSkills(dir)
	if len(skills) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(skills))
	}
	if skills[0].Name() != "parity-skill" {
		t.Fatalf("name = %q, want parity-skill", skills[0].Name())
	}
}
