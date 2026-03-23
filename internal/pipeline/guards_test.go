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
