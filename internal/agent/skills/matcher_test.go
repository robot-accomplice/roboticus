package skills

import (
	"os"
	"path/filepath"
	"testing"
)

// ---------------------------------------------------------------------------
// Matcher tests
// ---------------------------------------------------------------------------

func TestNewMatcher_Empty(t *testing.T) {
	m := NewMatcher(nil)
	if m == nil {
		t.Fatal("expected non-nil matcher")
	}
	if got := m.Match("anything"); got != nil {
		t.Errorf("expected nil match on empty matcher, got %v", got)
	}
}

func TestMatcher_Match_SingleKeyword(t *testing.T) {
	skill := &Skill{
		Manifest: Manifest{
			Name:     "greet",
			Triggers: Trigger{Keywords: []string{"hello"}},
			Priority: 5,
		},
	}
	m := NewMatcher([]*Skill{skill})

	// Exact match.
	if got := m.Match("hello"); got != skill {
		t.Error("expected match on exact keyword")
	}

	// Substring match.
	if got := m.Match("say hello world"); got != skill {
		t.Error("expected match on substring")
	}

	// No match.
	if got := m.Match("goodbye"); got != nil {
		t.Error("expected no match")
	}
}

func TestMatcher_Match_CaseInsensitive(t *testing.T) {
	skill := &Skill{
		Manifest: Manifest{
			Name:     "greet",
			Triggers: Trigger{Keywords: []string{"Hello"}},
			Priority: 5,
		},
	}
	m := NewMatcher([]*Skill{skill})

	if got := m.Match("HELLO"); got != skill {
		t.Error("expected case-insensitive match")
	}
	if got := m.Match("hello"); got != skill {
		t.Error("expected case-insensitive match on lowercase")
	}
}

func TestMatcher_Match_MultipleKeywords(t *testing.T) {
	skill := &Skill{
		Manifest: Manifest{
			Name:     "weather",
			Triggers: Trigger{Keywords: []string{"forecast", "weather", "temperature"}},
			Priority: 5,
		},
	}
	m := NewMatcher([]*Skill{skill})

	for _, input := range []string{"what's the forecast?", "weather report", "check temperature"} {
		if got := m.Match(input); got != skill {
			t.Errorf("expected match on %q", input)
		}
	}
}

func TestMatcher_Match_HighestPriorityWins(t *testing.T) {
	low := &Skill{
		Manifest: Manifest{
			Name:     "low",
			Triggers: Trigger{Keywords: []string{"deploy"}},
			Priority: 1,
		},
	}
	high := &Skill{
		Manifest: Manifest{
			Name:     "high",
			Triggers: Trigger{Keywords: []string{"deploy"}},
			Priority: 10,
		},
	}
	mid := &Skill{
		Manifest: Manifest{
			Name:     "mid",
			Triggers: Trigger{Keywords: []string{"deploy"}},
			Priority: 5,
		},
	}

	// Test regardless of insertion order.
	m := NewMatcher([]*Skill{low, high, mid})
	if got := m.Match("deploy now"); got != high {
		t.Errorf("expected highest priority skill, got %v", got.Name())
	}

	// Reversed order.
	m2 := NewMatcher([]*Skill{mid, high, low})
	if got := m2.Match("deploy now"); got != high {
		t.Errorf("expected highest priority (reversed), got %v", got.Name())
	}
}

func TestMatcher_Match_MultipleSkillsDifferentKeywords(t *testing.T) {
	greet := &Skill{
		Manifest: Manifest{
			Name:     "greet",
			Triggers: Trigger{Keywords: []string{"hello"}},
			Priority: 5,
		},
	}
	weather := &Skill{
		Manifest: Manifest{
			Name:     "weather",
			Triggers: Trigger{Keywords: []string{"forecast"}},
			Priority: 5,
		},
	}
	m := NewMatcher([]*Skill{greet, weather})

	if got := m.Match("hello there"); got != greet {
		t.Error("expected greet to match")
	}
	if got := m.Match("show the forecast"); got != weather {
		t.Error("expected weather to match")
	}
	if got := m.Match("random text"); got != nil {
		t.Error("expected no match")
	}
}

