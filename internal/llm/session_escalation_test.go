package llm

import "testing"

func TestSessionEscalation_ConsecutiveFailures(t *testing.T) {
	tracker := NewSessionEscalationTracker([]string{"gpt-4o", "gpt-3.5-turbo", "local/qwen"})

	sid := "session-1"

	// First failure — not enough to escalate.
	tracker.RecordOutcome(sid, "local/qwen", false, 0.5)
	esc, _ := tracker.ShouldEscalate(sid)
	if esc {
		t.Error("should not escalate after 1 failure")
	}

	// Second consecutive failure — should escalate.
	tracker.RecordOutcome(sid, "local/qwen", false, 0.4)
	esc, model := tracker.ShouldEscalate(sid)
	if !esc {
		t.Error("should escalate after 2 consecutive failures")
	}
	if model != "gpt-3.5-turbo" {
		t.Errorf("expected gpt-3.5-turbo, got %q", model)
	}
}

func TestSessionEscalation_LowQuality(t *testing.T) {
	tracker := NewSessionEscalationTracker([]string{"gpt-4o", "gpt-3.5-turbo", "local/qwen"})

	sid := "session-2"

	// Three turns with quality < 0.3 but all "successful".
	tracker.RecordOutcome(sid, "local/qwen", true, 0.2)
	tracker.RecordOutcome(sid, "local/qwen", true, 0.1)

	esc, _ := tracker.ShouldEscalate(sid)
	if esc {
		t.Error("should not escalate after 2 low-quality turns")
	}

	tracker.RecordOutcome(sid, "local/qwen", true, 0.25)
	esc, model := tracker.ShouldEscalate(sid)
	if !esc {
		t.Error("should escalate after 3 low-quality turns")
	}
	if model != "gpt-3.5-turbo" {
		t.Errorf("expected gpt-3.5-turbo, got %q", model)
	}
}

func TestSessionEscalation_SuccessResetsFailures(t *testing.T) {
	tracker := NewSessionEscalationTracker([]string{"gpt-4o", "local/qwen"})

	sid := "session-3"

	tracker.RecordOutcome(sid, "local/qwen", false, 0.5)
	// A success should reset the consecutive failure counter.
	tracker.RecordOutcome(sid, "local/qwen", true, 0.8)

	esc, _ := tracker.ShouldEscalate(sid)
	if esc {
		t.Error("success should reset consecutive failures")
	}

	// One more failure after the success — still only 1.
	tracker.RecordOutcome(sid, "local/qwen", false, 0.5)
	esc, _ = tracker.ShouldEscalate(sid)
	if esc {
		t.Error("should not escalate after 1 failure post-reset")
	}
}

func TestSessionEscalation_GoodQualityResetsLowQuality(t *testing.T) {
	tracker := NewSessionEscalationTracker([]string{"gpt-4o", "local/qwen"})

	sid := "session-4"

	tracker.RecordOutcome(sid, "local/qwen", true, 0.2)
	tracker.RecordOutcome(sid, "local/qwen", true, 0.1)
	// Good quality turn resets the low-quality counter.
	tracker.RecordOutcome(sid, "local/qwen", true, 0.9)

	esc, _ := tracker.ShouldEscalate(sid)
	if esc {
		t.Error("good quality turn should reset low quality counter")
	}
}

func TestSessionEscalation_AlreadyAtTop(t *testing.T) {
	tracker := NewSessionEscalationTracker([]string{"gpt-4o", "gpt-3.5-turbo"})

	sid := "session-5"

	// Two failures on the primary model — no higher model to escalate to.
	tracker.RecordOutcome(sid, "gpt-4o", false, 0.5)
	tracker.RecordOutcome(sid, "gpt-4o", false, 0.5)

	esc, model := tracker.ShouldEscalate(sid)
	if esc {
		t.Errorf("should not escalate when already at top, got model=%q", model)
	}
}

func TestSessionEscalation_EmptyFallbacks(t *testing.T) {
	tracker := NewSessionEscalationTracker(nil)

	sid := "session-6"
	tracker.RecordOutcome(sid, "some-model", false, 0.1)
	tracker.RecordOutcome(sid, "some-model", false, 0.1)

	esc, _ := tracker.ShouldEscalate(sid)
	if esc {
		t.Error("should not escalate with empty fallback chain")
	}
}

func TestSessionEscalation_UnknownModelEscalatesToPrimary(t *testing.T) {
	tracker := NewSessionEscalationTracker([]string{"gpt-4o", "gpt-3.5-turbo"})

	sid := "session-7"

	// Model not in chain — should suggest the primary.
	tracker.RecordOutcome(sid, "unknown-model", false, 0.5)
	tracker.RecordOutcome(sid, "unknown-model", false, 0.5)

	esc, model := tracker.ShouldEscalate(sid)
	if !esc {
		t.Error("should escalate unknown model to primary")
	}
	if model != "gpt-4o" {
		t.Errorf("expected gpt-4o, got %q", model)
	}
}

func TestSessionEscalation_ResetSession(t *testing.T) {
	tracker := NewSessionEscalationTracker([]string{"gpt-4o", "local/qwen"})

	sid := "session-8"
	tracker.RecordOutcome(sid, "local/qwen", false, 0.5)
	tracker.RecordOutcome(sid, "local/qwen", false, 0.5)

	// Verify escalation is triggered.
	esc, _ := tracker.ShouldEscalate(sid)
	if !esc {
		t.Fatal("expected escalation before reset")
	}

	// Reset and verify cleared.
	tracker.ResetSession(sid)
	esc, _ = tracker.ShouldEscalate(sid)
	if esc {
		t.Error("should not escalate after reset")
	}
}
