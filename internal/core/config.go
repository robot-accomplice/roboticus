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

	"github.com/pelletier/go-toml/v2"
)

var (
	tomlUnmarshal = toml.Unmarshal
	tomlMarshal   = toml.Marshal
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

	// New roboticus-compatible sections.
	CircuitBreaker           CircuitBreakerConfig `json:"circuit_breaker" mapstructure:"circuit_breaker"`
	SelfFunding              SelfFundingConfig    `json:"self_funding" mapstructure:"self_funding"`
	Yield                    YieldConfig          `json:"yield" mapstructure:"yield"`
	A2A                      A2AConfig            `json:"a2a" mapstructure:"a2a"`
	Context                  ContextConfig        `json:"context" mapstructure:"context"`
	Browser                  BrowserConfig        `json:"browser" mapstructure:"browser"`
	Daemon                   DaemonConfig         `json:"daemon" mapstructure:"daemon"`
	Update                   UpdateConfig         `json:"update" mapstructure:"update"`
	TierAdapt                TierAdaptConfig      `json:"tier_adapt" mapstructure:"tier_adapt"`
	Personality              PersonalityConfig    `json:"personality" mapstructure:"personality"`
	Digest                   DigestConfig         `json:"digest" mapstructure:"digest"`
	Learning                 LearningConfig       `json:"learning" mapstructure:"learning"`
	Multimodal               MultimodalConfig     `json:"multimodal" mapstructure:"multimodal"`
	Knowledge                KnowledgeConfig      `json:"knowledge" mapstructure:"knowledge"`
	Workspace                WorkspaceCfg         `json:"workspace" mapstructure:"workspace"`
	Devices                  DeviceConfig         `json:"devices" mapstructure:"devices"`
	Discovery                DiscoveryConfig      `json:"discovery" mapstructure:"discovery"`
	Obsidian                 ObsidianConfig       `json:"obsidian" mapstructure:"obsidian"`
	Backups                  BackupsConfig        `json:"backups" mapstructure:"backups"`
	ContextBudget            ContextBudgetConfig  `json:"context_budget" mapstructure:"context_budget"`
	DisabledBundledProviders []string             `json:"disabled_bundled_providers" mapstructure:"disabled_bundled_providers"`
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
	Name                        string  `json:"name" mapstructure:"name"`
	ID                          string  `json:"id" mapstructure:"id"`
	Workspace                   string  `json:"workspace" mapstructure:"workspace"`
	AutonomyMaxReactTurns       int     `json:"autonomy_max_react_turns" mapstructure:"autonomy_max_react_turns"`
	AutonomyMaxTurnDurationSecs int     `json:"autonomy_max_turn_duration_seconds" mapstructure:"autonomy_max_turn_duration_seconds"`
	LogLevel                    string  `json:"log_level" mapstructure:"log_level"`
	DelegationEnabled           bool    `json:"delegation_enabled" mapstructure:"delegation_enabled"`
	DelegationMinComplexity     float64 `json:"delegation_min_complexity" mapstructure:"delegation_min_complexity"`
	CompositionPolicy           string  `json:"composition_policy" mapstructure:"composition_policy"`
}

// ServerConfig holds HTTP server settings.
type ServerConfig struct {
	Port               int      `json:"port" mapstructure:"port"`
	Bind               string   `json:"bind" mapstructure:"bind"`
	LogDir             string   `json:"log_dir" mapstructure:"log_dir"`
	CronMaxConcurrency int      `json:"cron_max_concurrency" mapstructure:"cron_max_concurrency"`
	APIKey             string   `json:"api_key" mapstructure:"api_key"`
	LogMaxDays         int      `json:"log_max_days" mapstructure:"log_max_days"`
	TrustedProxyCIDRs  []string `json:"trusted_proxy_cidrs" mapstructure:"trusted_proxy_cidrs"`
}

// DatabaseConfig holds SQLite connection settings.
type DatabaseConfig struct {
	Path string `json:"path" mapstructure:"path"`
}