func TestMatcher_Match_NoTriggers(t *testing.T) {
	skill := &Skill{
		Manifest: Manifest{
			Name:     "no-triggers",
			Triggers: Trigger{Keywords: nil},
			Priority: 5,
		},
	}
	m := NewMatcher([]*Skill{skill})
	if got := m.Match("anything"); got != nil {
		t.Error("skill with no triggers should never match")
	}
}

func TestMatcher_SetSkills(t *testing.T) {
	original := &Skill{
		Manifest: Manifest{
			Name:     "original",
			Triggers: Trigger{Keywords: []string{"orig"}},
			Priority: 5,
		},
	}
	replacement := &Skill{
		Manifest: Manifest{
			Name:     "replacement",
			Triggers: Trigger{Keywords: []string{"new"}},
			Priority: 5,
		},
	}

	m := NewMatcher([]*Skill{original})
	if got := m.Match("orig"); got != original {
		t.Fatal("expected original match")
	}

	m.SetSkills([]*Skill{replacement})

	if got := m.Match("orig"); got != nil {
		t.Error("original skill should no longer match after SetSkills")
	}
	if got := m.Match("new"); got != replacement {
		t.Error("replacement skill should match after SetSkills")
	}
}

func TestMatcher_SetSkills_Nil(t *testing.T) {
	skill := &Skill{
		Manifest: Manifest{
			Name:     "test",
			Triggers: Trigger{Keywords: []string{"kw"}},
			Priority: 5,
		},
	}
	m := NewMatcher([]*Skill{skill})
	m.SetSkills(nil)
	if got := m.Match("kw"); got != nil {
		t.Error("expected no match after clearing skills")
	}
}

// ---------------------------------------------------------------------------
// Additional loader edge cases not covered by existing tests
// ---------------------------------------------------------------------------

func TestLoader_UnclosedFrontmatter(t *testing.T) {
	dir := t.TempDir()
	content := "---\nname: broken\n"
	_ = os.WriteFile(filepath.Join(dir, "bad.md"), []byte(content), 0644)

	sl := NewLoader()
	skills := sl.LoadFromDir(dir)
	if len(skills) != 0 {
		t.Errorf("expected 0 skills for unclosed frontmatter, got %d", len(skills))
	}
}

func TestLoader_InvalidYAMLFrontmatter(t *testing.T) {
	dir := t.TempDir()
	content := "---\n: invalid: yaml: [broken\n---\nbody"
	_ = os.WriteFile(filepath.Join(dir, "bad.md"), []byte(content), 0644)

	sl := NewLoader()
	skills := sl.LoadFromDir(dir)
	if len(skills) != 0 {
		t.Errorf("expected 0 skills for invalid YAML, got %d", len(skills))
	}
}

func TestLoader_InvalidYAMLStructured(t *testing.T) {
	dir := t.TempDir()
	content := ": invalid: yaml: [broken"
	_ = os.WriteFile(filepath.Join(dir, "bad.yaml"), []byte(content), 0644)

	sl := NewLoader()
	skills := sl.LoadFromDir(dir)
	if len(skills) != 0 {
		t.Errorf("expected 0 skills for invalid structured YAML, got %d", len(skills))
	}
}

func TestLoader_TOMLExtension(t *testing.T) {
	dir := t.TempDir()
	// TOML files are parsed by the yaml parser too, so valid YAML works.
	content := "name: toml-skill\ndescription: loaded via .toml\n"
	_ = os.WriteFile(filepath.Join(dir, "skill.toml"), []byte(content), 0644)

	sl := NewLoader()
	skills := sl.LoadFromDir(dir)
	if len(skills) != 1 {
		t.Fatalf("expected 1 skill from .toml, got %d", len(skills))
	}
	if skills[0].Type != Structured {
		t.Errorf("type = %v, want Structured", skills[0].Type)
	}
	if skills[0].Name() != "toml-skill" {
		t.Errorf("name = %q, want toml-skill", skills[0].Name())
	}
}

func TestLoader_YMLExtension(t *testing.T) {
	dir := t.TempDir()
	content := "name: yml-skill\n"
	_ = os.WriteFile(filepath.Join(dir, "skill.yml"), []byte(content), 0644)

	sl := NewLoader()
	skills := sl.LoadFromDir(dir)
	if len(skills) != 1 {
		t.Fatalf("expected 1 skill from .yml, got %d", len(skills))
	}
	if skills[0].Name() != "yml-skill" {
		t.Errorf("name = %q", skills[0].Name())
	}
}

