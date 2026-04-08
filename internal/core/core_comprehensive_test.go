package core

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// config.go — LoadConfigFromFile, MarshalTOML, Validate branches, env alias
// ---------------------------------------------------------------------------

func TestLoadConfigFromFile_NonexistentReturnsDefaults(t *testing.T) {
	cfg, err := LoadConfigFromFile("/nonexistent/path/roboticus.toml")
	if err != nil {
		t.Fatalf("nonexistent file should return defaults, got err: %v", err)
	}
	if cfg.Agent.Name != "roboticus" {
		t.Errorf("agent.name = %q, want roboticus", cfg.Agent.Name)
	}
	if cfg.Server.Port != DefaultServerPort {
		t.Errorf("server.port = %d, want %d", cfg.Server.Port, DefaultServerPort)
	}
}

func TestLoadConfigFromFile_ValidTOML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "roboticus.toml")
	content := `
[agent]
name = "test-agent"
id = "test-id"

[server]
port = 9999

[models]
primary = "gpt-4"
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfigFromFile(path)
	if err != nil {
		t.Fatalf("valid TOML should parse: %v", err)
	}
	if cfg.Agent.Name != "test-agent" {
		t.Errorf("agent.name = %q, want test-agent", cfg.Agent.Name)
	}
	if cfg.Server.Port != 9999 {
		t.Errorf("server.port = %d, want 9999", cfg.Server.Port)
	}
}

func TestLoadConfigFromFile_InvalidTOML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.toml")
	if err := os.WriteFile(path, []byte("{{invalid toml"), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := LoadConfigFromFile(path)
	if err == nil {
		t.Error("invalid TOML should return error")
	}
}

func TestLoadConfigFromFile_WorkingBudgetPctOverride(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "roboticus.toml")

	// First, marshal a config with WorkingBudgetPct set to find the correct TOML key.
	cfg := DefaultConfig()
	cfg.Memory.WorkingBudgetPct = 50
	cfg.Memory.EpisodicBudget = 20
	cfg.Memory.SemanticBudget = 10
	cfg.Memory.ProceduralBudget = 10
	cfg.Memory.RelationshipBudget = 10
	data, err := MarshalTOML(&cfg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}

	loaded, err := LoadConfigFromFile(path)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	// WorkingBudgetPct should override WorkingBudget.
	if loaded.Memory.WorkingBudget != 50 {
		t.Errorf("working_budget should be overridden by working_budget_pct, got %f", loaded.Memory.WorkingBudget)
	}
}

func TestMarshalTOML(t *testing.T) {
	cfg := DefaultConfig()
	data, err := MarshalTOML(&cfg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if len(data) == 0 {
		t.Error("marshaled data should not be empty")
	}
	// Should be valid TOML that we can round-trip.
	if !strings.Contains(string(data), "roboticus") {
		t.Error("marshaled TOML should contain agent name")
	}
}

// ---------------------------------------------------------------------------
// config.go — Validate comprehensive branch coverage
// ---------------------------------------------------------------------------

func TestValidate_DefaultConfigIsValid(t *testing.T) {
	cfg := DefaultConfig()
	if err := cfg.Validate(); err != nil {
		t.Fatalf("default config should be valid: %v", err)
	}
}

func TestValidate_EmptyPrimaryModel(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Models.Primary = ""
	err := cfg.Validate()
	if err == nil || !errors.Is(err, ErrConfig) {
		t.Errorf("empty primary model should be invalid: %v", err)
	}
}

func TestValidate_EmptyDatabasePath(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Database.Path = ""
	err := cfg.Validate()
	if err == nil || !errors.Is(err, ErrConfig) {
		t.Errorf("empty database path should be invalid: %v", err)
	}
}

func TestValidate_InvalidServerBind(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Server.Bind = "not-an-ip"
	err := cfg.Validate()
	if err == nil {
		t.Error("invalid bind address should fail validation")
	}
}

func TestValidate_ServerBind_Localhost(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Server.Bind = "localhost"
	if err := cfg.Validate(); err != nil {
		t.Errorf("localhost should be valid bind: %v", err)
	}
}

func TestValidate_ServerBind_ValidIP(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Server.Bind = "0.0.0.0"
	if err := cfg.Validate(); err != nil {
		t.Errorf("0.0.0.0 should be valid bind: %v", err)
	}
}

func TestValidate_CronMaxConcurrency(t *testing.T) {
	cfg := DefaultConfig()

	cfg.Server.CronMaxConcurrency = 0
	if err := cfg.Validate(); err == nil {
		t.Error("cron_max_concurrency=0 should be invalid")
	}

	cfg.Server.CronMaxConcurrency = 17
	if err := cfg.Validate(); err == nil {
		t.Error("cron_max_concurrency=17 should be invalid")
	}

	cfg.Server.CronMaxConcurrency = 16
	if err := cfg.Validate(); err != nil {
		t.Errorf("cron_max_concurrency=16 should be valid: %v", err)
	}
}

func TestValidate_EmptyAgentID(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Agent.ID = ""
	if err := cfg.Validate(); err == nil {
		t.Error("empty agent.id should be invalid")
	}
}

func TestValidate_EmptyAgentName(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Agent.Name = ""
	if err := cfg.Validate(); err == nil {
		t.Error("empty agent.name should be invalid")
	}
}

func TestValidate_ZeroAutonomyTurns(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Agent.AutonomyMaxReactTurns = 0
	if err := cfg.Validate(); err == nil {
		t.Error("zero autonomy turns should be invalid")
	}
}

func TestValidate_ZeroAutonomyDuration(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Agent.AutonomyMaxTurnDurationSecs = 0
	if err := cfg.Validate(); err == nil {
		t.Error("zero autonomy duration should be invalid")
	}
}

func TestValidate_SessionScopeMode(t *testing.T) {
	cfg := DefaultConfig()

	for _, mode := range []string{"agent", "peer", "group"} {
		cfg.Session.ScopeMode = mode
		if err := cfg.Validate(); err != nil {
			t.Errorf("scope_mode=%q should be valid: %v", mode, err)
		}
	}

	cfg.Session.ScopeMode = "invalid"
	if err := cfg.Validate(); err == nil {
		t.Error("invalid scope_mode should fail")
	}
}

func TestValidate_MemoryBudgetsSum(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Memory.WorkingBudget = 50 // total 50+25+20+15+10 = 120
	if err := cfg.Validate(); err == nil {
		t.Error("memory budgets not summing to 100 should be invalid")
	}
}

func TestValidate_TreasuryPerPaymentCap(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Treasury.PerPaymentCap = 0
	if err := cfg.Validate(); err == nil {
		t.Error("per_payment_cap=0 should be invalid")
	}
}

func TestValidate_TreasuryMinimumReserve(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Treasury.MinimumReserve = -1
	if err := cfg.Validate(); err == nil {
		t.Error("negative minimum_reserve should be invalid")
	}
}

func TestValidate_SecurityDenyOnEmptyAllowlist(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Security.DenyOnEmptyAllowlist = false
	if err := cfg.Validate(); err == nil {
		t.Error("deny_on_empty_allowlist=false should be invalid")
	}
}

func TestValidate_ScriptAllowedPathsMustBeAbsolute(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Security.ScriptAllowedPaths = []string{"relative/path"}
	if err := cfg.Validate(); err == nil {
		t.Error("relative script_allowed_paths should be invalid")
	}
}

func TestValidate_ScriptAllowedPathsAbsolute(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Security.ScriptAllowedPaths = []string{"/absolute/path"}
	if err := cfg.Validate(); err != nil {
		t.Errorf("absolute script_allowed_paths should be valid: %v", err)
	}
}

func TestValidate_RoutingMode(t *testing.T) {
	cfg := DefaultConfig()
	validModes := []string{"primary", "fallback", "auto", "routed", "metascore", ""}
	for _, m := range validModes {
		cfg.Models.Routing.Mode = m
		if err := cfg.Validate(); err != nil {
			t.Errorf("routing mode=%q should be valid: %v", m, err)
		}
	}

	cfg.Models.Routing.Mode = "garbage"
	if err := cfg.Validate(); err == nil {
		t.Error("invalid routing mode should fail")
	}
}

func TestValidate_ConfidenceThreshold(t *testing.T) {
	cfg := DefaultConfig()

	cfg.Models.Routing.ConfidenceThreshold = -0.1
	if err := cfg.Validate(); err == nil {
		t.Error("negative confidence_threshold should be invalid")
	}

	cfg.Models.Routing.ConfidenceThreshold = 1.1
	if err := cfg.Validate(); err == nil {
		t.Error("confidence_threshold > 1 should be invalid")
	}
}

func TestValidate_AccuracyFloor(t *testing.T) {
	cfg := DefaultConfig()

	cfg.Models.Routing.AccuracyFloor = -0.1
	if err := cfg.Validate(); err == nil {
		t.Error("negative accuracy_floor should be invalid")
	}

	cfg.Models.Routing.AccuracyFloor = 1.1
	if err := cfg.Validate(); err == nil {
		t.Error("accuracy_floor > 1 should be invalid")
	}
}

func TestValidate_CanaryFraction(t *testing.T) {
	cfg := DefaultConfig()

	cfg.Models.Routing.CanaryFraction = 1.5
	if err := cfg.Validate(); err == nil {
		t.Error("canary_fraction > 1 should be invalid")
	}

	// canary_fraction > 0 requires canary_model.
	cfg.Models.Routing.CanaryFraction = 0.1
	cfg.Models.Routing.CanaryModel = ""
	if err := cfg.Validate(); err == nil {
		t.Error("canary_fraction without canary_model should be invalid")
	}

	// canary_model set requires canary_fraction > 0.
	cfg.Models.Routing.CanaryFraction = 0
	cfg.Models.Routing.CanaryModel = "test-model"
	if err := cfg.Validate(); err == nil {
		t.Error("canary_model without canary_fraction should be invalid")
	}

	// Both set should be valid.
	cfg.Models.Routing.CanaryFraction = 0.1
	cfg.Models.Routing.CanaryModel = "test-model"
	if err := cfg.Validate(); err != nil {
		t.Errorf("valid canary config should pass: %v", err)
	}
}

func TestValidate_BlockedModels(t *testing.T) {
	cfg := DefaultConfig()

	cfg.Models.Routing.BlockedModels = []string{""}
	if err := cfg.Validate(); err == nil {
		t.Error("empty string in blocked_models should be invalid")
	}

	cfg.Models.Routing.CanaryFraction = 0.1
	cfg.Models.Routing.CanaryModel = "canary-model"
	cfg.Models.Routing.BlockedModels = []string{"canary-model"}
	if err := cfg.Validate(); err == nil {
		t.Error("canary_model in blocked_models should be invalid")
	}
}

func TestValidate_PerProviderTimeout(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Models.Routing.PerProviderTimeoutSecs = 3
	if err := cfg.Validate(); err == nil {
		t.Error("per_provider_timeout < 5 should be invalid")
	}
}

func TestValidate_MaxTotalInferenceSecs(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Models.Routing.PerProviderTimeoutSecs = 30
	cfg.Models.Routing.MaxTotalInferenceSecs = 10 // less than per-provider
	if err := cfg.Validate(); err == nil {
		t.Error("max_total_inference < per_provider should be invalid")
	}
}

func TestValidate_ThreatCautionCeiling(t *testing.T) {
	cfg := DefaultConfig()

	cfg.Security.ThreatCautionCeiling = "Safe"
	if err := cfg.Validate(); err != nil {
		t.Errorf("Safe ceiling should be valid: %v", err)
	}

	cfg.Security.ThreatCautionCeiling = "Caution"
	if err := cfg.Validate(); err != nil {
		t.Errorf("Caution ceiling should be valid: %v", err)
	}

	cfg.Security.ThreatCautionCeiling = "Dangerous"
	if err := cfg.Validate(); err != nil {
		t.Errorf("Dangerous ceiling should be valid: %v", err)
	}

	cfg.Security.ThreatCautionCeiling = "External"
	if err := cfg.Validate(); err != nil {
		t.Errorf("External ceiling should be valid: %v", err)
	}

	cfg.Security.ThreatCautionCeiling = "Creator"
	if err := cfg.Validate(); err == nil {
		t.Error("Creator ceiling should be invalid (must be below Creator)")
	}

	cfg.Security.ThreatCautionCeiling = "bogus"
	if err := cfg.Validate(); err == nil {
		t.Error("unknown ceiling should be invalid")
	}
}

func TestValidate_HeartbeatInterval(t *testing.T) {
	cfg := DefaultConfig()

	cfg.Heartbeat.IntervalSeconds = 0 // 0 means disabled, should be valid
	if err := cfg.Validate(); err != nil {
		t.Errorf("heartbeat=0 should be valid: %v", err)
	}

	cfg.Heartbeat.IntervalSeconds = 10 // too low
	if err := cfg.Validate(); err == nil {
		t.Error("heartbeat=10 should be invalid (< 30)")
	}

	cfg.Heartbeat.IntervalSeconds = 30
	if err := cfg.Validate(); err != nil {
		t.Errorf("heartbeat=30 should be valid: %v", err)
	}
}

func TestValidate_Revenue(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Revenue.Enabled = true
	cfg.Revenue.TaxRate = 0.5
	cfg.Revenue.DestinationWallet = ""
	if err := cfg.Validate(); err == nil {
		t.Error("revenue enabled without destination_wallet should be invalid")
	}

	cfg.Revenue.DestinationWallet = "0xabc"
	cfg.Revenue.TaxRate = -0.1
	if err := cfg.Validate(); err == nil {
		t.Error("negative tax_rate should be invalid")
	}

	cfg.Revenue.TaxRate = 1.1
	if err := cfg.Validate(); err == nil {
		t.Error("tax_rate > 1 should be invalid")
	}

	cfg.Revenue.TaxRate = 0.5
	if err := cfg.Validate(); err != nil {
		t.Errorf("valid revenue config should pass: %v", err)
	}
}

// ---------------------------------------------------------------------------
// config.go — NormalizePaths
// ---------------------------------------------------------------------------

func TestNormalizePaths_ExpandsTilde(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Database.Path = "~/data/test.db"
	cfg.Agent.Workspace = "~/workspace"
	cfg.Server.LogDir = "~/logs"
	cfg.Skills.Directory = "~/skills"
	cfg.Wallet.Path = "~/wallet.enc"
	cfg.Plugins.Dir = "~/plugins"
	cfg.Personality.OSPath = "~/os.toml"
	cfg.Personality.FirmwarePath = "~/firmware.toml"
	cfg.Personality.OperatorPath = "~/operator.toml"
	cfg.Knowledge.SourcesDir = "~/knowledge"
	cfg.Obsidian.VaultPath = "~/vault"
	cfg.Daemon.PIDFile = "~/daemon.pid"
	cfg.Security.AllowedPaths = []string{"~/allowed"}
	cfg.Security.ProtectedPaths = []string{"~/protected"}
	cfg.Security.ScriptAllowedPaths = []string{"~/scripts"}

	cfg.NormalizePaths()

	home := os.Getenv("HOME")
	if home == "" {
		t.Skip("HOME not set")
	}

	assertNoTilde := func(name, val string) {
		t.Helper()
		if strings.HasPrefix(val, "~") {
			t.Errorf("%s should not start with ~, got %q", name, val)
		}
		if !strings.HasPrefix(val, home) {
			t.Errorf("%s should start with home dir, got %q", name, val)
		}
	}

	assertNoTilde("Database.Path", cfg.Database.Path)
	assertNoTilde("Agent.Workspace", cfg.Agent.Workspace)
	assertNoTilde("Server.LogDir", cfg.Server.LogDir)
	assertNoTilde("Skills.Directory", cfg.Skills.Directory)
	assertNoTilde("Wallet.Path", cfg.Wallet.Path)
	assertNoTilde("Plugins.Dir", cfg.Plugins.Dir)
	assertNoTilde("Personality.OSPath", cfg.Personality.OSPath)
	assertNoTilde("Personality.FirmwarePath", cfg.Personality.FirmwarePath)
	assertNoTilde("Personality.OperatorPath", cfg.Personality.OperatorPath)
	assertNoTilde("Knowledge.SourcesDir", cfg.Knowledge.SourcesDir)
	assertNoTilde("Obsidian.VaultPath", cfg.Obsidian.VaultPath)
	assertNoTilde("Daemon.PIDFile", cfg.Daemon.PIDFile)
	assertNoTilde("Security.AllowedPaths[0]", cfg.Security.AllowedPaths[0])
	assertNoTilde("Security.ProtectedPaths[0]", cfg.Security.ProtectedPaths[0])
	assertNoTilde("Security.ScriptAllowedPaths[0]", cfg.Security.ScriptAllowedPaths[0])
}

func TestNormalizePaths_MergesScriptAllowedIntoAllowed(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Security.AllowedPaths = []string{"/existing"}
	cfg.Security.ScriptAllowedPaths = []string{"/scripts", "/existing"} // /existing is a dup

	cfg.NormalizePaths()

	// Should have /existing and /scripts but not duplicate /existing.
	found := map[string]int{}
	for _, p := range cfg.Security.AllowedPaths {
		found[p]++
	}
	if found["/existing"] != 1 {
		t.Errorf("/existing should appear once, got %d", found["/existing"])
	}
	if found["/scripts"] != 1 {
		t.Errorf("/scripts should be merged in, got %d", found["/scripts"])
	}
}

func TestNormalizePaths_NoTildeNoChange(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Database.Path = "/absolute/path/test.db"
	cfg.NormalizePaths()
	if cfg.Database.Path != "/absolute/path/test.db" {
		t.Errorf("absolute path should not change, got %q", cfg.Database.Path)
	}
}

// ---------------------------------------------------------------------------
// config.go — MergeBundledProviders, parseBundledProviders
// ---------------------------------------------------------------------------

func TestMergeBundledProviders(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Providers = make(map[string]ProviderConfig)

	cfg.MergeBundledProviders()

	// Should have multiple bundled providers.
	if len(cfg.Providers) == 0 {
		t.Fatal("should have bundled providers after merge")
	}

	// Check specific known providers from bundled_providers.toml.
	for _, name := range []string{"ollama", "openai", "anthropic", "google"} {
		if _, ok := cfg.Providers[name]; !ok {
			t.Errorf("missing bundled provider %q", name)
		}
	}

	// Ollama should be local.
	if !cfg.Providers["ollama"].IsLocal {
		t.Error("ollama should be is_local=true")
	}

	// Anthropic should have extra_headers.
	if cfg.Providers["anthropic"].ExtraHeaders == nil {
		t.Error("anthropic should have extra_headers")
	}
	if cfg.Providers["anthropic"].ExtraHeaders["anthropic-version"] != "2023-06-01" {
		t.Error("anthropic should have anthropic-version header")
	}
}

func TestMergeBundledProviders_UserOverride(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Providers = map[string]ProviderConfig{
		"ollama": {URL: "http://custom:1234", Tier: "T0"},
	}

	cfg.MergeBundledProviders()

	// User's ollama should take precedence.
	if cfg.Providers["ollama"].URL != "http://custom:1234" {
		t.Error("user-defined provider should take precedence over bundled")
	}
	if cfg.Providers["ollama"].Tier != "T0" {
		t.Error("user-defined tier should be preserved")
	}

	// Other bundled providers should still be added.
	if _, ok := cfg.Providers["openai"]; !ok {
		t.Error("non-overridden bundled providers should be added")
	}
}

func TestMergeBundledProviders_NilProviders(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Providers = nil

	cfg.MergeBundledProviders()

	if cfg.Providers == nil {
		t.Error("providers should be initialized")
	}
	if len(cfg.Providers) == 0 {
		t.Error("should have bundled providers")
	}
}

// ---------------------------------------------------------------------------
// config.go — ConfigDir, ConfigFilePath
// ---------------------------------------------------------------------------

func TestConfigDir(t *testing.T) {
	dir := ConfigDir()
	if dir == "" {
		t.Error("ConfigDir should not be empty")
	}
	if !strings.HasSuffix(dir, ".roboticus") {
		t.Errorf("ConfigDir should end with .roboticus, got %q", dir)
	}
}

func TestConfigFilePath(t *testing.T) {
	path := ConfigFilePath()
	if !strings.HasSuffix(path, "roboticus.toml") {
		t.Errorf("ConfigFilePath should end with roboticus.toml, got %q", path)
	}
}

// ---------------------------------------------------------------------------
// config.go — expandTilde
// ---------------------------------------------------------------------------

func TestExpandTilde(t *testing.T) {
	home := os.Getenv("HOME")
	if home == "" {
		t.Skip("HOME not set")
	}

	result := expandTilde("~/test")
	expected := filepath.Join(home, "test")
	if result != expected {
		t.Errorf("expandTilde(~/test) = %q, want %q", result, expected)
	}

	result = expandTilde("/absolute")
	if result != "/absolute" {
		t.Errorf("expandTilde(/absolute) = %q, want /absolute", result)
	}

	result = expandTilde("")
	if result != "" {
		t.Errorf("expandTilde('') = %q, want empty", result)
	}
}

// ---------------------------------------------------------------------------
// personality_loader.go
// ---------------------------------------------------------------------------

func TestLoadOsConfig_DefaultOnMissing(t *testing.T) {
	dir := t.TempDir()
	cfg, err := LoadOsConfig(dir, "")
	if err != nil {
		t.Fatalf("missing file should return defaults: %v", err)
	}
	if cfg.Identity.Name != "roboticus" {
		t.Errorf("default name = %q, want roboticus", cfg.Identity.Name)
	}
	if cfg.Voice.Formality != "balanced" {
		t.Errorf("default formality = %q, want balanced", cfg.Voice.Formality)
	}
}

func TestLoadOsConfig_CustomFilename(t *testing.T) {
	dir := t.TempDir()
	content := `
