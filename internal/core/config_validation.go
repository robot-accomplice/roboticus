package core

import (
	"fmt"
	"math"
	"net"
	"os"
	"path/filepath"
	"strings"
)

// Validate checks the config for required fields and constraint violations.
func (c *Config) Validate() error {
	// Required fields.
	if c.Models.Primary == "" {
		return fmt.Errorf("%w: models.primary is required", ErrConfig)
	}
	if c.Database.Path == "" {
		return fmt.Errorf("%w: database.path is required", ErrConfig)
	}

	// Server constraints.
	if c.Server.Port < 1 || c.Server.Port > 65535 {
		return fmt.Errorf("%w: server.port must be 1-65535, got %d", ErrConfig, c.Server.Port)
	}
	if c.Server.Bind != "" && c.Server.Bind != "localhost" {
		if net.ParseIP(c.Server.Bind) == nil {
			return fmt.Errorf("%w: server.bind must be a valid IP or 'localhost', got %q", ErrConfig, c.Server.Bind)
		}
	}
	if c.Server.CronMaxConcurrency < 1 || c.Server.CronMaxConcurrency > 16 {
		return fmt.Errorf("%w: server.cron_max_concurrency must be 1-16, got %d", ErrConfig, c.Server.CronMaxConcurrency)
	}

	// Agent constraints.
	if c.Agent.ID == "" {
		return fmt.Errorf("%w: agent.id is required", ErrConfig)
	}
	if c.Agent.Name == "" {
		return fmt.Errorf("%w: agent.name is required", ErrConfig)
	}
	if c.Agent.AutonomyMaxReactTurns == 0 {
		return fmt.Errorf("%w: agent.autonomy_max_react_turns must be > 0", ErrConfig)
	}
	if c.Agent.AutonomyMaxTurnDurationSecs == 0 {
		return fmt.Errorf("%w: agent.autonomy_max_turn_duration_seconds must be > 0", ErrConfig)
	}

	// Session scope mode.
	switch c.Session.ScopeMode {
	case "agent", "peer", "group":
		// valid
	default:
		return fmt.Errorf("%w: session.scope_mode must be 'agent', 'peer', or 'group', got %q", ErrConfig, c.Session.ScopeMode)
	}

	// Memory budgets must sum to 100 (±0.01).
	budgetSum := c.Memory.WorkingBudget + c.Memory.EpisodicBudget +
		c.Memory.SemanticBudget + c.Memory.ProceduralBudget + c.Memory.RelationshipBudget
	if math.Abs(budgetSum-100.0) > 0.01 {
		return fmt.Errorf("%w: memory budgets must sum to 100, got %.2f", ErrConfig, budgetSum)
	}

	// Treasury constraints — all limits must be non-negative.
	if c.Treasury.DailyCap < 0 {
		return fmt.Errorf("%w: treasury.daily_cap must be >= 0, got %.2f", ErrConfig, c.Treasury.DailyCap)
	}
	if c.Treasury.PerPaymentCap <= 0 {
		return fmt.Errorf("%w: treasury.per_payment_cap must be > 0", ErrConfig)
	}
	if c.Treasury.TransferLimit < 0 {
		return fmt.Errorf("%w: treasury.transfer_limit must be >= 0, got %.2f", ErrConfig, c.Treasury.TransferLimit)
	}
	if c.Treasury.MinimumReserve < 0 {
		return fmt.Errorf("%w: treasury.minimum_reserve must be >= 0", ErrConfig)
	}

	// Security.
	if !c.Security.Filesystem.DenyOnEmptyAllowlist {
		return fmt.Errorf("%w: security.filesystem.deny_on_empty_allowlist=false is not allowed (fail-open)", ErrConfig)
	}
	for _, p := range c.Security.ScriptAllowedPaths {
		if !filepath.IsAbs(p) {
			return fmt.Errorf("%w: security.script_allowed_paths entries must be absolute, got %q", ErrConfig, p)
		}
	}

	// Routing constraints.
	r := c.Models.Routing
	switch r.Mode {
	case "primary", "fallback", "auto", "routed", "metascore", "":
		// valid
	default:
		return fmt.Errorf("%w: models.routing.mode must be one of 'primary', 'fallback', 'auto', 'routed', 'metascore', got %q", ErrConfig, r.Mode)
	}
	if r.ConfidenceThreshold < 0 || r.ConfidenceThreshold > 1 {
		return fmt.Errorf("%w: models.routing.confidence_threshold must be [0,1]", ErrConfig)
	}
	if r.AccuracyFloor < 0 || r.AccuracyFloor > 1 {
		return fmt.Errorf("%w: models.routing.accuracy_floor must be [0,1]", ErrConfig)
	}
	if r.CanaryFraction < 0 || r.CanaryFraction > 1 {
		return fmt.Errorf("%w: models.routing.canary_fraction must be [0,1]", ErrConfig)
	}
	if r.CanaryFraction > 0 && r.CanaryModel == "" {
		return fmt.Errorf("%w: models.routing.canary_model required when canary_fraction > 0", ErrConfig)
	}
	if r.CanaryModel != "" && r.CanaryFraction == 0 {
		return fmt.Errorf("%w: models.routing.canary_fraction must be > 0 when canary_model is set", ErrConfig)
	}
	for _, bm := range r.BlockedModels {
		if bm == "" {
			return fmt.Errorf("%w: models.routing.blocked_models entries must be non-empty", ErrConfig)
		}
		if bm == r.CanaryModel {
			return fmt.Errorf("%w: canary_model %q must not appear in blocked_models", ErrConfig, bm)
		}
	}
	if r.PerProviderTimeoutSecs < 5 {
		return fmt.Errorf("%w: models.routing.per_provider_timeout_seconds must be >= 5, got %d", ErrConfig, r.PerProviderTimeoutSecs)
	}
	if r.AccuracyMinObs != 0 && r.AccuracyMinObs <= 0 {
		return fmt.Errorf("%w: models.routing.accuracy_min_obs must be > 0 when set", ErrConfig)
	}
	if r.MaxTotalInferenceSecs > 0 && r.MaxTotalInferenceSecs < r.PerProviderTimeoutSecs {
		return fmt.Errorf("%w: models.routing.max_total_inference_seconds must be >= per_provider_timeout_seconds", ErrConfig)
	}
	if r.MaxFallbackAttempts > 0 && r.MaxFallbackAttempts < 1 {
		return fmt.Errorf("%w: models.routing.max_fallback_attempts must be >= 1", ErrConfig)
	}
	if r.EstimatedOutputTokens > 0 && r.EstimatedOutputTokens < 1 {
		return fmt.Errorf("%w: models.routing.estimated_output_tokens must be >= 1 if set", ErrConfig)
	}

	// Security: threat_caution_ceiling must be below Creator authority if set.
	if c.Security.ThreatCautionCeiling != "" {
		validCeilings := map[string]int{
			"Safe":      0,
			"Caution":   1,
			"Dangerous": 2,
			"External":  3,
			"Creator":   4,
		}
		level, ok := validCeilings[c.Security.ThreatCautionCeiling]
		if !ok {
			return fmt.Errorf("%w: security.threat_caution_ceiling must be one of Safe, Caution, Dangerous, External, Creator; got %q",
				ErrConfig, c.Security.ThreatCautionCeiling)
		}
		if level >= validCeilings["Creator"] {
			return fmt.Errorf("%w: security.threat_caution_ceiling must be below Creator authority", ErrConfig)
		}
		_ = level
	}

	// Heartbeat interval.
	if c.Heartbeat.IntervalSeconds > 0 && c.Heartbeat.IntervalSeconds < 30 {
		return fmt.Errorf("%w: heartbeat.interval_seconds must be >= 30 if set, got %d", ErrConfig, c.Heartbeat.IntervalSeconds)
	}

	// Revenue config.
	if c.Revenue.Enabled {
		if c.Revenue.TaxRate < 0 || c.Revenue.TaxRate > 1 {
			return fmt.Errorf("%w: revenue.tax_rate must be in [0,1], got %f", ErrConfig, c.Revenue.TaxRate)
		}
		if c.Revenue.DestinationWallet == "" {
			return fmt.Errorf("%w: revenue.destination_wallet is required when revenue is enabled", ErrConfig)
		}
	}

	// Legacy E.164 validation removed — signal_account field is gone.

	return nil
}

