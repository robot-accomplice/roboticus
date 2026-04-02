package agent

import (
	"strings"
	"testing"
)

func TestBuildSystemPrompt_ContainsAgentName(t *testing.T) {
	cfg := PromptConfig{AgentName: "Goboticus"}
	prompt := BuildSystemPrompt(cfg)
	if !strings.Contains(prompt, "Goboticus") {
		t.Error("should contain agent name")
	}
}

func TestBuildSystemPrompt_ContainsVersion(t *testing.T) {
	cfg := PromptConfig{AgentName: "Bot", Version: "1.0.0"}
	prompt := BuildSystemPrompt(cfg)
	if !strings.Contains(prompt, "1.0.0") {
		t.Error("should contain version")
	}
}

func TestBuildSystemPrompt_ContainsFirmware(t *testing.T) {
	cfg := PromptConfig{AgentName: "Bot", Firmware: "Custom firmware text."}
	prompt := BuildSystemPrompt(cfg)
	if !strings.Contains(prompt, "Custom firmware text") {
		t.Error("should contain firmware")
	}
}

func TestBuildSystemPrompt_ContainsPersonality(t *testing.T) {
	cfg := PromptConfig{AgentName: "Bot", Personality: "Friendly and helpful."}
	prompt := BuildSystemPrompt(cfg)
	if !strings.Contains(prompt, "Friendly and helpful") {
		t.Error("should contain personality")
	}
}

func TestBuildSystemPrompt_Empty(t *testing.T) {
	cfg := PromptConfig{}
	prompt := BuildSystemPrompt(cfg)
	if prompt == "" {
		t.Error("should produce non-empty prompt even with empty config")
	}
}
