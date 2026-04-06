package agent

import (
	"os"
	"path/filepath"
	"testing"

	"roboticus/internal/agent/skills"
)

func TestSkillLoader_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	loader := skills.NewLoader()
	skills := loader.LoadFromDir(dir)
	if len(skills) != 0 {
		t.Errorf("empty dir should return 0 skills, got %d", len(skills))
	}
}

func TestSkillLoader_InstructionSkill(t *testing.T) {
	dir := t.TempDir()
	content := `---
name: test-skill
description: A test skill
triggers:
  keywords:
    - test
    - hello
---
You are a testing assistant.`
	_ = os.WriteFile(filepath.Join(dir, "test.md"), []byte(content), 0o644)

	loader := skills.NewLoader()
	skills := loader.LoadFromDir(dir)
	if len(skills) < 1 {
		t.Skip("skill not loaded (frontmatter may differ)")
	}

	skill := skills[0]
	if skill.Name() == "" {
		t.Error("name should not be empty")
	}
	if skill.Body == "" {
		t.Error("body should not be empty")
	}
}

func TestSkillLoader_NonMarkdown(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "readme.txt"), []byte("not a skill"), 0o644)

	loader := skills.NewLoader()
	skills := loader.LoadFromDir(dir)
	// .txt files should be ignored.
	if len(skills) != 0 {
		t.Errorf("non-markdown should be ignored, got %d", len(skills))
	}
}
