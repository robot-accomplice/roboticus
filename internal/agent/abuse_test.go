package agent

import (
	"context"
	"testing"

	"roboticus/testutil"
)

func TestNewAbuseTracker(t *testing.T) {
	store := testutil.TempStore(t)
	tracker := NewAbuseTracker(DefaultAbuseTrackerConfig(), store)
	if tracker == nil {
		t.Fatal("should not be nil")
	}
}

func TestAbuseTracker_RecordSignal(t *testing.T) {
	store := testutil.TempStore(t)
	tracker := NewAbuseTracker(DefaultAbuseTrackerConfig(), store)

	action, err := tracker.RecordSignal(context.Background(), AbuseSignal{
		ActorID: "user1", Origin: "api", Channel: "web",
		SignalType: SignalRateBurst, Severity: 0.15,
	})
	if err != nil {
		t.Fatalf("record: %v", err)
	}
	if action != ActionAllow && action != ActionSlowdown && action != ActionQuarantine {
		t.Errorf("unexpected action: %s", action)
	}
}

func TestAbuseTracker_MultipleSignals(t *testing.T) {
	store := testutil.TempStore(t)
	tracker := NewAbuseTracker(DefaultAbuseTrackerConfig(), store)

	for i := 0; i < 10; i++ {
		_, err := tracker.RecordSignal(context.Background(), AbuseSignal{
			ActorID: "user1", Origin: "api", Channel: "web",
			SignalType: SignalPolicyViolation, Severity: 0.25,
		})
		if err != nil {
			t.Fatalf("record %d: %v", i, err)
		}
	}
	if tracker.GetActorScore("user1") <= 0 {
		t.Error("score should be positive after signals")
	}
}

func TestAbuseTracker_GetActorScore(t *testing.T) {
	store := testutil.TempStore(t)
	tracker := NewAbuseTracker(DefaultAbuseTrackerConfig(), store)

	_, _ = tracker.RecordSignal(context.Background(), AbuseSignal{
		ActorID: "u1", Origin: "api", Channel: "web",
		SignalType: SignalRateBurst, Severity: 0.15,
	})
	if tracker.GetActorScore("u1") <= 0 {
		t.Error("should be positive")
	}
	if tracker.GetActorScore("unknown") != 0 {
		t.Error("unknown should be 0")
	}
}

func TestAbuseTracker_ListRecentEvents(t *testing.T) {
	store := testutil.TempStore(t)
	tracker := NewAbuseTracker(DefaultAbuseTrackerConfig(), store)

	_, _ = tracker.RecordSignal(context.Background(), AbuseSignal{
		ActorID: "u1", Origin: "api", Channel: "web",
		SignalType: SignalRateBurst, Severity: 0.1,
	})
	events, err := tracker.ListRecentEvents(context.Background(), 10)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(events) < 1 {
		t.Error("should have events")
	}
}

func TestAbuseTracker_Disabled(t *testing.T) {
	store := testutil.TempStore(t)
	cfg := DefaultAbuseTrackerConfig()
	cfg.Enabled = false
	tracker := NewAbuseTracker(cfg, store)

	action, _ := tracker.RecordSignal(context.Background(), AbuseSignal{
		ActorID: "u1", Origin: "api", Channel: "web",
		SignalType: SignalSensitiveProbe, Severity: 0.9,
	})
	if action != ActionAllow {
		t.Errorf("disabled should allow, got %s", action)
	}
}

func TestDefaultAbuseTrackerConfig(t *testing.T) {
	cfg := DefaultAbuseTrackerConfig()
	if cfg.SlowdownThreshold <= 0 {
		t.Error("slowdown should be positive")
	}
	if cfg.QuarantineThreshold <= cfg.SlowdownThreshold {
		t.Error("quarantine should exceed slowdown")
	}
}
