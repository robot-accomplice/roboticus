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
		{AgentStateReflecting, "reflecting"},
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

func TestSurvivalTierFromBalance(t *testing.T) {
	tests := []struct {
		name           string
		usd            float64
		hoursBelowZero float64
		want           SurvivalTier
	}{
		{"thriving high balance", 10.0, 0.0, SurvivalTierThriving},
		{"thriving at boundary", 5.0, 0.0, SurvivalTierThriving},
		{"growth just under 5", 4.99, 0.0, SurvivalTierGrowth},
		{"growth at 0.50", 0.50, 0.0, SurvivalTierGrowth},
		{"stable just under 0.50", 0.49, 0.0, SurvivalTierStable},
		{"stable at 0.10", 0.10, 0.0, SurvivalTierStable},
		{"survival just under 0.10", 0.09, 0.0, SurvivalTierSurvival},
		{"survival at zero", 0.0, 0.0, SurvivalTierSurvival},
		{"survival negative but not long enough", -1.0, 0.5, SurvivalTierSurvival},
		{"dead at boundary", -0.01, 0.999, SurvivalTierDead},
		{"survival just under dead boundary", -0.01, 0.998, SurvivalTierSurvival},
		{"dead well past boundary", -1.0, 1.0, SurvivalTierDead},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SurvivalTierFromBalance(tt.usd, tt.hoursBelowZero)
			if got != tt.want {
				t.Errorf("SurvivalTierFromBalance(%v, %v) = %v, want %v", tt.usd, tt.hoursBelowZero, got, tt.want)
			}
		})
	}
}

func TestThreatScore_DowngradeCeiling(t *testing.T) {
	tests := []struct {
		name    string
		score   ThreatScore
		ceiling ThreatScore
		want    ThreatScore
	}{
		{"score below ceiling unchanged", 0.3, 0.7, 0.3},
		{"score at ceiling unchanged", 0.7, 0.7, 0.7},
		{"score above ceiling capped", 0.9, 0.5, 0.5},
		{"zero ceiling caps everything", 0.5, 0.0, 0.0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.score.DowngradeCeiling(tt.ceiling)
			if got != tt.want {
				t.Errorf("ThreatScore(%v).DowngradeCeiling(%v) = %v, want %v", tt.score, tt.ceiling, got, tt.want)
			}
		})
	}
}

func TestAPIFormat_IsOpenAI(t *testing.T) {
	if !APIFormatOpenAI.IsOpenAI() {
		t.Error("APIFormatOpenAI should be OpenAI")
	}
	if !APIFormatOpenAICompletions.IsOpenAI() {
		t.Error("APIFormatOpenAICompletions should be OpenAI")
	}
	if !APIFormatOpenAIResponses.IsOpenAI() {
		t.Error("APIFormatOpenAIResponses should be OpenAI")
	}
	if APIFormatAnthropic.IsOpenAI() {
		t.Error("APIFormatAnthropic should not be OpenAI")
	}
	if APIFormatGoogle.IsOpenAI() {
		t.Error("APIFormatGoogle should not be OpenAI")
	}
}

func TestAPIFormat_String_NewVariants(t *testing.T) {
	if APIFormatOpenAICompletions.String() != "openai" {
		t.Errorf("expected 'openai', got %q", APIFormatOpenAICompletions.String())
	}
	if APIFormatOpenAIResponses.String() != "openai_responses" {
		t.Errorf("expected 'openai_responses', got %q", APIFormatOpenAIResponses.String())
	}
}

func TestAuthorityLevel_Ordering(t *testing.T) {
	// Verify iota ordering matches Rust's Ord derive
	if AuthorityExternal >= AuthorityPeer {
		t.Error("External should be < Peer")
	}
	if AuthorityPeer >= AuthoritySelfGenerated {
		t.Error("Peer should be < SelfGenerated")
	}
	if AuthoritySelfGenerated >= AuthorityCreator {
		t.Error("SelfGenerated should be < Creator")
	}
}
