package core

import (
	"encoding/json"
	"testing"
)

func TestSkillManifest_DefaultValues(t *testing.T) {
	m := DefaultSkillManifest()
	if m.Priority != 5 {
		t.Errorf("expected priority 5, got %d", m.Priority)
	}
	if m.RiskLevel != RiskLevelCaution {
		t.Errorf("expected RiskLevelCaution, got %v", m.RiskLevel)
	}
	if m.Version != "0.0.0" {
		t.Errorf("expected version 0.0.0, got %s", m.Version)
	}
	if m.Author != "local" {
		t.Errorf("expected author local, got %s", m.Author)
	}
}

func TestSkillManifest_JSONRoundTrip(t *testing.T) {
	m := SkillManifest{
		Name:        "test-skill",
		Description: "A test skill",
		Kind:        SkillKindStructured,
		Triggers: SkillTrigger{
			Keywords:  []string{"help", "assist"},
			ToolNames: []string{"web_search"},
		},
		Priority:  10,
		RiskLevel: RiskLevelSafe,
		Version:   "1.0.0",
		Author:    "tester",
	}
	data, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	var back SkillManifest
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatal(err)
	}
	if back.Name != "test-skill" {
		t.Errorf("expected test-skill, got %s", back.Name)
	}
	if len(back.Triggers.Keywords) != 2 {
		t.Errorf("expected 2 keywords, got %d", len(back.Triggers.Keywords))
	}
}

func TestInstructionSkill_DefaultValues(t *testing.T) {
	s := DefaultInstructionSkill()
	if s.Priority != 5 {
		t.Errorf("expected priority 5, got %d", s.Priority)
	}
}

func TestInstructionSkill_JSONRoundTrip(t *testing.T) {
	s := InstructionSkill{
		Name:        "help",
		Description: "Provides help",
		Triggers:    SkillTrigger{Keywords: []string{"help"}},
		Priority:    3,
		Body:        "Help text here",
		Version:     "0.1.0",
		Author:      "local",
	}
	data, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	var back InstructionSkill
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatal(err)
	}
	if back.Body != "Help text here" {
		t.Errorf("expected body 'Help text here', got %s", back.Body)
	}
}

func TestSkillKind_String(t *testing.T) {
	if SkillKindStructured.String() != "structured" {
		t.Error("expected 'structured'")
	}
	if SkillKindInstruction.String() != "instruction" {
		t.Error("expected 'instruction'")
	}
}

func TestSkillTrigger_EmptyDefault(t *testing.T) {
	trigger := SkillTrigger{}
	if len(trigger.Keywords) != 0 || len(trigger.ToolNames) != 0 || len(trigger.RegexPatterns) != 0 {
		t.Error("expected empty trigger fields")
	}
}
