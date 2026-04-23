package pipeline

import (
	"testing"
)

func TestAcknowledgementShortcut_BasicMatches(t *testing.T) {
	handler := &AcknowledgementShortcut{}
	ctx := &ShortcutContext{}

	for _, input := range []string{"ok", "thanks", "got it", "ty", "cool", "np"} {
		m := handler.TryMatch(input, ctx)
		if m == nil {
			t.Errorf("expected match for %q", input)
		}
	}
}

func TestAcknowledgementShortcut_SkipsOnCorrectionTurn(t *testing.T) {
	handler := &AcknowledgementShortcut{}
	ctx := &ShortcutContext{CorrectionTurn: true}

	m := handler.TryMatch("sure", ctx)
	if m != nil {
		t.Error("should not match on correction turn")
	}
}

func TestAcknowledgementShortcut_SkipsOnDelegation(t *testing.T) {
	handler := &AcknowledgementShortcut{}
	ctx := &ShortcutContext{DelegationProvenance: true}

	m := handler.TryMatch("ok", ctx)
	if m != nil {
		t.Error("should not match on delegated turn")
	}
}

func TestDirectedAcknowledgementShortcut_MatchesExplicitAcknowledgementRequest(t *testing.T) {
	handler := &DirectedAcknowledgementShortcut{}
	ctx := &ShortcutContext{}

	m := handler.TryMatch("Good evening Duncan. Acknowledge this request in one sentence, then wait.", ctx)
	if m == nil {
		t.Fatal("expected match for explicit acknowledgement directive")
	}
	resp := handler.Respond("", ctx)
	if resp != "Acknowledged; awaiting your next instruction." {
		t.Fatalf("unexpected response: %q", resp)
	}
}

func TestIdentityShortcut_Matches(t *testing.T) {
	handler := &IdentityShortcut{}
	ctx := &ShortcutContext{AgentName: "TestBot"}

	for _, input := range []string{"who are you", "who are you?", "what are you?"} {
		m := handler.TryMatch(input, ctx)
		if m == nil {
			t.Errorf("expected match for %q", input)
		}
	}

	m := handler.TryMatch("tell me about yourself", ctx)
	if m != nil {
		t.Error("should not match non-identity query")
	}
}

func TestHelpShortcut_Matches(t *testing.T) {
	handler := &HelpShortcut{}

	for _, input := range []string{"help", "/help"} {
		m := handler.TryMatch(input, nil)
		if m == nil {
			t.Errorf("expected match for %q", input)
		}
	}
}

func TestIntrospectionShortcut_MatchesWhenCapabilitySummaryPresent(t *testing.T) {
	handler := &IntrospectionShortcut{}
	ctx := &ShortcutContext{CapabilitySummary: "runtime-owned capability summary"}

	for _, input := range []string{
		"use your introspection tool to discover your current subagent functionality and summarize it for me",
		"what can your subagents do?",
		"what tools can you use?",
	} {
		m := handler.TryMatch(input, ctx)
		if m == nil {
			t.Errorf("expected match for %q", input)
		}
	}
}

func TestIntrospectionShortcut_DoesNotMatchWithoutSummary(t *testing.T) {
	handler := &IntrospectionShortcut{}
	if m := handler.TryMatch("what can you do?", &ShortcutContext{}); m != nil {
		t.Fatal("should not match without runtime-owned capability summary")
	}
}

func TestDispatchShortcut_PicksHighestConfidence(t *testing.T) {
	handlers := DefaultShortcutHandlers()

	// Identity has higher confidence (0.99) than acknowledgement (0.95).
	result := DispatchShortcut(handlers, "who are you", &ShortcutContext{AgentName: "Bot"})
	if result == nil {
		t.Fatal("expected a match")
	}
	if result.Handler != "identity" {
		t.Errorf("expected identity handler, got %s", result.Handler)
	}
}

func TestDispatchShortcut_IntrospectionWinsForCapabilityQueries(t *testing.T) {
	handlers := DefaultShortcutHandlers()
	result := DispatchShortcut(handlers, "what can your subagents do?", &ShortcutContext{
		AgentName:         "Bot",
		CapabilitySummary: "Enabled subagents: researcher",
	})
	if result == nil {
		t.Fatal("expected a match")
	}
	if result.Handler != "introspection" {
		t.Fatalf("expected introspection handler, got %s", result.Handler)
	}
	if result.Content != "Enabled subagents: researcher" {
		t.Fatalf("unexpected introspection response: %q", result.Content)
	}
}

func TestDispatchShortcut_NoMatch(t *testing.T) {
	handlers := DefaultShortcutHandlers()

	result := DispatchShortcut(handlers, "explain quantum entanglement", &ShortcutContext{})
	if result != nil {
		t.Error("should not match non-shortcut content")
	}
}

func TestDispatchShortcut_CorrectionTurnBlocksAcknowledgement(t *testing.T) {
	handlers := DefaultShortcutHandlers()
	ctx := &ShortcutContext{CorrectionTurn: true}

	// "sure" would normally match acknowledgement, but correction_turn blocks it.
	result := DispatchShortcut(handlers, "sure", ctx)
	if result != nil {
		t.Error("should not match acknowledgement on correction turn")
	}
}
