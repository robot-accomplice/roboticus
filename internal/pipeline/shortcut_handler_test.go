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