// ModelsConfig holds LLM provider and model settings.
type ModelsConfig struct {
	Primary         string                   `json:"primary" mapstructure:"primary"`
	Fallback        []string                 `json:"fallback,omitempty" toml:"fallbacks" mapstructure:"fallbacks"`
	Routing         RoutingConfig            `json:"routing" mapstructure:"routing"`
	ModelOverrides  map[string]ModelOverride `json:"model_overrides,omitempty" mapstructure:"model_overrides"`
	StreamByDefault bool                     `json:"stream_by_default" mapstructure:"stream_by_default"`
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
	LocalFirst             bool     `json:"local_first" mapstructure:"local_first"`
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
	AuthMode            string            `json:"auth_mode,omitempty" mapstructure:"auth_mode"`
	OAuthClientID       string            `json:"oauth_client_id,omitempty" mapstructure:"oauth_client_id"`
	OAuthRedirectURI    string            `json:"oauth_redirect_uri,omitempty" mapstructure:"oauth_redirect_uri"`
	APIKeyRef           string            `json:"api_key_ref,omitempty" mapstructure:"api_key_ref"`
}

// SessionConfig holds session scoping and timeout settings.
type SessionConfig struct {
	ScopeMode  string `json:"scope_mode" mapstructure:"scope_mode"`
	TTLSeconds int    `json:"ttl_seconds" mapstructure:"ttl_seconds"`
}

// MemoryConfig holds memory budget settings as percentages (must sum to 100).
// WorkingBudgetPct is an alias for WorkingBudget for roboticus compatibility.
type MemoryConfig struct {
	WorkingBudget      float64 `json:"working_budget" mapstructure:"working_budget"`
	WorkingBudgetPct   float64 `json:"working_budget_pct,omitempty" mapstructure:"working_budget_pct"`
	EpisodicBudget     float64 `json:"episodic_budget" mapstructure:"episodic_budget"`
	SemanticBudget     float64 `json:"semantic_budget" mapstructure:"semantic_budget"`
	ProceduralBudget   float64 `json:"procedural_budget" mapstructure:"procedural_budget"`
	RelationshipBudget float64 `json:"relationship_budget" mapstructure:"relationship_budget"`
	EmbeddingProvider  string  `json:"embedding_provider,omitempty" mapstructure:"embedding_provider"`
	EmbeddingModel     string  `json:"embedding_model,omitempty" mapstructure:"embedding_model"`
	HybridWeight       float64 `json:"hybrid_weight" mapstructure:"hybrid_weight"`
	AnnIndex           bool    `json:"ann_index" mapstructure:"ann_index"`
	DecayHalfLifeDays  float64 `json:"decay_half_life_days" mapstructure:"decay_half_life_days"`
}

// CacheConfig holds semantic cache settings.
type CacheConfig struct {
	Enabled             bool    `json:"enabled" mapstructure:"enabled"`
	TTLSeconds          int     `json:"ttl_seconds" mapstructure:"ttl_seconds"`
	SimilarityThreshold float64 `json:"similarity_threshold" mapstructure:"similarity_threshold"`
	MaxEntries          int     `json:"max_entries" mapstructure:"max_entries"`
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
	Path    string `json:"path" mapstructure:"path"`
	ChainID uint64 `json:"chain_id" mapstructure:"chain_id"`
	RPCURL  string `json:"rpc_url" mapstructure:"rpc_url"`
}

