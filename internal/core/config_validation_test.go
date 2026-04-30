package core

import (
	"strings"
	"testing"
)

func TestConfig_Validate_DefaultPasses(t *testing.T) {
	cfg := DefaultConfig()
	if err := cfg.Validate(); err != nil {
		t.Fatalf("DefaultConfig() should pass validation, got: %v", err)
	}
}

func TestConfig_Validate_MemoryBudgetMustSumTo100(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Memory.WorkingBudget = 50 // 50+25+20+15+10 = 120
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected validation error for memory budget sum != 100")
	}
	if !strings.Contains(err.Error(), "memory budgets must sum to 100") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestConfig_Validate_MemoryBudgetExactly100(t *testing.T) {
	cfg := DefaultConfig()
	// Default is 30+25+20+15+10 = 100, should pass
	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected pass with sum=100, got: %v", err)
	}
}

func TestConfig_Validate_MemoryBudgetTolerance(t *testing.T) {
	cfg := DefaultConfig()
	// Within 0.01 tolerance
	cfg.Memory.WorkingBudget = 30.005
	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected pass within tolerance, got: %v", err)
	}
}

func TestConfig_Validate_TreasuryLimitsPositive(t *testing.T) {
	tests := []struct {
		name        string
		mutate      func(*Config)
		errContains string
	}{
		{
			"negative daily_cap",
			func(c *Config) { c.Treasury.DailyCap = -1.0 },
			"daily_cap must be >= 0",
		},
		{
			"negative per_payment_cap",
			func(c *Config) { c.Treasury.PerPaymentCap = -0.01 },
			"per_payment_cap must be > 0",
		},
		{
			"negative transfer_limit",
			func(c *Config) { c.Treasury.TransferLimit = -5.0 },
			"transfer_limit must be >= 0",
		},
		{
			"negative minimum_reserve",
			func(c *Config) { c.Treasury.MinimumReserve = -100.0 },
			"minimum_reserve must be >= 0",
		},
		{
			"zero values are fine",
			func(c *Config) {
				c.Treasury.DailyCap = 0
				c.Treasury.TransferLimit = 0
			},
			"",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := DefaultConfig()
			tt.mutate(&cfg)
			err := cfg.Validate()
			if tt.errContains == "" {
				if err != nil {
					t.Fatalf("expected no error, got: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("expected validation error")
			}
			if !strings.Contains(err.Error(), tt.errContains) {
				t.Errorf("expected error containing %q, got: %v", tt.errContains, err)
			}
		})
	}
}

func TestConfig_Validate_DenyOnEmptyAllowlistFalseAllowed(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Security.Filesystem.DenyOnEmptyAllowlist = false
	if err := cfg.Validate(); err != nil {
		t.Fatalf("deny_on_empty_allowlist=false is allowed (fail-open), got: %v", err)
	}
}

func TestConfig_DefaultValues_NewFields(t *testing.T) {
	cfg := DefaultConfig()

	// Skills config depth
	if cfg.Skills.ScriptTimeoutSeconds != 30 {
		t.Errorf("expected ScriptTimeoutSeconds=30, got %d", cfg.Skills.ScriptTimeoutSeconds)
	}
	if cfg.Skills.ScriptMaxOutputBytes != 1<<20 {
		t.Errorf("expected ScriptMaxOutputBytes=1MiB, got %d", cfg.Skills.ScriptMaxOutputBytes)
	}
	if len(cfg.Skills.AllowedInterpreters) != 8 {
		t.Errorf("expected 8 allowed interpreters, got %d", len(cfg.Skills.AllowedInterpreters))
	}
	if cfg.Skills.ScriptMaxMemoryBytes != 256<<20 {
		t.Errorf("expected ScriptMaxMemoryBytes=256MiB, got %d", cfg.Skills.ScriptMaxMemoryBytes)
	}

	// Learning config depth
	if cfg.Learning.MinSuccessRatio != 0.7 {
		t.Errorf("expected MinSuccessRatio=0.7, got %f", cfg.Learning.MinSuccessRatio)
	}
	if cfg.Learning.PriorityBoostOnSuccess != 5 {
		t.Errorf("expected PriorityBoostOnSuccess=5, got %d", cfg.Learning.PriorityBoostOnSuccess)
	}
	if cfg.Learning.PriorityDecayOnFailure != 10 {
		t.Errorf("expected PriorityDecayOnFailure=10 (2:1 asymmetry), got %d", cfg.Learning.PriorityDecayOnFailure)
	}
	if cfg.Learning.MaxLearnedSkills != 100 {
		t.Errorf("expected MaxLearnedSkills=100, got %d", cfg.Learning.MaxLearnedSkills)
	}

	// Digest config depth
	if cfg.Digest.MaxTokens != 512 {
		t.Errorf("expected MaxTokens=512, got %d", cfg.Digest.MaxTokens)
	}
	if cfg.Digest.DecayHalfLifeDays != 7.0 {
		t.Errorf("expected DecayHalfLifeDays=7.0, got %f", cfg.Digest.DecayHalfLifeDays)
	}

	// Multimodal size limits
	if cfg.Multimodal.MaxImageSizeBytes != 10<<20 {
		t.Errorf("expected MaxImageSizeBytes=10MiB, got %d", cfg.Multimodal.MaxImageSizeBytes)
	}
	if cfg.Multimodal.MaxAudioSizeBytes != 25<<20 {
		t.Errorf("expected MaxAudioSizeBytes=25MiB, got %d", cfg.Multimodal.MaxAudioSizeBytes)
	}

	// Heartbeat per-domain intervals
	if cfg.Heartbeat.TreasuryIntervalSeconds != 300 {
		t.Errorf("expected TreasuryIntervalSeconds=300, got %d", cfg.Heartbeat.TreasuryIntervalSeconds)
	}
	if cfg.Heartbeat.YieldIntervalSeconds != 600 {
		t.Errorf("expected YieldIntervalSeconds=600, got %d", cfg.Heartbeat.YieldIntervalSeconds)
	}
	if cfg.Heartbeat.MemoryIntervalSeconds != 60 {
		t.Errorf("expected MemoryIntervalSeconds=60, got %d", cfg.Heartbeat.MemoryIntervalSeconds)
	}
}
