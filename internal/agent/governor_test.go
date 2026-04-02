package agent

import (
	"testing"
	"time"
)

func TestGovernor_Allow(t *testing.T) {
	g := NewGovernor(GovernorConfig{
		MaxTurnsPerSession:  25,
		MaxTokensPerSession: 100000,
		MaxCostPerSession:   1.0,
	})

	decision := g.Check(5, 10000, 0.10)
	if decision != GovernorAllow {
		t.Errorf("expected Allow, got %v", decision)
	}
}

func TestGovernor_DenyTurns(t *testing.T) {
	g := NewGovernor(GovernorConfig{
		MaxTurnsPerSession:  10,
		MaxTokensPerSession: 100000,
		MaxCostPerSession:   1.0,
	})

	decision := g.Check(11, 5000, 0.05)
	if decision != GovernorDeny {
		t.Errorf("expected Deny on turns, got %v", decision)
	}
}

func TestGovernor_DenyTokens(t *testing.T) {
	g := NewGovernor(GovernorConfig{
		MaxTurnsPerSession:  25,
		MaxTokensPerSession: 50000,
		MaxCostPerSession:   1.0,
	})

	decision := g.Check(5, 60000, 0.10)
	if decision != GovernorDeny {
		t.Errorf("expected Deny on tokens, got %v", decision)
	}
}

func TestGovernor_DenyCost(t *testing.T) {
	g := NewGovernor(GovernorConfig{
		MaxTurnsPerSession:  25,
		MaxTokensPerSession: 100000,
		MaxCostPerSession:   0.50,
	})

	decision := g.Check(5, 10000, 0.60)
	if decision != GovernorDeny {
		t.Errorf("expected Deny on cost, got %v", decision)
	}
}

func TestGovernor_ThrottleNearLimit(t *testing.T) {
	g := NewGovernor(GovernorConfig{
		MaxTurnsPerSession:  10,
		MaxTokensPerSession: 100000,
		MaxCostPerSession:   1.0,
	})

	// 80% of turn limit -> throttle
	decision := g.Check(8, 10000, 0.10)
	if decision != GovernorThrottle {
		t.Errorf("expected Throttle at 80%% turns, got %v", decision)
	}
}

func TestGovernor_ZeroConfig(t *testing.T) {
	g := NewGovernor(GovernorConfig{}) // zero values = no limits
	decision := g.Check(1000, 1000000, 100.0)
	if decision != GovernorAllow {
		t.Errorf("zero config should always allow, got %v", decision)
	}
}

func TestGovernor_CooldownTracking(t *testing.T) {
	g := NewGovernor(GovernorConfig{
		MaxTurnsPerSession:  25,
		MaxTokensPerSession: 100000,
		MaxCostPerSession:   1.0,
		CooldownAfterError:  1 * time.Second,
	})

	g.RecordError()
	if !g.InCooldown() {
		t.Error("should be in cooldown after error")
	}

	// After cooldown expires
	g.lastError = time.Now().Add(-2 * time.Second)
	if g.InCooldown() {
		t.Error("should not be in cooldown after expiry")
	}
}
