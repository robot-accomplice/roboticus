package agent

import "testing"

func TestContextBuilder_SetSystemPrompt(t *testing.T) {
	cb := NewContextBuilder(DefaultContextConfig())
	// Just verify no panic.
	cb.SetSystemPrompt("You are a helpful assistant.")
}

func TestContextBuilder_SetTools(t *testing.T) {
	cb := NewContextBuilder(DefaultContextConfig())
	cb.SetTools(nil) // no panic with nil
}

func TestContextBuilder_SetMemory(t *testing.T) {
	cb := NewContextBuilder(DefaultContextConfig())
	cb.SetMemory("some memory context")
}

func TestBuildSystemPrompt(t *testing.T) {
	cfg := PromptConfig{
		AgentName: "TestBot",
		Version:   "1.0",
	}
	prompt := BuildSystemPrompt(cfg)
	if prompt == "" {
		t.Error("system prompt should not be empty")
	}
	if len(prompt) < 50 {
		t.Errorf("system prompt too short: %d chars", len(prompt))
	}
}

func TestDefaultContextConfig_TokenBudget(t *testing.T) {
	cfg := DefaultContextConfig()
	if cfg.MaxTokens <= 0 {
		t.Error("max tokens should be positive")
	}
}

func TestNewContextBuilder(t *testing.T) {
	cfg := DefaultContextConfig()
	cb := NewContextBuilder(cfg)
	if cb == nil {
		t.Fatal("should not be nil")
	}
}