[identity]
name = "custom-bot"
version = "2.0"

[voice]
formality = "formal"
`
	if err := os.WriteFile(filepath.Join(dir, "custom.toml"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadOsConfig(dir, "custom.toml")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if cfg.Identity.Name != "custom-bot" {
		t.Errorf("name = %q, want custom-bot", cfg.Identity.Name)
	}
	if cfg.Identity.Version != "2.0" {
		t.Errorf("version = %q, want 2.0", cfg.Identity.Version)
	}
}

func TestLoadOsConfig_InvalidTOML(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "OS.toml"), []byte("{{bad"), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := LoadOsConfig(dir, "")
	if err == nil {
		t.Error("invalid TOML should return error")
	}
}

func TestLoadFirmwareConfig_DefaultOnMissing(t *testing.T) {
	dir := t.TempDir()
	cfg, err := LoadFirmwareConfig(dir, "")
	if err != nil {
		t.Fatalf("missing file should return defaults: %v", err)
	}
	if cfg.Approvals.SpendingThreshold != 50.0 {
		t.Errorf("default spending_threshold = %f, want 50.0", cfg.Approvals.SpendingThreshold)
	}
}

func TestLoadFirmwareConfig_CustomFile(t *testing.T) {
	dir := t.TempDir()

	// Use MarshalTOML-compatible keys by round-tripping.
	fw := FirmwareConfig{
		Approvals: FirmwareApprovals{
			SpendingThreshold:   100.0,
			RequireConfirmation: "always",
		},
		Rules: []FirmwareRule{
			{RuleType: "must", Rule: "be helpful"},
			{RuleType: "must_not", Rule: "be rude"},
		},
	}
	data, err := tomlMarshal(fw)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "FIRMWARE.toml"), data, 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadFirmwareConfig(dir, "")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if cfg.Approvals.SpendingThreshold != 100.0 {
		t.Errorf("spending_threshold = %f, want 100.0", cfg.Approvals.SpendingThreshold)
	}
	if len(cfg.Rules) != 2 {
		t.Fatalf("rules count = %d, want 2", len(cfg.Rules))
	}
}

func TestLoadFirmwareConfig_InvalidTOML(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "FIRMWARE.toml"), []byte("{{bad"), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := LoadFirmwareConfig(dir, "")
	if err == nil {
		t.Error("invalid TOML should return error")
	}
}

func TestFormatFirmwareRules(t *testing.T) {
	// Empty rules.
	fw := FirmwareConfig{}
	if result := FormatFirmwareRules(fw); result != "" {
		t.Errorf("empty rules should return empty string, got %q", result)
	}

	// With rules.
	fw.Rules = []FirmwareRule{
		{RuleType: "must", Rule: "be helpful"},
		{RuleType: "must_not", Rule: "be rude"},
		{RuleType: "custom", Rule: "custom rule"},
	}
	result := FormatFirmwareRules(fw)
	if !strings.Contains(result, "MUST: be helpful") {
		t.Errorf("should contain MUST rule, got %q", result)
	}
	if !strings.Contains(result, "MUST NOT: be rude") {
		t.Errorf("should contain MUST NOT rule, got %q", result)
	}
	if !strings.Contains(result, "- custom rule") {
		t.Errorf("should contain custom rule, got %q", result)
	}
}

func TestFormatOsPersonality(t *testing.T) {
	// With prompt_text set, should return it directly.
	os1 := OsConfig{PromptText: "I am a helpful bot."}
	if result := FormatOsPersonality(os1); result != "I am a helpful bot." {
		t.Errorf("should return prompt_text, got %q", result)
	}

	// Without prompt_text, should generate from voice params.
	os2 := OsConfig{
		Voice: OsVoice{
			Formality: "balanced",
			Verbosity: "concise",
			Humor:     "dry",
		},
	}
	result := FormatOsPersonality(os2)
	if !strings.Contains(result, "Formality: balanced") {
		t.Errorf("should contain formality, got %q", result)
	}
	if !strings.Contains(result, "Verbosity: concise") {
		t.Errorf("should contain verbosity, got %q", result)
	}
	if !strings.HasSuffix(result, ".") {
		t.Errorf("should end with period, got %q", result)
	}

	// Empty voice params.
	os3 := OsConfig{}
	if result := FormatOsPersonality(os3); result != "" {
		t.Errorf("empty voice should return empty string, got %q", result)
	}
}

// ---------------------------------------------------------------------------
// profiles.go — additional coverage
// ---------------------------------------------------------------------------

func TestProfileRegistry_SwitchNonexistent(t *testing.T) {
	reg := NewProfileRegistry(t.TempDir())
	err := reg.Switch("nonexistent")
	if err == nil {
		t.Error("switching to nonexistent profile should fail")
	}
}

func TestProfileRegistry_DeleteNonexistent(t *testing.T) {
	reg := NewProfileRegistry(t.TempDir())
	err := reg.Delete("nonexistent")
	if err == nil {
		t.Error("deleting nonexistent profile should fail")
	}
}

func TestProfileRegistry_CreateEmptyName(t *testing.T) {
	reg := NewProfileRegistry(t.TempDir())
	_, err := reg.Create("", "desc")
	if err == nil {
		t.Error("empty profile name should fail")
	}
}

func TestProfileRegistry_ConfigDir(t *testing.T) {
	base := t.TempDir()
	reg := NewProfileRegistry(base)

	// Default profile returns base path.
	dir := reg.ConfigDir("default")
	if dir != base {
		t.Errorf("default config dir = %q, want %q", dir, base)
	}

	// Nonexistent profile returns base path.
	dir = reg.ConfigDir("nonexistent")
	if dir != base {
		t.Errorf("nonexistent config dir = %q, want %q", dir, base)
	}

	// Created profile returns profile-specific path.
	_, err := reg.Create("myprofile", "test")
	if err != nil {
		t.Fatal(err)
	}
	dir = reg.ConfigDir("myprofile")
	expected := filepath.Join(base, "profiles", "myprofile")
	if dir != expected {
		t.Errorf("profile config dir = %q, want %q", dir, expected)
	}
}

func TestProfileRegistry_ActiveFallsBackToDefault(t *testing.T) {
	base := t.TempDir()
	reg := NewProfileRegistry(base)
	// Manually deactivate default (edge case).
	reg.profiles["default"].Active = false

	active := reg.Active()
	if active.Name != "Default" {
		t.Errorf("should fall back to default, got %q", active.Name)
	}
}

// ---------------------------------------------------------------------------
// ratelimit.go
// ---------------------------------------------------------------------------

func TestRateLimiter_AllowsUpToLimit(t *testing.T) {
	rl := NewRateLimiter(3, time.Second)

	for i := 0; i < 3; i++ {
		if !rl.Allow() {
			t.Errorf("request %d should be allowed", i+1)
		}
	}

	if rl.Allow() {
		t.Error("4th request should be denied")
	}
}

func TestRateLimiter_WindowExpiry(t *testing.T) {
	rl := NewRateLimiter(2, 50*time.Millisecond)

	rl.Allow()
	rl.Allow()

	if rl.Allow() {
		t.Error("should be rate limited")
	}

	time.Sleep(60 * time.Millisecond)

	if !rl.Allow() {
		t.Error("should be allowed after window expires")
	}
}

func TestRateLimiter_SingleRequest(t *testing.T) {
	rl := NewRateLimiter(1, time.Minute)
	if !rl.Allow() {
		t.Error("first request should be allowed")
	}
	if rl.Allow() {
		t.Error("second request should be denied")
	}
}

// ---------------------------------------------------------------------------
// types.go — additional String() coverage for all enum types
// ---------------------------------------------------------------------------

func TestModelTierString(t *testing.T) {
	tests := []struct {
		tier ModelTier
		want string
	}{
		{ModelTierSmall, "small"},
		{ModelTierMedium, "medium"},
		{ModelTierLarge, "large"},
		{ModelTierFrontier, "frontier"},
		{ModelTier(99), "unknown"},
	}
	for _, tt := range tests {
		if got := tt.tier.String(); got != tt.want {
			t.Errorf("ModelTier(%d).String() = %q, want %q", tt.tier, got, tt.want)
		}
	}
}

func TestAPIFormatString(t *testing.T) {
	tests := []struct {
		f    APIFormat
		want string
	}{
		{APIFormatOpenAI, "openai"},
		{APIFormatAnthropic, "anthropic"},
		{APIFormatOllama, "ollama"},
		{APIFormatGoogle, "google"},
		{APIFormat(99), "unknown"},
	}
	for _, tt := range tests {
		if got := tt.f.String(); got != tt.want {
			t.Errorf("APIFormat(%d).String() = %q, want %q", tt.f, got, tt.want)
		}
	}
}

func TestDeliveryStatusString(t *testing.T) {
	tests := []struct {
		s    DeliveryStatus
		want string
	}{
		{DeliveryPending, "pending"},
		{DeliveryInFlight, "in_flight"},
		{DeliveryDelivered, "delivered"},
		{DeliveryFailed, "failed"},
		{DeliveryDeadLetter, "dead_letter"},
		{DeliveryStatus(99), "unknown"},
	}
	for _, tt := range tests {
		if got := tt.s.String(); got != tt.want {
			t.Errorf("DeliveryStatus(%d).String() = %q, want %q", tt.s, got, tt.want)
		}
	}
}

func TestMemoryTierString(t *testing.T) {
	tests := []struct {
		m    MemoryTier
		want string
	}{
		{MemoryTierWorking, "working"},
		{MemoryTierEpisodic, "episodic"},
		{MemoryTierSemantic, "semantic"},
		{MemoryTierProcedural, "procedural"},
		{MemoryTierRelationship, "relationship"},
		{MemoryTier(99), "unknown"},
	}
	for _, tt := range tests {
		if got := tt.m.String(); got != tt.want {
			t.Errorf("MemoryTier(%d).String() = %q, want %q", tt.m, got, tt.want)
		}
	}
}

func TestSurvivalTierString_Unknown(t *testing.T) {
	if got := SurvivalTier(99).String(); got != "unknown" {
		t.Errorf("unknown SurvivalTier = %q, want unknown", got)
	}
}

func TestAgentStateString_Unknown(t *testing.T) {
	if got := AgentState(99).String(); got != "unknown" {
		t.Errorf("unknown AgentState = %q, want unknown", got)
	}
}

func TestRiskLevelString_Unknown(t *testing.T) {
	if got := RiskLevel(99).String(); got != "unknown" {
		t.Errorf("unknown RiskLevel = %q, want unknown", got)
	}
}

func TestAuthorityLevelString(t *testing.T) {
	tests := []struct {
		a    AuthorityLevel
		want string
	}{
		{AuthorityExternal, "external"},
		{AuthorityPeer, "peer"},
		{AuthoritySelfGenerated, "self_generated"},
		{AuthorityCreator, "creator"},
		{AuthorityLevel(99), "unknown"},
	}
	for _, tt := range tests {
		if got := tt.a.String(); got != tt.want {
			t.Errorf("AuthorityLevel(%d).String() = %q, want %q", tt.a, got, tt.want)
		}
	}
}

// ---------------------------------------------------------------------------
// types.go — ThreatScore boundary values
// ---------------------------------------------------------------------------

func TestThreatScore_AtBoundaries(t *testing.T) {
	// Exactly at clean threshold.
	atClean := ThreatScore(ThreatThresholdClean)
	if atClean.IsClean() {
		t.Error("score at clean threshold should not be clean")
	}
	if !atClean.IsCaution() {
		t.Error("score at clean threshold should be caution")
	}

	// Exactly at caution threshold.
	atCaution := ThreatScore(ThreatThresholdCaution)
	if !atCaution.IsBlocked() {
		t.Error("score at caution threshold should be blocked")
	}
	if atCaution.IsCaution() {
		t.Error("score at caution threshold should not be caution (is blocked)")
	}
}

// ---------------------------------------------------------------------------
// errors.go — additional error coverage
// ---------------------------------------------------------------------------

func TestGobError_AllSentinelErrors(t *testing.T) {
	sentinels := []error{
		ErrConfig, ErrChannel, ErrDatabase, ErrLLM, ErrNetwork,
		ErrPolicy, ErrTool, ErrWallet, ErrInjection, ErrSchedule,
		ErrA2A, ErrIO, ErrSkill, ErrKeystore, ErrInjectionBlocked,
		ErrDuplicate, ErrNotFound, ErrUnauthorized, ErrRateLimited,
		ErrCreditExhausted, ErrGuardExhausted,
	}
	for _, sentinel := range sentinels {
		err := NewError(sentinel, "test")
		if !errors.Is(err, sentinel) {
			t.Errorf("NewError(%v) should match via errors.Is", sentinel)
		}
	}
}

func TestGobError_Is_MismatchCategory(t *testing.T) {
	err := NewError(ErrConfig, "test")
	// Should not match a different category.
	if errors.Is(err, ErrDatabase) {
		t.Error("should not match ErrDatabase")
	}
}

func TestWrapError_ChainedUnwrap(t *testing.T) {
	innermost := fmt.Errorf("root cause")
	middle := WrapError(ErrNetwork, "layer1", innermost)
	outer := WrapError(ErrLLM, "layer2", middle)

	if !errors.Is(outer, ErrLLM) {
		t.Error("outer should match ErrLLM")
	}

	// Unwrapping outer should give us middle.
	unwrapped := errors.Unwrap(outer)
	if unwrapped != middle {
		t.Error("unwrap outer should give middle")
	}

	// Check the error message contains all layers.
	msg := outer.Error()
	if !strings.Contains(msg, "layer2") || !strings.Contains(msg, "layer1") {
		t.Errorf("error message should chain: %q", msg)
	}
}

// ---------------------------------------------------------------------------
// keystore.go — Save with empty path
// ---------------------------------------------------------------------------

func TestKeystore_SaveWithEmptyPath(t *testing.T) {
	ks, _ := OpenKeystore(KeystoreConfig{Passphrase: "test"})
	_ = ks.Set("key", "val")
	err := ks.Save()
	if err == nil {
		t.Error("save with empty path should fail")
	}
}

func TestKeystore_SaveCreatesDirs(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "dir", "keystore.enc")

	ks, err := OpenKeystore(KeystoreConfig{Path: path, Passphrase: "test"})
	if err != nil {
		t.Fatal(err)
	}
	_ = ks.Set("key", "val")

	if err := ks.Save(); err != nil {
		t.Fatalf("save should create intermediate dirs: %v", err)
	}

	if _, err := os.Stat(path); err != nil {
		t.Error("keystore file should exist after save")
	}
}

func TestKeystore_CountEmpty(t *testing.T) {
	ks, _ := OpenKeystore(KeystoreConfig{Passphrase: "test"})
	if ks.Count() != 0 {
		t.Errorf("empty keystore count = %d, want 0", ks.Count())
	}
}

func TestKeystore_EnvFallback_WithSetenv(t *testing.T) {
	ks, _ := OpenKeystore(KeystoreConfig{})
	t.Setenv("TEST_KS_ENV_KEY", "env-value")

	val, err := ks.Get("TEST_KS_ENV_KEY")
	if err != nil {
		t.Fatalf("env fallback should work: %v", err)
	}
	if val != "env-value" {
		t.Errorf("got %q, want env-value", val)
	}
}

func TestKeystore_MasterKeyFromEnv(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "keystore.enc")

	t.Setenv("ROBOTICUS_MASTER_KEY", "env-master-key")

	ks, err := OpenKeystore(KeystoreConfig{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	_ = ks.Set("secret", "from-env-key")

	if err := ks.Save(); err != nil {
		t.Fatalf("save: %v", err)
	}

	// Reload with same env key.
	ks2, err := OpenKeystore(KeystoreConfig{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	val, err := ks2.Get("secret")
	if err != nil {
		t.Fatal(err)
	}
	if val != "from-env-key" {
		t.Errorf("got %q, want from-env-key", val)
	}
}

func TestKeystore_ListSorted(t *testing.T) {
	ks, _ := OpenKeystore(KeystoreConfig{Passphrase: "test"})
	_ = ks.Set("zebra", "z")
	_ = ks.Set("apple", "a")
	_ = ks.Set("mango", "m")

	names := ks.List()
	if len(names) != 3 {
		t.Fatalf("count = %d, want 3", len(names))
	}
	if names[0] != "apple" || names[1] != "mango" || names[2] != "zebra" {
		t.Errorf("list not sorted: %v", names)
	}
}

func TestResolveSecret_NilKeystore(t *testing.T) {
	t.Setenv("RESOLVE_TEST_KEY", "from-env")
	if got := ResolveSecret(nil, "RESOLVE_TEST_KEY"); got != "from-env" {
		t.Errorf("nil keystore should fall back to env, got %q", got)
	}
}

func TestResolveSecret_NotFound(t *testing.T) {
	ks, _ := OpenKeystore(KeystoreConfig{Passphrase: "test"})
	if got := ResolveSecret(ks, "DEFINITELY_NOT_SET_12345"); got != "" {
		t.Errorf("missing key should return empty, got %q", got)
	}
}

// ---------------------------------------------------------------------------
// security.go — additional edge cases
// ---------------------------------------------------------------------------

func TestIsPathAllowed_WorkspaceExact(t *testing.T) {
	// Path equals workspace.
	if !IsPathAllowed("/workspace", "/workspace", nil) {
		t.Error("path equal to workspace should be allowed")
	}
}

func TestIsPathAllowed_AllowedExact(t *testing.T) {
	// Path equals an allowed path exactly.
	if !IsPathAllowed("/opt/data", "/workspace", []string{"/opt/data"}) {
		t.Error("path equal to allowed path should be allowed")
	}
}

func TestHashSHA256_EmptyInput(t *testing.T) {
	hash := HashSHA256([]byte{})
	// SHA-256 of empty is well-known.
	expected := "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	if hash != expected {
		t.Errorf("hash of empty = %q, want %q", hash, expected)
	}
}

// ---------------------------------------------------------------------------
// bgwork.go — NewBackgroundWorker edge case
// ---------------------------------------------------------------------------

func TestNewBackgroundWorker_ZeroConcurrency(t *testing.T) {
	w := NewBackgroundWorker(0)
	// Should default to 16 and not panic.
	var ran bool
	w.Submit("test", func(_ context.Context) {
		ran = true
	})
	w.Drain(time.Second)
	if !ran {
		t.Error("task should have run with default concurrency")
	}
}

// ---------------------------------------------------------------------------
// personality.go — DefaultOsConfig, DefaultFirmwareConfig
// ---------------------------------------------------------------------------

func TestDefaultOsConfig(t *testing.T) {
	cfg := DefaultOsConfig()
	if cfg.Identity.Name != "roboticus" {
		t.Errorf("default os name = %q", cfg.Identity.Name)
	}
	if cfg.Voice.Domain != "general" {
		t.Errorf("default voice domain = %q", cfg.Voice.Domain)
	}
}

func TestDefaultFirmwareConfig(t *testing.T) {
	cfg := DefaultFirmwareConfig()
	if cfg.Approvals.SpendingThreshold != 50.0 {
		t.Errorf("default spending threshold = %f", cfg.Approvals.SpendingThreshold)
	}
	if cfg.Approvals.RequireConfirmation != "risky" {
		t.Errorf("default require_confirmation = %q", cfg.Approvals.RequireConfirmation)
	}
}

// ---------------------------------------------------------------------------
// config.go — LoadConfigFromFile with unreadable file
// ---------------------------------------------------------------------------

func TestLoadConfigFromFile_Unreadable(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "unreadable.toml")
	if err := os.WriteFile(path, []byte("[agent]\nname = \"test\"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	// Make it unreadable.
	if err := os.Chmod(path, 0000); err != nil {
		t.Skip("cannot change file permissions on this OS")
	}
	defer func() { _ = os.Chmod(path, 0644) }()

	_, err := LoadConfigFromFile(path)
	if err == nil {
		t.Error("unreadable file should return error")
	}
}

// ---------------------------------------------------------------------------
// config.go — MarshalTOML error path via mock
// ---------------------------------------------------------------------------

func TestMarshalTOML_RoundTrip(t *testing.T) {
	cfg := DefaultConfig()
	data, err := MarshalTOML(&cfg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	// Should be able to unmarshal back.
	var cfg2 Config
	if err := tomlUnmarshal(data, &cfg2); err != nil {
		t.Fatalf("round-trip unmarshal: %v", err)
	}
	if cfg2.Agent.Name != cfg.Agent.Name {
		t.Errorf("round-trip agent.name = %q, want %q", cfg2.Agent.Name, cfg.Agent.Name)
	}
}