// NormalizePaths expands ~ in all path-valued fields.
func (c *Config) NormalizePaths() {
	c.Database.Path = expandTilde(c.Database.Path)
	c.Agent.Workspace = expandTilde(c.Agent.Workspace)
	c.ProvidersFile = expandTilde(c.ProvidersFile)
	c.Server.LogDir = expandTilde(c.Server.LogDir)
	c.Skills.Directory = expandTilde(c.Skills.Directory)
	c.Wallet.Path = expandTilde(c.Wallet.Path)
	c.Plugins.Dir = expandTilde(c.Plugins.Dir)
	c.Personality.OSPath = expandTilde(c.Personality.OSPath)
	c.Personality.FirmwarePath = expandTilde(c.Personality.FirmwarePath)
	c.Personality.OperatorPath = expandTilde(c.Personality.OperatorPath)
	c.Knowledge.SourcesDir = expandTilde(c.Knowledge.SourcesDir)
	c.Obsidian.VaultPath = expandTilde(c.Obsidian.VaultPath)
	c.Daemon.PIDFile = expandTilde(c.Daemon.PIDFile)

	if c.Obsidian.Enabled && strings.TrimSpace(c.Obsidian.VaultPath) == "" {
		if detected := detectWorkspaceObsidianVault(c.Agent.Workspace, c.Obsidian.AutoDetectPaths); detected != "" {
			c.Obsidian.VaultPath = detected
		}
	}

	for i, p := range c.Security.AllowedPaths {
		c.Security.AllowedPaths[i] = expandTilde(p)
	}
	for i, p := range c.Security.ProtectedPaths {
		c.Security.ProtectedPaths[i] = expandTilde(p)
	}
	for i, p := range c.Security.ScriptAllowedPaths {
		c.Security.ScriptAllowedPaths[i] = expandTilde(p)
	}

	// Rust-aligned: auto-populate allowed_paths from Obsidian vault_path.
	if c.Obsidian.VaultPath != "" {
		vaultPath := expandTilde(c.Obsidian.VaultPath)
		found := false
		for _, ap := range c.Security.AllowedPaths {
			if ap == vaultPath {
				found = true
				break
			}
		}
		if !found {
			c.Security.AllowedPaths = append(c.Security.AllowedPaths, vaultPath)
		}
	}

	// Merge script_allowed_paths into allowed_paths.
	for _, sp := range c.Security.ScriptAllowedPaths {
		found := false
		for _, ap := range c.Security.AllowedPaths {
			if ap == sp {
				found = true
				break
			}
		}
		if !found {
			c.Security.AllowedPaths = append(c.Security.AllowedPaths, sp)
		}
	}
}

func detectWorkspaceObsidianVault(workspace string, extraCandidates []string) string {
	var candidates []string
	if strings.TrimSpace(workspace) != "" {
		candidates = append(candidates,
			filepath.Join(workspace, "Vault"),
			filepath.Join(workspace, "vault"),
			filepath.Join(workspace, "Obsidian"),
			filepath.Join(workspace, "obsidian"),
		)
	}
	for _, raw := range extraCandidates {
		if path := strings.TrimSpace(expandTilde(raw)); path != "" {
			candidates = append(candidates, path)
		}
	}

	seen := make(map[string]struct{}, len(candidates))
	for _, candidate := range candidates {
		if _, dup := seen[candidate]; dup || candidate == "" {
			continue
		}
		seen[candidate] = struct{}{}

		info, err := os.Stat(candidate)
		if err != nil || !info.IsDir() {
			continue
		}
		if marker, err := os.Stat(filepath.Join(candidate, ".obsidian")); err == nil && marker.IsDir() {
			return candidate
		}
	}
	return ""
}
