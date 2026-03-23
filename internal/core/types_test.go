package core

import (
	"testing"
)

func TestSurvivalTierString(t *testing.T) {
	tests := []struct {
		tier SurvivalTier
		want string
	}{
		{SurvivalTierDead, "dead"},
		{SurvivalTierSurvival, "survival"},
		{SurvivalTierStable, "stable"},
		{SurvivalTierGrowth, "growth"},
		{SurvivalTierThriving, "thriving"},
	}
	for _, tt := range tests {
		if got := tt.tier.String(); got != tt.want {
			t.Errorf("SurvivalTier(%d).String() = %q, want %q", tt.tier, got, tt.want)
		}
	}
}

func TestAgentStateString(t *testing.T) {
	tests := []struct {
		state AgentState
		want  string
	}{
		{AgentStateIdle, "idle"},
		{AgentStateThinking, "thinking"},
		{AgentStateActing, "acting"},
		{AgentStateObserving, "observing"},
		{AgentStatePersisting, "persisting"},
		{AgentStateDone, "done"},
	}
	for _, tt := range tests {
		if got := tt.state.String(); got != tt.want {
			t.Errorf("AgentState(%d).String() = %q, want %q", tt.state, got, tt.want)
		}
	}
}

func TestRiskLevelString(t *testing.T) {
	tests := []struct {
		level RiskLevel
		want  string
	}{
		{RiskLevelSafe, "safe"},
		{RiskLevelCaution, "caution"},
		{RiskLevelDangerous, "dangerous"},
		{RiskLevelForbidden, "forbidden"},
	}
	for _, tt := range tests {
		if got := tt.level.String(); got != tt.want {
			t.Errorf("RiskLevel(%d).String() = %q, want %q", tt.level, got, tt.want)
		}
	}
}

func TestThreatScore(t *testing.T) {
	clean := ThreatScore(0.1)
	if !clean.IsClean() {
		t.Error("0.1 should be clean")
	}
	if clean.IsCaution() {
		t.Error("0.1 should not be caution")
	}
	if clean.IsBlocked() {
		t.Error("0.1 should not be blocked")
	}

	caution := ThreatScore(0.5)
	if caution.IsClean() {
		t.Error("0.5 should not be clean")
	}
	if !caution.IsCaution() {
		t.Error("0.5 should be caution")
	}
	if caution.IsBlocked() {
		t.Error("0.5 should not be blocked")
	}

	blocked := ThreatScore(0.8)
	if blocked.IsClean() {
		t.Error("0.8 should not be clean")
	}
	if !blocked.IsBlocked() {
		t.Error("0.8 should be blocked")
	}
}