// PluginsConfig holds plugin discovery settings.
type PluginsConfig struct {
	Dir               string   `json:"dir" mapstructure:"dir"`
	Allow             []string `json:"allow,omitempty" mapstructure:"allow"`
	Deny              []string `json:"deny,omitempty" mapstructure:"deny"`
	StrictPermissions bool     `json:"strict_permissions" mapstructure:"strict_permissions"`
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

// CircuitBreakerConfig holds circuit breaker settings.
type CircuitBreakerConfig struct {
	Threshold          int `json:"threshold" mapstructure:"threshold"`
	WindowSeconds      int `json:"window_seconds" mapstructure:"window_seconds"`
	CooldownSeconds    int `json:"cooldown_seconds" mapstructure:"cooldown_seconds"`
	MaxCooldownSeconds int `json:"max_cooldown_seconds" mapstructure:"max_cooldown_seconds"`
}

// SelfFundingTaxConfig holds self-funding tax settings.
type SelfFundingTaxConfig struct {
	Enabled           bool    `json:"enabled" mapstructure:"enabled"`
	Rate              float64 `json:"rate" mapstructure:"rate"`
	DestinationWallet string  `json:"destination_wallet" mapstructure:"destination_wallet"`
}

// SelfFundingConfig holds self-funding settings.
type SelfFundingConfig struct {
	Tax SelfFundingTaxConfig `json:"tax" mapstructure:"tax"`
}

// YieldConfig holds DeFi yield settings.
type YieldConfig struct {
	Enabled             bool    `json:"enabled" mapstructure:"enabled"`
	Protocol            string  `json:"protocol" mapstructure:"protocol"`
	Chain               string  `json:"chain" mapstructure:"chain"`
	MinDeposit          float64 `json:"min_deposit" mapstructure:"min_deposit"`
	WithdrawalThreshold float64 `json:"withdrawal_threshold" mapstructure:"withdrawal_threshold"`
	ChainRPCURL         string  `json:"chain_rpc_url" mapstructure:"chain_rpc_url"`
	PoolAddress         string  `json:"pool_address" mapstructure:"pool_address"`
	USDCAddress         string  `json:"usdc_address" mapstructure:"usdc_address"`
	ATokenAddress       string  `json:"atoken_address" mapstructure:"atoken_address"`
}

// A2AConfig holds agent-to-agent protocol settings.
type A2AConfig struct {
	Enabled                bool `json:"enabled" mapstructure:"enabled"`
	MaxMessageSize         int  `json:"max_message_size" mapstructure:"max_message_size"`
	RateLimitPerPeer       int  `json:"rate_limit_per_peer" mapstructure:"rate_limit_per_peer"`
	SessionTimeoutSeconds  int  `json:"session_timeout_seconds" mapstructure:"session_timeout_seconds"`
	RequireOnChainIdentity bool `json:"require_on_chain_identity" mapstructure:"require_on_chain_identity"`
	NonceTTLSeconds        int  `json:"nonce_ttl_seconds" mapstructure:"nonce_ttl_seconds"`
}

// ContextConfig holds context window management settings.
type ContextConfig struct {
	MaxTokens               int     `json:"max_tokens" mapstructure:"max_tokens"`
	SoftTrimRatio           float64 `json:"soft_trim_ratio" mapstructure:"soft_trim_ratio"`
	HardClearRatio          float64 `json:"hard_clear_ratio" mapstructure:"hard_clear_ratio"`
	PreserveRecent          int     `json:"preserve_recent" mapstructure:"preserve_recent"`
	CheckpointEnabled       bool    `json:"checkpoint_enabled" mapstructure:"checkpoint_enabled"`
	CheckpointIntervalTurns int     `json:"checkpoint_interval_turns" mapstructure:"checkpoint_interval_turns"`
}

// BrowserConfig holds headless browser / CDP settings.
type BrowserConfig struct {
	CDPPort        int `json:"cdp_port" mapstructure:"cdp_port"`
	TimeoutSeconds int `json:"timeout_seconds" mapstructure:"timeout_seconds"`
}

// DaemonConfig holds background daemon settings.
type DaemonConfig struct {
	AutoRestart bool   `json:"auto_restart" mapstructure:"auto_restart"`
	PIDFile     string `json:"pid_file" mapstructure:"pid_file"`
}

// UpdateConfig holds auto-update settings.
type UpdateConfig struct {
	Enabled            bool `json:"enabled" mapstructure:"enabled"`
	CheckIntervalHours int  `json:"check_interval_hours" mapstructure:"check_interval_hours"`
}

// TierAdaptConfig holds adaptive tier settings.
type TierAdaptConfig struct {
	Enabled bool `json:"enabled" mapstructure:"enabled"`
}

// PersonalityConfig holds personality file paths.
type PersonalityConfig struct {
	OSPath       string `json:"os_path" mapstructure:"os_path"`
	FirmwarePath string `json:"firmware_path" mapstructure:"firmware_path"`
	OperatorPath string `json:"operator_path" mapstructure:"operator_path"`
}

// DigestConfig holds conversation digest settings.
type DigestConfig struct {
	Enabled  bool `json:"enabled" mapstructure:"enabled"`
	MinTurns int  `json:"min_turns" mapstructure:"min_turns"`
}

// LearningConfig holds pattern learning settings.
type LearningConfig struct {
	Enabled           bool `json:"enabled" mapstructure:"enabled"`
	MinSequenceLength int  `json:"min_sequence_length" mapstructure:"min_sequence_length"`
}

// MultimodalConfig holds multimodal input settings.
type MultimodalConfig struct {
	VisionEnabled bool `json:"vision_enabled" mapstructure:"vision_enabled"`
	AudioEnabled  bool `json:"audio_enabled" mapstructure:"audio_enabled"`
}

// KnowledgeConfig holds knowledge base settings.
type KnowledgeConfig struct {
	SourcesDir string `json:"sources_dir" mapstructure:"sources_dir"`
	Enabled    bool   `json:"enabled" mapstructure:"enabled"`
}

// WorkspaceCfg holds workspace indexing settings (distinct from SandboxCfg).
type WorkspaceCfg struct {
	IndexingEnabled bool `json:"indexing_enabled" mapstructure:"indexing_enabled"`
}

// DeviceConfig holds device pairing settings.
type DeviceConfig struct {
	PairingEnabled bool `json:"pairing_enabled" mapstructure:"pairing_enabled"`
}

// DiscoveryConfig holds network discovery settings.
type DiscoveryConfig struct {
	MDNSEnabled bool `json:"mdns_enabled" mapstructure:"mdns_enabled"`
}

// ObsidianConfig holds Obsidian vault integration settings.
type ObsidianConfig struct {
	VaultPath string `json:"vault_path" mapstructure:"vault_path"`
	Enabled   bool   `json:"enabled" mapstructure:"enabled"`
}

// BackupsConfig holds backup settings.
type BackupsConfig struct {
	Enabled       bool `json:"enabled" mapstructure:"enabled"`
	RetentionDays int  `json:"retention_days" mapstructure:"retention_days"`
}

// ContextBudgetConfig holds per-layer context budget settings.
type ContextBudgetConfig struct {
	L0                int     `json:"l0" mapstructure:"l0"`
	L1                int     `json:"l1" mapstructure:"l1"`
	L2                int     `json:"l2" mapstructure:"l2"`
	L3                int     `json:"l3" mapstructure:"l3"`
	ChannelMinimum    string  `json:"channel_minimum" mapstructure:"channel_minimum"`
	SoulMaxContextPct float64 `json:"soul_max_context_pct" mapstructure:"soul_max_context_pct"`
}

// ModelOverride holds per-model override settings.
type ModelOverride struct {
	MaxTokens   int     `json:"max_tokens,omitempty" mapstructure:"max_tokens"`
	Temperature float64 `json:"temperature,omitempty" mapstructure:"temperature"`
	TopP        float64 `json:"top_p,omitempty" mapstructure:"top_p"`
	Provider    string  `json:"provider,omitempty" mapstructure:"provider"`
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() Config {
	home := homeDir()
	dataDir := filepath.Join(home, ".goboticus")

	return Config{
		Agent: AgentConfig{
			Name:                        "goboticus",
			ID:                          "goboticus-default",
			Workspace:                   filepath.Join(dataDir, "workspace"),
			AutonomyMaxReactTurns:       25,
			AutonomyMaxTurnDurationSecs: 120,
			LogLevel:                    "info",
			DelegationEnabled:           true,
			DelegationMinComplexity:     0.35,
			CompositionPolicy:           "propose",
		},
		Server: ServerConfig{
			Port:               DefaultServerPort,
			Bind:               DefaultServerBind,
			LogDir:             filepath.Join(dataDir, "logs"),
			CronMaxConcurrency: 8,
			LogMaxDays:         7,
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
				AccuracyFloor:          0.0,
				AccuracyMinObs:         10,
				CostAware:              false,
				PerProviderTimeoutSecs: 30,
				MaxTotalInferenceSecs:  120,
				MaxFallbackAttempts:    6,
				LocalFirst:             true,
			},
		},
		Providers: make(map[string]ProviderConfig),
		Memory: MemoryConfig{
			WorkingBudget:      40,
			EpisodicBudget:     25,
			SemanticBudget:     15,
			ProceduralBudget:   10,
			RelationshipBudget: 10,
			HybridWeight:       0.5,
			DecayHalfLifeDays:  7.0,
		},
		Cache: CacheConfig{
			Enabled:             true,
			TTLSeconds:          3600,
			SimilarityThreshold: 0.85,
			MaxEntries:          10000,
		},
		Treasury: TreasuryConfig{
			DailyCap:      5.0,
			PerPaymentCap: 100.0,
			TransferLimit: 1.0,
		},
		Session: SessionConfig{
			ScopeMode:  "peer",
			TTLSeconds: 86400,
		},
		Wallet: WalletConfig{
			Path:    filepath.Join(dataDir, "wallet.enc"),
			ChainID: 8453,
			RPCURL:  "https://mainnet.base.org",
		},
		Plugins: PluginsConfig{
			Dir:               filepath.Join(dataDir, "plugins"),
			StrictPermissions: true,
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
		Approvals: ApprovalsConfig{
			TimeoutSeconds: 300,
		},
		CircuitBreaker: CircuitBreakerConfig{
			Threshold:          3,
			WindowSeconds:      60,
			CooldownSeconds:    60,
			MaxCooldownSeconds: 900,
		},
		SelfFunding: SelfFundingConfig{
			Tax: SelfFundingTaxConfig{
				Enabled: false,
				Rate:    0.0,
			},
		},
		Yield: YieldConfig{
			Enabled:             false,
			Protocol:            "aave",
			Chain:               "base",
			MinDeposit:          50.0,
			WithdrawalThreshold: 30.0,
		},
		A2A: A2AConfig{
			Enabled:                true,
			MaxMessageSize:         65536,
			RateLimitPerPeer:       10,
			SessionTimeoutSeconds:  3600,
			RequireOnChainIdentity: true,
			NonceTTLSeconds:        7200,
		},
		Context: ContextConfig{
			MaxTokens:               128000,
			SoftTrimRatio:           0.8,
			HardClearRatio:          0.95,
			PreserveRecent:          10,
			CheckpointEnabled:       false,
			CheckpointIntervalTurns: 10,
		},
		Browser: BrowserConfig{
			CDPPort:        9222,
			TimeoutSeconds: 30,
		},
		Daemon: DaemonConfig{
			AutoRestart: false,
		},
		Update: UpdateConfig{
			Enabled:            true,
			CheckIntervalHours: 24,
		},
		TierAdapt: TierAdaptConfig{
			Enabled: false,
		},
		Digest: DigestConfig{
			Enabled:  true,
			MinTurns: 3,
		},
		Learning: LearningConfig{
			Enabled:           true,
			MinSequenceLength: 3,
		},
		Multimodal: MultimodalConfig{
			VisionEnabled: false,
			AudioEnabled:  false,
		},
		Knowledge: KnowledgeConfig{
			Enabled: false,
		},
		Workspace: WorkspaceCfg{
			IndexingEnabled: false,
		},
		Devices: DeviceConfig{
			PairingEnabled: false,
		},
		Discovery: DiscoveryConfig{
			MDNSEnabled: false,
		},
		Obsidian: ObsidianConfig{
			Enabled: false,
		},
		Backups: BackupsConfig{
			Enabled:       false,
			RetentionDays: 30,
		},
		ContextBudget: ContextBudgetConfig{
			L0:                8000,
			L1:                8000,
			L2:                16000,
			L3:                32000,
			ChannelMinimum:    "L1",
			SoulMaxContextPct: 0.4,
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
	c.Personality.OSPath = expandTilde(c.Personality.OSPath)
	c.Personality.FirmwarePath = expandTilde(c.Personality.FirmwarePath)
	c.Personality.OperatorPath = expandTilde(c.Personality.OperatorPath)
	c.Knowledge.SourcesDir = expandTilde(c.Knowledge.SourcesDir)
	c.Obsidian.VaultPath = expandTilde(c.Obsidian.VaultPath)
	c.Daemon.PIDFile = expandTilde(c.Daemon.PIDFile)

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

	var current string     // e.g. "ollama"
	var extraTarget string // non-empty when inside [providers.<name>.extra_headers]
	var cfg ProviderConfig
	for _, line := range strings.Split(bundledProvidersTOML, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[providers.") {
			// Flush the previous provider.
			if current != "" {
				result[current] = cfg
			}
			inner := strings.TrimPrefix(line, "[providers.")
			inner = strings.TrimSuffix(inner, "]")
			// Check for sub-table like "anthropic.extra_headers".
			if strings.HasSuffix(inner, ".extra_headers") {
				provName := strings.TrimSuffix(inner, ".extra_headers")
				extraTarget = provName
				// Retrieve the already-stored provider to attach headers.
				if prev, ok := result[provName]; ok {
					current = provName
					cfg = prev
				}
			} else {
				extraTarget = ""
				current = inner
				cfg = ProviderConfig{}
			}
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

		// If we are inside an extra_headers sub-table, store as header.
		if extraTarget != "" {
			if cfg.ExtraHeaders == nil {
				cfg.ExtraHeaders = make(map[string]string)
			}
			cfg.ExtraHeaders[key] = val
			continue
		}

		switch key {
		case "url":
			cfg.URL = val
		case "tier":
			cfg.Tier = val
		case "format":
			cfg.Format = val
		case "chat_path":
			cfg.ChatPath = val
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
		case "cost_per_input_token":
			_, _ = fmt.Sscanf(val, "%f", &cfg.CostPerInputToken)
		case "cost_per_output_token":
			_, _ = fmt.Sscanf(val, "%f", &cfg.CostPerOutputToken)
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

	// Channel phone number format warnings (E.164).
	warnE164 := func(field, value string) {
		if value != "" && !strings.HasPrefix(value, "+") {
			fmt.Fprintf(os.Stderr, "warning: %s should be in E.164 format (e.g., +1234567890), got %q\n", field, value)
		}
	}
	warnE164("channels.signal_account", c.Channels.SignalAccount)

	return nil
}

// LoadConfigFromFile reads and parses a TOML config file into a Config struct.
// It starts with DefaultConfig and overlays values from the file.
func LoadConfigFromFile(path string) (Config, error) {
	cfg := DefaultConfig()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return cfg, fmt.Errorf("read config: %w", err)
	}
	if err := tomlUnmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("parse config %s: %w", path, err)
	}
	// Resolve roboticus alias: working_budget_pct overrides working_budget when set.
	if cfg.Memory.WorkingBudgetPct != 0 {
		cfg.Memory.WorkingBudget = cfg.Memory.WorkingBudgetPct
	}
	return cfg, nil
}

// MarshalTOML serialises a Config as TOML bytes.
func MarshalTOML(cfg *Config) ([]byte, error) {
	return tomlMarshal(cfg)
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
