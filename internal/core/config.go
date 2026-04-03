package core

import (
	_ "embed"
	"fmt"
	"math"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

//go:embed bundled_providers.toml
var bundledProvidersTOML string

// Config is the top-level application configuration, loaded from TOML.
type Config struct {
	Agent      AgentConfig               `json:"agent" mapstructure:"agent"`
	Server     ServerConfig              `json:"server" mapstructure:"server"`
	Database   DatabaseConfig            `json:"database" mapstructure:"database"`
	Models     ModelsConfig              `json:"models" mapstructure:"models"`
	Providers  map[string]ProviderConfig `json:"providers" mapstructure:"providers"`
	Memory     MemoryConfig              `json:"memory" mapstructure:"memory"`
	Cache      CacheConfig               `json:"cache" mapstructure:"cache"`
	Treasury   TreasuryConfig            `json:"treasury" mapstructure:"treasury"`
	Channels   ChannelsConfig            `json:"channels" mapstructure:"channels"`
	Security   SecurityConfig            `json:"security" mapstructure:"security"`
	Skills     SkillsConfig              `json:"skills" mapstructure:"skills"`
	Session    SessionConfig             `json:"session" mapstructure:"session"`
	Wallet     WalletConfig              `json:"wallet" mapstructure:"wallet"`
	Plugins    PluginsConfig             `json:"plugins" mapstructure:"plugins"`
	Approvals  ApprovalsConfig           `json:"approvals" mapstructure:"approvals"`
	Abuse      AbuseConfig               `json:"abuse" mapstructure:"abuse"`
	RateLimit  RateLimitConfig           `json:"rate_limit" mapstructure:"rate_limit"`
	MCP        MCPConfig                 `json:"mcp" mapstructure:"mcp"`
	Matrix     MatrixChannelConfig       `json:"matrix" mapstructure:"matrix"`
	Sandbox    SandboxCfg                `json:"sandbox" mapstructure:"sandbox"`
	Classifier ClassifierConfig          `json:"classifier" mapstructure:"classifier"`
	Planner    PlannerConfig             `json:"planner" mapstructure:"planner"`
	Themes     ThemesConfig              `json:"themes" mapstructure:"themes"`
	DKIM       DKIMConfig                `json:"dkim" mapstructure:"dkim"`
	CORS       CORSConfig                `json:"cors" mapstructure:"cors"`
	Revenue    RevenueConfig             `json:"revenue" mapstructure:"revenue"`
	Heartbeat  HeartbeatConfig           `json:"heartbeat" mapstructure:"heartbeat"`
}

// CORSConfig holds cross-origin request settings.
type CORSConfig struct {
	AllowedOrigins []string `json:"allowed_origins" mapstructure:"allowed_origins"`
	MaxAgeSeconds  int      `json:"max_age_seconds" mapstructure:"max_age_seconds"`
}

// MatrixChannelConfig holds Matrix homeserver connection settings.
type MatrixChannelConfig struct {
	Enabled       bool     `json:"enabled" mapstructure:"enabled"`
	HomeserverURL string   `json:"homeserver_url" mapstructure:"homeserver_url"`
	AccessToken   string   `json:"access_token" mapstructure:"access_token"`
	DeviceID      string   `json:"device_id" mapstructure:"device_id"`
	AllowedRooms  []string `json:"allowed_rooms" mapstructure:"allowed_rooms"`
	AutoJoin      bool     `json:"auto_join" mapstructure:"auto_join"`
	E2EEEnabled   bool     `json:"e2ee_enabled" mapstructure:"e2ee_enabled"`
}

// SandboxCfg holds OS-level process confinement settings.
type SandboxCfg struct {
	Enabled        bool     `json:"enabled" mapstructure:"enabled"`
	MaxMemoryBytes int64    `json:"max_memory_bytes" mapstructure:"max_memory_bytes"`
	AllowedPaths   []string `json:"allowed_paths" mapstructure:"allowed_paths"`
}

// ClassifierConfig holds intent classification settings.
type ClassifierConfig struct {
	Enabled             bool    `json:"enabled" mapstructure:"enabled"`
	ConfidenceThreshold float64 `json:"confidence_threshold" mapstructure:"confidence_threshold"`
}

// PlannerConfig holds action planner settings.
type PlannerConfig struct {
	Enabled                 bool `json:"enabled" mapstructure:"enabled"`
	MaxNormalizationRetries int  `json:"max_normalization_retries" mapstructure:"max_normalization_retries"`
}

// ThemesConfig holds theme marketplace settings.
type ThemesConfig struct {
	CatalogURL string `json:"catalog_url" mapstructure:"catalog_url"`
}

// DKIMConfig holds DKIM verification settings.
type DKIMConfig struct {
	Enabled      bool `json:"enabled" mapstructure:"enabled"`
	RequireValid bool `json:"require_valid" mapstructure:"require_valid"`
}

// MCPConfig holds MCP (Model Context Protocol) server configuration.
type MCPConfig struct {
	Servers []MCPServerEntry `json:"servers" mapstructure:"servers"`
}

// MCPServerEntry defines an MCP server to connect to.
type MCPServerEntry struct {
	Name      string            `json:"name" mapstructure:"name"`
	Transport string            `json:"transport" mapstructure:"transport"` // "stdio" or "sse"
	Command   string            `json:"command" mapstructure:"command"`
	Args      []string          `json:"args" mapstructure:"args"`
	URL       string            `json:"url" mapstructure:"url"`
	Env       map[string]string `json:"env" mapstructure:"env"`
	Enabled   bool              `json:"enabled" mapstructure:"enabled"`
}

// ApprovalsConfig controls human-in-the-loop tool gating.
type ApprovalsConfig struct {
	Enabled        bool     `json:"enabled" mapstructure:"enabled"`
	GatedTools     []string `json:"gated_tools" mapstructure:"gated_tools"`
	BlockedTools   []string `json:"blocked_tools" mapstructure:"blocked_tools"`
	TimeoutSeconds int      `json:"timeout_seconds" mapstructure:"timeout_seconds"`
}

// AbuseConfig controls the abuse tracking system.
type AbuseConfig struct {
	Enabled             bool    `json:"enabled" mapstructure:"enabled"`
	WindowMinutes       int     `json:"window_minutes" mapstructure:"window_minutes"`
	SlowdownThreshold   float64 `json:"slowdown_threshold" mapstructure:"slowdown_threshold"`
	QuarantineThreshold float64 `json:"quarantine_threshold" mapstructure:"quarantine_threshold"`
}

// RateLimitConfig controls per-IP HTTP rate limiting.
type RateLimitConfig struct {
	Enabled           bool `json:"enabled" mapstructure:"enabled"`
	RequestsPerWindow int  `json:"requests_per_window" mapstructure:"requests_per_window"`
	WindowSeconds     int  `json:"window_seconds" mapstructure:"window_seconds"`
}

// AgentConfig holds agent identity and workspace settings.
type AgentConfig struct {
	Name                        string `json:"name" mapstructure:"name"`
	ID                          string `json:"id" mapstructure:"id"`
	Workspace                   string `json:"workspace" mapstructure:"workspace"`
	AutonomyMaxReactTurns       int    `json:"autonomy_max_react_turns" mapstructure:"autonomy_max_react_turns"`
	AutonomyMaxTurnDurationSecs int    `json:"autonomy_max_turn_duration_seconds" mapstructure:"autonomy_max_turn_duration_seconds"`
}

// ServerConfig holds HTTP server settings.
type ServerConfig struct {
	Port               int    `json:"port" mapstructure:"port"`
	Bind               string `json:"bind" mapstructure:"bind"`
	LogDir             string `json:"log_dir" mapstructure:"log_dir"`
	CronMaxConcurrency int    `json:"cron_max_concurrency" mapstructure:"cron_max_concurrency"`
}

// DatabaseConfig holds SQLite connection settings.
type DatabaseConfig struct {
	Path string `json:"path" mapstructure:"path"`
}

// ModelsConfig holds LLM provider and model settings.
type ModelsConfig struct {
	Primary  string        `json:"primary" mapstructure:"primary"`
	Fallback []string      `json:"fallback" mapstructure:"fallback"`
	Routing  RoutingConfig `json:"routing" mapstructure:"routing"`
}

// RoutingConfig holds model routing parameters.
type RoutingConfig struct {
	Mode                   string   `json:"mode" mapstructure:"mode"`
	ConfidenceThreshold    float64  `json:"confidence_threshold" mapstructure:"confidence_threshold"`
	EstimatedOutputTokens  int      `json:"estimated_output_tokens" mapstructure:"estimated_output_tokens"`
	AccuracyFloor          float64  `json:"accuracy_floor" mapstructure:"accuracy_floor"`
	AccuracyMinObs         int      `json:"accuracy_min_obs" mapstructure:"accuracy_min_obs"`
	CostWeight             *float64 `json:"cost_weight,omitempty" mapstructure:"cost_weight"`
	CostAware              bool     `json:"cost_aware" mapstructure:"cost_aware"`
	CanaryFraction         float64  `json:"canary_fraction" mapstructure:"canary_fraction"`
	CanaryModel            string   `json:"canary_model" mapstructure:"canary_model"`
	BlockedModels          []string `json:"blocked_models" mapstructure:"blocked_models"`
	PerProviderTimeoutSecs int      `json:"per_provider_timeout_seconds" mapstructure:"per_provider_timeout_seconds"`
	MaxTotalInferenceSecs  int      `json:"max_total_inference_seconds" mapstructure:"max_total_inference_seconds"`
	MaxFallbackAttempts    int      `json:"max_fallback_attempts" mapstructure:"max_fallback_attempts"`
}

// ProviderConfig describes a single LLM provider endpoint.
type ProviderConfig struct {
	URL                 string            `json:"url" mapstructure:"url"`
	Tier                string            `json:"tier" mapstructure:"tier"`
	Format              string            `json:"format,omitempty" mapstructure:"format"`
	APIKeyEnv           string            `json:"api_key_env,omitempty" mapstructure:"api_key_env"`
	ChatPath            string            `json:"chat_path,omitempty" mapstructure:"chat_path"`
	EmbeddingPath       string            `json:"embedding_path,omitempty" mapstructure:"embedding_path"`
	EmbeddingModel      string            `json:"embedding_model,omitempty" mapstructure:"embedding_model"`
	EmbeddingDimensions int               `json:"embedding_dimensions,omitempty" mapstructure:"embedding_dimensions"`
	IsLocal             bool              `json:"is_local,omitempty" mapstructure:"is_local"`
	CostPerInputToken   float64           `json:"cost_per_input_token,omitempty" mapstructure:"cost_per_input_token"`
	CostPerOutputToken  float64           `json:"cost_per_output_token,omitempty" mapstructure:"cost_per_output_token"`
	AuthHeader          string            `json:"auth_header,omitempty" mapstructure:"auth_header"`
	ExtraHeaders        map[string]string `json:"extra_headers,omitempty" mapstructure:"extra_headers"`
	TPMLimit            uint64            `json:"tpm_limit,omitempty" mapstructure:"tpm_limit"`
	RPMLimit            uint64            `json:"rpm_limit,omitempty" mapstructure:"rpm_limit"`
}

// SessionConfig holds session scoping and timeout settings.
type SessionConfig struct {
	ScopeMode string `json:"scope_mode" mapstructure:"scope_mode"`
}

// MemoryConfig holds memory budget settings as percentages (must sum to 100).
type MemoryConfig struct {
	WorkingBudget      float64 `json:"working_budget" mapstructure:"working_budget"`
	EpisodicBudget     float64 `json:"episodic_budget" mapstructure:"episodic_budget"`
	SemanticBudget     float64 `json:"semantic_budget" mapstructure:"semantic_budget"`
	ProceduralBudget   float64 `json:"procedural_budget" mapstructure:"procedural_budget"`
	RelationshipBudget float64 `json:"relationship_budget" mapstructure:"relationship_budget"`
}

// CacheConfig holds semantic cache settings.
type CacheConfig struct {
	TTLSeconds          int     `json:"ttl_seconds" mapstructure:"ttl_seconds"`
	SimilarityThreshold float64 `json:"similarity_threshold" mapstructure:"similarity_threshold"`
}

// TreasuryConfig holds financial policy limits.
type TreasuryConfig struct {
	DailyCap       float64 `json:"daily_cap" mapstructure:"daily_cap"`
	PerPaymentCap  float64 `json:"per_payment_cap" mapstructure:"per_payment_cap"`
	TransferLimit  float64 `json:"transfer_limit" mapstructure:"transfer_limit"`
	MinimumReserve float64 `json:"minimum_reserve" mapstructure:"minimum_reserve"`
}

// WalletConfig holds crypto wallet settings.
type WalletConfig struct {
	Path string `json:"path" mapstructure:"path"`
}

// PluginsConfig holds plugin discovery settings.
type PluginsConfig struct {
	Dir string `json:"dir" mapstructure:"dir"`
}

// ChannelsConfig holds channel adapter token references.
type ChannelsConfig struct {
	TelegramTokenEnv string `json:"telegram_token_env" mapstructure:"telegram_token_env"`
	WhatsAppTokenEnv string `json:"whatsapp_token_env" mapstructure:"whatsapp_token_env"`
	DiscordTokenEnv  string `json:"discord_token_env" mapstructure:"discord_token_env"`
	SignalAccount    string `json:"signal_account" mapstructure:"signal_account"`
	SignalDaemonURL  string `json:"signal_daemon_url" mapstructure:"signal_daemon_url"`
	EmailFromAddress string `json:"email_from_address" mapstructure:"email_from_address"`
}

// SecurityConfig holds filesystem and sandbox settings.
type SecurityConfig struct {
	WorkspaceOnly        bool     `json:"workspace_only" mapstructure:"workspace_only"`
	DenyOnEmptyAllowlist bool     `json:"deny_on_empty_allowlist" mapstructure:"deny_on_empty_allowlist"`
	AllowedPaths         []string `json:"allowed_paths" mapstructure:"allowed_paths"`
	ProtectedPaths       []string `json:"protected_paths" mapstructure:"protected_paths"`
	InterpreterAllow     []string `json:"interpreter_allow" mapstructure:"interpreter_allow"`
	ScriptAllowedPaths   []string `json:"script_allowed_paths" mapstructure:"script_allowed_paths"`
	ThreatCautionCeiling string   `json:"threat_caution_ceiling,omitempty" mapstructure:"threat_caution_ceiling"`
}

// RevenueConfig holds revenue settlement settings.
type RevenueConfig struct {
	Enabled           bool    `json:"enabled" mapstructure:"enabled"`
	TaxRate           float64 `json:"tax_rate" mapstructure:"tax_rate"`
	DestinationWallet string  `json:"destination_wallet" mapstructure:"destination_wallet"`
}

// HeartbeatConfig holds heartbeat timing settings.
type HeartbeatConfig struct {
	IntervalSeconds int `json:"interval_seconds" mapstructure:"interval_seconds"`
}

// SkillsConfig holds skill discovery settings.
type SkillsConfig struct {
	Directory string `json:"directory" mapstructure:"directory"`
	WatchMode bool   `json:"watch_mode" mapstructure:"watch_mode"`
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() Config {
	home := homeDir()
	dataDir := filepath.Join(home, ".goboticus")

	return Config{
		Agent: AgentConfig{
			Name:                        "goboticus",
			Workspace:                   filepath.Join(dataDir, "workspace"),
			AutonomyMaxReactTurns:       25,
			AutonomyMaxTurnDurationSecs: 120,
		},
		Server: ServerConfig{
			Port:               DefaultServerPort,
			Bind:               DefaultServerBind,
			LogDir:             filepath.Join(dataDir, "logs"),
			CronMaxConcurrency: 4,
		},
		Database: DatabaseConfig{
			Path: filepath.Join(dataDir, "goboticus.db"),
		},
		Models: ModelsConfig{
			Primary: "claude-sonnet-4-20250514",
			Routing: RoutingConfig{
				Mode:                   "primary",
				ConfidenceThreshold:    0.9,
				EstimatedOutputTokens:  512,
				AccuracyFloor:          0.7,
				AccuracyMinObs:         10,
				CostAware:              true,
				PerProviderTimeoutSecs: 30,
				MaxTotalInferenceSecs:  90,
				MaxFallbackAttempts:    3,
			},
		},
		Providers: make(map[string]ProviderConfig),
		Memory: MemoryConfig{
			WorkingBudget:      40,
			EpisodicBudget:     25,
			SemanticBudget:     15,
			ProceduralBudget:   10,
			RelationshipBudget: 10,
		},
		Cache: CacheConfig{
			TTLSeconds:          3600,
			SimilarityThreshold: 0.85,
		},
		Treasury: TreasuryConfig{
			DailyCap:      5.0,
			PerPaymentCap: 1.0,
			TransferLimit: 1.0,
		},
		Session: SessionConfig{
			ScopeMode: "agent",
		},
		Wallet: WalletConfig{
			Path: filepath.Join(dataDir, "wallet.enc"),
		},
		Plugins: PluginsConfig{
			Dir: filepath.Join(dataDir, "plugins"),
		},
		Security: SecurityConfig{
			WorkspaceOnly:        true,
			DenyOnEmptyAllowlist: true,
		},
		Skills: SkillsConfig{
			WatchMode: true,
		},
		CORS: CORSConfig{
			MaxAgeSeconds: 3600,
		},
	}
}

// ConfigDir returns the goboticus configuration directory.
func ConfigDir() string {
	return filepath.Join(homeDir(), ".goboticus")
}

// ConfigFilePath returns the default config file path.
func ConfigFilePath() string {
	return filepath.Join(ConfigDir(), "goboticus.toml")
}

// NormalizePaths expands ~ in all path-valued fields.
func (c *Config) NormalizePaths() {
	c.Database.Path = expandTilde(c.Database.Path)
	c.Agent.Workspace = expandTilde(c.Agent.Workspace)
	c.Server.LogDir = expandTilde(c.Server.LogDir)
	c.Skills.Directory = expandTilde(c.Skills.Directory)
	c.Wallet.Path = expandTilde(c.Wallet.Path)
	c.Plugins.Dir = expandTilde(c.Plugins.Dir)

	for i, p := range c.Security.AllowedPaths {
		c.Security.AllowedPaths[i] = expandTilde(p)
	}
	for i, p := range c.Security.ProtectedPaths {
		c.Security.ProtectedPaths[i] = expandTilde(p)
	}
	for i, p := range c.Security.ScriptAllowedPaths {
		c.Security.ScriptAllowedPaths[i] = expandTilde(p)
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

// MergeBundledProviders adds default provider configs for well-known services.
// User-defined providers take precedence — bundled configs are only inserted
// if no provider with that name exists.
func (c *Config) MergeBundledProviders() {
	bundled := parseBundledProviders()
	if c.Providers == nil {
		c.Providers = make(map[string]ProviderConfig)
	}
	for name, cfg := range bundled {
		if _, exists := c.Providers[name]; !exists {
			c.Providers[name] = cfg
		}
	}
}

// parseBundledProviders decodes the embedded bundled_providers.toml.
func parseBundledProviders() map[string]ProviderConfig {
	// Parse manually since we don't want a viper dependency here.
	// The bundled file is simple enough for a lightweight parse.
	result := make(map[string]ProviderConfig)

	var current string
	var cfg ProviderConfig
	for _, line := range strings.Split(bundledProvidersTOML, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[providers.") {
			if current != "" {
				result[current] = cfg
			}
			name := strings.TrimPrefix(line, "[providers.")
			name = strings.TrimSuffix(name, "]")
			current = name
			cfg = ProviderConfig{}
			continue
		}
		if current == "" {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])
		val = strings.Trim(val, "\"")
		switch key {
		case "url":
			cfg.URL = val
		case "tier":
			cfg.Tier = val
		case "format":
			cfg.Format = val
		case "api_key_env":
			cfg.APIKeyEnv = val
		case "is_local":
			cfg.IsLocal = val == "true"
		case "auth_header":
			cfg.AuthHeader = val
		case "embedding_path":
			cfg.EmbeddingPath = val
		case "embedding_model":
			cfg.EmbeddingModel = val
		case "embedding_dimensions":
			_, _ = fmt.Sscanf(val, "%d", &cfg.EmbeddingDimensions)
		}
	}
	if current != "" {
		result[current] = cfg
	}
	return result
}

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

	// Treasury constraints.
	if c.Treasury.PerPaymentCap <= 0 {
		return fmt.Errorf("%w: treasury.per_payment_cap must be > 0", ErrConfig)
	}
	if c.Treasury.MinimumReserve < 0 {
		return fmt.Errorf("%w: treasury.minimum_reserve must be >= 0", ErrConfig)
	}

	// Security.
	if !c.Security.DenyOnEmptyAllowlist {
		return fmt.Errorf("%w: security.deny_on_empty_allowlist=false is not allowed (removed feature)", ErrConfig)
	}
	for _, p := range c.Security.ScriptAllowedPaths {
		if !filepath.IsAbs(p) {
			return fmt.Errorf("%w: security.script_allowed_paths entries must be absolute, got %q", ErrConfig, p)
		}
	}

	// Routing constraints.
	r := c.Models.Routing
	switch r.Mode {
	case "primary", "metascore", "":
		// valid
	default:
		return fmt.Errorf("%w: models.routing.mode must be 'primary' or 'metascore', got %q", ErrConfig, r.Mode)
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
	if r.PerProviderTimeoutSecs > 0 && r.PerProviderTimeoutSecs < 5 {
		return fmt.Errorf("%w: models.routing.per_provider_timeout_seconds must be >= 5", ErrConfig)
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

	// Channel phone number format warnings (E.164).
	warnE164 := func(field, value string) {
		if value != "" && !strings.HasPrefix(value, "+") {
			fmt.Fprintf(os.Stderr, "warning: %s should be in E.164 format (e.g., +1234567890), got %q\n", field, value)
		}
	}
	warnE164("channels.signal_account", c.Channels.SignalAccount)

	return nil
}

// expandTilde replaces a leading ~ with the user's home directory.
func expandTilde(path string) string {
	if !strings.HasPrefix(path, "~") {
		return path
	}
	return filepath.Join(homeDir(), strings.TrimPrefix(path, "~"))
}

func homeDir() string {
	if runtime.GOOS == "windows" {
		if h := os.Getenv("USERPROFILE"); h != "" {
			return h
		}
	}
	if h := os.Getenv("HOME"); h != "" {
		return h
	}
	return "."
}