func TestLoader_StructuredWithToolChain(t *testing.T) {
	dir := t.TempDir()
	content := `name: chain-skill
description: skill with tool chain
version: "1.0"
triggers:
  keywords:
    - chain
paired_tool: my_tool
tool_chain:
  - tool: step1
    params:
      query: "{{input}}"
  - tool: step2
    params:
      data: "{{prev_result}}"
`
	_ = os.WriteFile(filepath.Join(dir, "chain.yaml"), []byte(content), 0644)

	sl := NewLoader()
	skills := sl.LoadFromDir(dir)
	if len(skills) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(skills))
	}
	s := skills[0]
	if s.Manifest.PairedTool != "my_tool" {
		t.Errorf("paired_tool = %q, want my_tool", s.Manifest.PairedTool)
	}
	if len(s.Manifest.ToolChain) != 2 {
		t.Fatalf("tool_chain length = %d, want 2", len(s.Manifest.ToolChain))
	}
	if s.Manifest.ToolChain[0].ToolName != "step1" {
		t.Errorf("step[0].tool = %q, want step1", s.Manifest.ToolChain[0].ToolName)
	}
	if s.Manifest.ToolChain[0].Params["query"] != "{{input}}" {
		t.Errorf("step[0].params.query = %q", s.Manifest.ToolChain[0].Params["query"])
	}
	if s.Manifest.ToolChain[1].ToolName != "step2" {
		t.Errorf("step[1].tool = %q, want step2", s.Manifest.ToolChain[1].ToolName)
	}
}

func TestLoader_InstructionSkillAllFields(t *testing.T) {
	dir := t.TempDir()
	content := `---
name: full-skill
description: fully specified
version: "3.0"
author: tester
triggers:
  keywords:
    - alpha
    - beta
priority: 8
paired_tool: companion
---
# Instructions

Do the thing.
`
	_ = os.WriteFile(filepath.Join(dir, "full.md"), []byte(content), 0644)

	sl := NewLoader()
	skills := sl.LoadFromDir(dir)
	if len(skills) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(skills))
	}
	s := skills[0]
	if s.Manifest.Version != "3.0" {
		t.Errorf("version = %q", s.Manifest.Version)
	}
	if s.Manifest.Author != "tester" {
		t.Errorf("author = %q", s.Manifest.Author)
	}
	if s.Manifest.PairedTool != "companion" {
		t.Errorf("paired_tool = %q", s.Manifest.PairedTool)
	}
	if s.Manifest.Priority != 8 {
		t.Errorf("priority = %d", s.Manifest.Priority)
	}
	if s.SourcePath != filepath.Join(dir, "full.md") {
		t.Errorf("source_path = %q", s.SourcePath)
	}
}

func TestLoader_MixedValidAndInvalid(t *testing.T) {
	dir := t.TempDir()
	// One valid, one invalid, one unsupported.
	_ = os.WriteFile(filepath.Join(dir, "good.md"), []byte("---\nname: good\n---\nbody"), 0644)
	_ = os.WriteFile(filepath.Join(dir, "bad.md"), []byte("no frontmatter"), 0644)
	_ = os.WriteFile(filepath.Join(dir, "skip.txt"), []byte("ignored"), 0644)

	sl := NewLoader()
	skills := sl.LoadFromDir(dir)
	if len(skills) != 1 {
		t.Errorf("expected 1 valid skill, got %d", len(skills))
	}
}

func TestSkill_NameAndTriggers(t *testing.T) {
	s := &Skill{
		Manifest: Manifest{
			Name:     "test",
			Triggers: Trigger{Keywords: []string{"a", "b"}},
		},
	}
	if s.Name() != "test" {
		t.Errorf("Name() = %q", s.Name())
	}
	if len(s.Triggers()) != 2 || s.Triggers()[0] != "a" {
		t.Errorf("Triggers() = %v", s.Triggers())
	}
}

func TestSkill_EmptyTriggers(t *testing.T) {
	s := &Skill{Manifest: Manifest{Name: "empty"}}
	if s.Triggers() != nil {
		t.Errorf("expected nil triggers, got %v", s.Triggers())
	}
}
