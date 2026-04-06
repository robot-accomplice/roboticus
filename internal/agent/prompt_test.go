package agent

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
)

func TestBuildSystemPrompt_ContainsAgentName(t *testing.T) {
	cfg := PromptConfig{AgentName: "Roboticus"}
	prompt := BuildSystemPrompt(cfg)
	if !strings.Contains(prompt, "Roboticus") {
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

func TestBuildSystemPrompt_SignedContainsBoundaryMarkers(t *testing.T) {
	cfg := PromptConfig{
		AgentName:   "TestBot",
		Firmware:    "Platform rules.",
		BoundaryKey: []byte("test-secret-key"),
	}
	prompt := BuildSystemPrompt(cfg)
	if !strings.Contains(prompt, "[BOUNDARY:") {
		t.Error("signed prompt should contain [BOUNDARY:...] markers")
	}
	// Should have one boundary per section. With AgentName + Firmware + Runtime +
	// ToolUse + Safety = 5 sections.
	count := strings.Count(prompt, "[BOUNDARY:")
	if count < 3 {
		t.Errorf("expected at least 3 boundary markers, got %d", count)
	}
}

func TestBuildSystemPrompt_UnsignedHasNoMarkers(t *testing.T) {
	cfg := PromptConfig{
		AgentName: "TestBot",
		Firmware:  "Rules.",
	}
	prompt := BuildSystemPrompt(cfg)
	if strings.Contains(prompt, "[BOUNDARY:") {
		t.Error("unsigned prompt (nil key) should not contain boundary markers")
	}
}

func TestBuildSystemPrompt_EmptyKeyNoMarkers(t *testing.T) {
	cfg := PromptConfig{
		AgentName:   "TestBot",
		BoundaryKey: []byte{},
	}
	prompt := BuildSystemPrompt(cfg)
	if strings.Contains(prompt, "[BOUNDARY:") {
		t.Error("empty key should not produce boundary markers")
	}
}

func TestSignBoundary_Deterministic(t *testing.T) {
	key := []byte("determinism-key")
	content := "Hello, world!"
	a := signBoundary(key, content)
	b := signBoundary(key, content)
	if a != b {
		t.Errorf("signBoundary should be deterministic: %q != %q", a, b)
	}
	// Verify format.
	if !strings.HasPrefix(a, "[BOUNDARY:") || !strings.HasSuffix(a, "]") {
		t.Errorf("unexpected format: %q", a)
	}
	// Verify the hex inside is a valid HMAC-SHA256 (64 hex chars).
	inner := a[len("[BOUNDARY:") : len(a)-1]
	if len(inner) != 64 {
		t.Errorf("expected 64 hex chars, got %d", len(inner))
	}
	decoded, err := hex.DecodeString(inner)
	if err != nil {
		t.Fatalf("hex decode failed: %v", err)
	}
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(content))
	expected := mac.Sum(nil)
	if !hmac.Equal(decoded, expected) {
		t.Error("HMAC mismatch in signBoundary output")
	}
}
