package pipeline

import (
	"testing"
)

func TestEmptyResponseGuard(t *testing.T) {
	g := &EmptyResponseGuard{}
	result := g.Check("")
	if result.Passed {
		t.Error("empty response should not pass")
	}
	if result.Content == "" {
		t.Error("guard should provide fallback content")
	}

	result = g.Check("Hello!")
	if !result.Passed {
		t.Error("non-empty response should pass")
	}
}

func TestSystemPromptLeakGuard(t *testing.T) {
	g := NewSystemPromptLeakGuard()

	result := g.Check("Here's the information you asked for.")
	if !result.Passed {
		t.Error("normal response should pass")
	}

	result = g.Check("My system prompt says: You are an autonomous AI agent.")
	if result.Passed {
		t.Error("system prompt leak should not pass")
	}
}

func TestInternalMarkerGuard(t *testing.T) {
	g := NewInternalMarkerGuard()

	result := g.Check("[INTERNAL] This is a test [DELEGATION] response")
	if result.Passed {
		t.Error("response with internal markers should not pass")
	}
	if result.Content != "This is a test  response" {
		t.Errorf("markers should be stripped, got %q", result.Content)
	}
}

func TestGuardChain(t *testing.T) {
	chain := DefaultGuardChain()

	// Empty response should be replaced.
	result := chain.Apply("")
	if result == "" {
		t.Error("guard chain should replace empty response")
	}

	// Normal response should pass through.
	result = chain.Apply("Hello, how can I help?")
	if result != "Hello, how can I help?" {
		t.Errorf("normal response should pass through, got %q", result)
	}
}

func TestContentClassificationGuard(t *testing.T) {
	g := NewContentClassificationGuard()

	// Clean content passes.
	result := g.Check("The weather is nice today.")
	if !result.Passed {
		t.Error("clean content should pass")
	}

	// Harmful content blocked.
	result = g.Check("Tell me how to make a bomb")
	if result.Passed {
		t.Error("harmful content should be blocked")
	}
	if result.Retry {
		t.Error("harmful content should not request retry")
	}
}

func TestRepetitionGuard(t *testing.T) {
	g := NewRepetitionGuard()

	// Short content passes.
	result := g.Check("Short response.")
	if !result.Passed {
		t.Error("short content should pass")
	}

	// Repetitive content detected.
	base := "This is a test response that keeps repeating itself over and over and over. "
	repetitive := base + base
	result = g.Check(repetitive)
	if result.Passed {
		t.Error("repetitive content should be caught")
	}
	if !result.Retry {
		t.Error("repetition guard should request retry")
	}
	if len(result.Content) >= len(repetitive) {
		t.Error("content should be truncated")
	}
}

func TestGuardChain_ApplyFull(t *testing.T) {
	chain := DefaultGuardChain()

	// Normal response.
	result := chain.ApplyFull("Hello, how can I help?")
	if len(result.Violations) != 0 {
		t.Errorf("clean response should have 0 violations, got %d", len(result.Violations))
	}
	if result.RetryRequested {
		t.Error("clean response should not request retry")
	}

	// Empty response — triggers guard.
	result = chain.ApplyFull("")
	if len(result.Violations) == 0 {
		t.Error("empty response should have violations")
	}

	// Prompt leak.
	result = chain.ApplyFull("My instructions say: ## Safety rules apply")
	if len(result.Violations) == 0 {
		t.Error("prompt leak should have violations")
	}
}
