package core

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

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
	Revenue       RevenueConfig             `json:"revenue" mapstructure:"revenue"`
	Heartbeat     HeartbeatConfig           `json:"heartbeat" mapstructure:"heartbeat"`

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

// MCPConfig holds MCP (Model Context Protocol) server configuration.
type MCPConfig struct {
	Servers []MCPServerEntry `json:"servers" mapstructure:"servers"`
}

// MCPServerEntry defines an MCP server to connect to.
type MCPServerEntry struct {
	Name          string            `json:"name" mapstructure:"name"`
	Transport     string            `json:"transport" mapstructure:"transport"` // "stdio" or "sse"
	Command       string            `json:"command" mapstructure:"command"`
	Args          []string          `json:"args" mapstructure:"args"`
	URL           string            `json:"url" mapstructure:"url"`
	Env           map[string]string `json:"env" mapstructure:"env"`
	Enabled       bool              `json:"enabled" mapstructure:"enabled"`
	AuthTokenEnv  string            `json:"auth_token_env,omitempty" mapstructure:"auth_token_env"`
	ToolAllowlist []string          `json:"tool_allowlist,omitempty" mapstructure:"tool_allowlist"`
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
// Rust parity: crates/roboticus-core/src/config/agent_paths.rs
type AgentConfig struct {
	Name                        string  `json:"name" mapstructure:"name"`
	ID                          string  `json:"id" mapstructure:"id"`
	Workspace                   string  `json:"workspace" mapstructure:"workspace"`
	AutonomyMaxReactTurns       int     `json:"autonomy_max_react_turns" mapstructure:"autonomy_max_react_turns"`
	AutonomyMaxTurnDurationSecs int     `json:"autonomy_max_turn_duration_seconds" mapstructure:"autonomy_max_turn_duration_seconds"`
	LogLevel                    string  `json:"log_level" mapstructure:"log_level"`
	DelegationEnabled           bool    `json:"delegation_enabled" mapstructure:"delegation_enabled"`
	DelegationMinComplexity     float64 `json:"delegation_min_complexity" mapstructure:"delegation_min_complexity"`
	DelegationMinUtilityMargin  float64 `json:"delegation_min_utility_margin" mapstructure:"delegation_min_utility_margin"`     // Rust parity: 0.15 default
	SpecialistRequiresApproval  bool    `json:"specialist_creation_requires_approval" mapstructure:"specialist_creation_requires_approval"` // Rust parity: true
	CompositionPolicy           string  `json:"composition_policy" mapstructure:"composition_policy"`
	SkillCreationRigor          string  `json:"skill_creation_rigor" mapstructure:"skill_creation_rigor"`                       // generate|validate|full (Rust parity)
	OutputValidationPolicy      string  `json:"output_validation_policy" mapstructure:"output_validation_policy"`               // strict|sample|off (Rust parity)
	OutputValidationSampleRate  float64 `json:"output_validation_sample_rate" mapstructure:"output_validation_sample_rate"`     // Rust parity: 0.1 default
	MaxOutputRetries            int     `json:"max_output_retries" mapstructure:"max_output_retries"`                           // Rust parity: 2 default
	RetirementSuccessThreshold  float64 `json:"retirement_success_threshold" mapstructure:"retirement_success_threshold"`       // Rust parity: 0.7 default
	RetirementMinDelegations    int     `json:"retirement_min_delegations" mapstructure:"retirement_min_delegations"`           // Rust parity: 10 default
}

// ServerConfig holds HTTP server settings.
type ServerConfig struct {
	Port                      int      `json:"port" mapstructure:"port"`
	Bind                      string   `json:"bind" mapstructure:"bind"`
	LogDir                    string   `json:"log_dir" mapstructure:"log_dir"`
	CronMaxConcurrency        int      `json:"cron_max_concurrency" mapstructure:"cron_max_concurrency"`
	APIKey                    string   `json:"api_key" mapstructure:"api_key"`
	LogMaxDays                int      `json:"log_max_days" mapstructure:"log_max_days"`
	TrustedProxyCIDRs         []string `json:"trusted_proxy_cidrs" mapstructure:"trusted_proxy_cidrs"`
	RateLimitRequests         int      `json:"rate_limit_requests" mapstructure:"rate_limit_requests"`
	RateLimitWindowSecs       int      `json:"rate_limit_window_secs" mapstructure:"rate_limit_window_secs"`
	PerIPRateLimitRequests    int      `json:"per_ip_rate_limit_requests" mapstructure:"per_ip_rate_limit_requests"`
	PerActorRateLimitRequests int      `json:"per_actor_rate_limit_requests" mapstructure:"per_actor_rate_limit_requests"`
}

// DatabaseConfig holds SQLite connection settings.
type DatabaseConfig struct {
	Path string `json:"path" mapstructure:"path"`
}

// ModelsConfig holds LLM provider and model settings.
type ModelsConfig struct {
	Primary          string                   `json:"primary" mapstructure:"primary"`
	Fallback         []string                 `json:"fallbacks,omitempty" toml:"fallbacks" mapstructure:"fallbacks"`
	Routing          RoutingConfig            `json:"routing" mapstructure:"routing"`
	ModelOverrides   map[string]ModelOverride  `json:"model_overrides,omitempty" mapstructure:"model_overrides"`
	StreamByDefault  bool                     `json:"stream_by_default" mapstructure:"stream_by_default"`
	TieredInference  TieredInferenceConfig    `json:"tiered_inference" mapstructure:"tiered_inference"`
	Timeouts         map[string]int           `json:"timeouts,omitempty" mapstructure:"timeouts"` // per-model timeout in seconds (e.g. "qwen2.5:32b": 300)
	ToolBlocklist    []string                 `json:"tool_blocklist,omitempty" mapstructure:"tool_blocklist"` // models that don't support tools
	ToolAllowlist    []string                 `json:"tool_allowlist,omitempty" mapstructure:"tool_allowlist"` // override: force tool support
}

// ResolveModelTimeout returns the timeout for a specific model.
// Priority: per-model Timeouts map → local provider default (300s) → global default (120s).
func (mc ModelsConfig) ResolveModelTimeout(model string) int {
	if mc.Timeouts != nil {
		if t, ok := mc.Timeouts[model]; ok && t > 0 {
			return t
		}
	}
	// Default: 120s for cloud, 300s for local (caller determines based on provider).
	return 0 // 0 means "use caller default"
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
	LocalFirst             bool                `json:"local_first" mapstructure:"local_first"`
	Profile                *RoutingProfileData `json:"profile,omitempty" mapstructure:"profile"` // persisted 6-axis profile (lossless)
}

// RoutingProfileData holds the 6-axis metascore profile for direct persistence.
// Avoids lossy round-trip through derived config fields.
type RoutingProfileData struct {
	Efficacy     float64 `json:"efficacy" mapstructure:"efficacy"`
	Cost         float64 `json:"cost" mapstructure:"cost"`
	Availability float64 `json:"availability" mapstructure:"availability"`
	Locality     float64 `json:"locality" mapstructure:"locality"`
	Confidence   float64 `json:"confidence" mapstructure:"confidence"`
	Speed        float64 `json:"speed" mapstructure:"speed"`
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
	TimeoutSecs         int               `json:"timeout_seconds,omitempty" mapstructure:"timeout_seconds"`
}

// SessionConfig holds session scoping and timeout settings.
type SessionConfig struct {
	ScopeMode     string `json:"scope_mode" mapstructure:"scope_mode"`
	TTLSeconds    int    `json:"ttl_seconds" mapstructure:"ttl_seconds"`
	ResetSchedule string `json:"reset_schedule,omitempty" mapstructure:"reset_schedule"`
}

// MemoryConfig holds memory budget settings as percentages (must sum to 100).
// WorkingBudgetPct is an alias for WorkingBudget for roboticus compatibility.
type MemoryConfig struct {
	WorkingBudget            float64 `json:"working_budget" mapstructure:"working_budget"`
	WorkingBudgetPct         float64 `json:"working_budget_pct,omitempty" mapstructure:"working_budget_pct"`
	EpisodicBudget           float64 `json:"episodic_budget" mapstructure:"episodic_budget"`
	SemanticBudget           float64 `json:"semantic_budget" mapstructure:"semantic_budget"`
	ProceduralBudget         float64 `json:"procedural_budget" mapstructure:"procedural_budget"`
	RelationshipBudget       float64 `json:"relationship_budget" mapstructure:"relationship_budget"`
	EmbeddingProvider        string  `json:"embedding_provider,omitempty" mapstructure:"embedding_provider"`
	EmbeddingModel           string  `json:"embedding_model,omitempty" mapstructure:"embedding_model"`
	HybridWeight             float64 `json:"hybrid_weight" mapstructure:"hybrid_weight"`
	AnnIndex                 bool    `json:"ann_index" mapstructure:"ann_index"`
	DecayHalfLifeDays        float64 `json:"decay_half_life_days" mapstructure:"decay_half_life_days"`
	SimilarityThreshold      float64 `json:"similarity_threshold" mapstructure:"similarity_threshold"`
	ANNActivationThreshold   int     `json:"ann_activation_threshold" mapstructure:"ann_activation_threshold"`
}

// CacheConfig holds semantic cache settings.
type CacheConfig struct {
	Enabled                 bool    `json:"enabled" mapstructure:"enabled"`
	TTLSeconds              int     `json:"ttl_seconds" mapstructure:"ttl_seconds"`
	SimilarityThreshold     float64 `json:"similarity_threshold" mapstructure:"similarity_threshold"`
	MaxEntries              int     `json:"max_entries" mapstructure:"max_entries"`
	PromptCompression       bool    `json:"prompt_compression" mapstructure:"prompt_compression"`
	CompressionTargetRatio  float64 `json:"compression_target_ratio" mapstructure:"compression_target_ratio"`
}

// TreasuryConfig holds financial policy limits.
type TreasuryConfig struct {
	DailyCap              float64           `json:"daily_cap" mapstructure:"daily_cap"`
	PerPaymentCap         float64           `json:"per_payment_cap" mapstructure:"per_payment_cap"`
	TransferLimit         float64           `json:"transfer_limit" mapstructure:"transfer_limit"`
	MinimumReserve        float64           `json:"minimum_reserve" mapstructure:"minimum_reserve"`
	HourlyTransferLimit   float64           `json:"hourly_transfer_limit" mapstructure:"hourly_transfer_limit"`
	DailyTransferLimit    float64           `json:"daily_transfer_limit" mapstructure:"daily_transfer_limit"`
	DailyInferenceBudget  float64           `json:"daily_inference_budget" mapstructure:"daily_inference_budget"`
	RevenueSwap           RevenueSwapConfig `json:"revenue_swap" mapstructure:"revenue_swap"`
}

// WalletConfig holds crypto wallet settings.
type WalletConfig struct {
	Path               string `json:"path" mapstructure:"path"`
	ChainID            uint64 `json:"chain_id" mapstructure:"chain_id"`
	RPCURL             string `json:"rpc_url" mapstructure:"rpc_url"`
	BalancePollSeconds int    `json:"balance_poll_seconds" mapstructure:"balance_poll_seconds"` // 0 = disabled, default 60
}

// PluginsConfig holds plugin discovery settings.
type PluginsConfig struct {
	Dir               string   `json:"dir" mapstructure:"dir"`
	Allow             []string `json:"allow,omitempty" mapstructure:"allow"`
	Deny              []string `json:"deny,omitempty" mapstructure:"deny"`
	StrictPermissions bool     `json:"strict_permissions" mapstructure:"strict_permissions"`
	CatalogURL        string   `json:"catalog_url" mapstructure:"catalog_url"`
}

// ChannelsConfig holds channel adapter settings.
// Rust parity: each channel has its own rich sub-struct with full configuration.
// Legacy flat fields are preserved for backwards compatibility.
type ChannelsConfig struct {
	// Rich per-channel configs (Rust parity: runtime_core.rs).
	Telegram *TelegramConfig `json:"telegram,omitempty" mapstructure:"telegram"`
	WhatsApp *WhatsAppConfig `json:"whatsapp,omitempty" mapstructure:"whatsapp"`
	Discord  *DiscordConfig  `json:"discord,omitempty" mapstructure:"discord"`
	Signal   *SignalConfig   `json:"signal,omitempty" mapstructure:"signal"`
	Email    *EmailConfig    `json:"email,omitempty" mapstructure:"email"`
	Voice    *VoiceConfig    `json:"voice,omitempty" mapstructure:"voice"`

	// Legacy flat fields — kept for backwards compatibility.
	// When rich sub-configs are present, they take precedence.
	TelegramTokenEnv string `json:"telegram_token_env" mapstructure:"telegram_token_env"`
	WhatsAppTokenEnv string `json:"whatsapp_token_env" mapstructure:"whatsapp_token_env"`
	DiscordTokenEnv  string `json:"discord_token_env" mapstructure:"discord_token_env"`
	SignalAccount    string `json:"signal_account" mapstructure:"signal_account"`
	SignalDaemonURL  string `json:"signal_daemon_url" mapstructure:"signal_daemon_url"`
	EmailFromAddress string `json:"email_from_address" mapstructure:"email_from_address"`
}

// TelegramConfig holds Telegram bot adapter settings.
type TelegramConfig struct {
	Enabled            bool    `json:"enabled" mapstructure:"enabled"`
	TokenEnv           string  `json:"token_env" mapstructure:"token_env"`
	TokenRef           string  `json:"token_ref,omitempty" mapstructure:"token_ref"`
	AllowedChatIDs     []int64 `json:"allowed_chat_ids,omitempty" mapstructure:"allowed_chat_ids"`
	PollTimeoutSeconds int     `json:"poll_timeout_seconds" mapstructure:"poll_timeout_seconds"`
	WebhookMode        bool    `json:"webhook_mode" mapstructure:"webhook_mode"`
	WebhookPath        string  `json:"webhook_path,omitempty" mapstructure:"webhook_path"`
	WebhookSecret      string  `json:"webhook_secret,omitempty" mapstructure:"webhook_secret"`
}

// WhatsAppConfig holds WhatsApp Cloud API adapter settings.
type WhatsAppConfig struct {
	Enabled        bool     `json:"enabled" mapstructure:"enabled"`
	TokenEnv       string   `json:"token_env" mapstructure:"token_env"`
	TokenRef       string   `json:"token_ref,omitempty" mapstructure:"token_ref"`
	PhoneNumberID  string   `json:"phone_number_id" mapstructure:"phone_number_id"`
	VerifyToken    string   `json:"verify_token" mapstructure:"verify_token"`
	AllowedNumbers []string `json:"allowed_numbers,omitempty" mapstructure:"allowed_numbers"`
	AppSecret      string   `json:"app_secret,omitempty" mapstructure:"app_secret"`
}

// DiscordConfig holds Discord bot adapter settings.
type DiscordConfig struct {
	Enabled         bool     `json:"enabled" mapstructure:"enabled"`
	TokenEnv        string   `json:"token_env" mapstructure:"token_env"`
	TokenRef        string   `json:"token_ref,omitempty" mapstructure:"token_ref"`
	ApplicationID   string   `json:"application_id" mapstructure:"application_id"`
	AllowedGuildIDs []string `json:"allowed_guild_ids,omitempty" mapstructure:"allowed_guild_ids"`
}

// SignalConfig holds Signal messenger adapter settings.
type SignalConfig struct {
	Enabled        bool     `json:"enabled" mapstructure:"enabled"`
	PhoneNumber    string   `json:"phone_number" mapstructure:"phone_number"`
	DaemonURL      string   `json:"daemon_url" mapstructure:"daemon_url"`
	AllowedNumbers []string `json:"allowed_numbers,omitempty" mapstructure:"allowed_numbers"`
}

// EmailConfig holds email (IMAP/SMTP) adapter settings.
type EmailConfig struct {
	Enabled            bool     `json:"enabled" mapstructure:"enabled"`
	IMAPHost           string   `json:"imap_host" mapstructure:"imap_host"`
	IMAPPort           int      `json:"imap_port" mapstructure:"imap_port"`
	SMTPHost           string   `json:"smtp_host" mapstructure:"smtp_host"`
	SMTPPort           int      `json:"smtp_port" mapstructure:"smtp_port"`
	Username           string   `json:"username" mapstructure:"username"`
	PasswordEnv        string   `json:"password_env" mapstructure:"password_env"`
	FromAddress        string   `json:"from_address" mapstructure:"from_address"`
	AllowedSenders     []string `json:"allowed_senders,omitempty" mapstructure:"allowed_senders"`
	PollIntervalSecs   int      `json:"poll_interval_seconds" mapstructure:"poll_interval_seconds"`
	OAuth2TokenEnv     string   `json:"oauth2_token_env,omitempty" mapstructure:"oauth2_token_env"`
	UseOAuth2          bool     `json:"use_oauth2" mapstructure:"use_oauth2"`
	IMAPIdleEnabled    bool     `json:"imap_idle_enabled" mapstructure:"imap_idle_enabled"`
}

// VoiceConfig holds voice channel adapter settings.
type VoiceConfig struct {
	Enabled  bool   `json:"enabled" mapstructure:"enabled"`
	STTModel string `json:"stt_model,omitempty" mapstructure:"stt_model"`
	TTSModel string `json:"tts_model,omitempty" mapstructure:"tts_model"`
	TTSVoice string `json:"tts_voice,omitempty" mapstructure:"tts_voice"`
}

// SecurityConfig holds filesystem and sandbox settings.
type SecurityConfig struct {
	WorkspaceOnly        bool     `json:"workspace_only" mapstructure:"workspace_only"`
	DenyOnEmptyAllowlist bool     `json:"deny_on_empty_allowlist" mapstructure:"deny_on_empty_allowlist"`
	AllowedPaths         []string `json:"allowed_paths" mapstructure:"allowed_paths"`
	ProtectedPaths       []string `json:"protected_paths" mapstructure:"protected_paths"`
	ExtraProtectedPaths  []string `json:"extra_protected_paths,omitempty" mapstructure:"extra_protected_paths"`
	InterpreterAllow     []string `json:"interpreter_allow" mapstructure:"interpreter_allow"`
	ScriptAllowedPaths   []string `json:"script_allowed_paths" mapstructure:"script_allowed_paths"`
	ThreatCautionCeiling string   `json:"threat_caution_ceiling,omitempty" mapstructure:"threat_caution_ceiling"`
	AllowlistAuthority   string   `json:"allowlist_authority,omitempty" mapstructure:"allowlist_authority"`
	TrustedAuthority     string   `json:"trusted_authority,omitempty" mapstructure:"trusted_authority"`
	APIAuthority         string   `json:"api_authority,omitempty" mapstructure:"api_authority"`
	SandboxRequired      bool     `json:"sandbox_required" mapstructure:"sandbox_required"`
	ScriptFsConfinement  bool     `json:"script_fs_confinement" mapstructure:"script_fs_confinement"` // Confine scripts to workspace directory
	// TrustedSenderIDs lists sender/chat IDs that receive Creator authority via
	// the SecurityClaim resolver's TrustedAuthority grant. Matches Rust's
	// channels.trusted_sender_ids configuration.
	TrustedSenderIDs []string `json:"trusted_sender_ids,omitempty" mapstructure:"trusted_sender_ids"`
	// Filesystem is a nested security section for fine-grained filesystem access control.
	// Mirrors Rust's security.filesystem configuration.
	Filesystem FilesystemSecurityConfig `json:"filesystem" mapstructure:"filesystem"`
}

// RevenueConfig holds revenue settlement settings.
type RevenueConfig struct {
	Enabled           bool    `json:"enabled" mapstructure:"enabled"`
	TaxRate           float64 `json:"tax_rate" mapstructure:"tax_rate"`
	DestinationWallet string  `json:"destination_wallet" mapstructure:"destination_wallet"`
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

// ModelOverride holds per-model override settings.
type ModelOverride struct {
	MaxTokens   int     `json:"max_tokens,omitempty" mapstructure:"max_tokens"`
	Temperature float64 `json:"temperature,omitempty" mapstructure:"temperature"`
	TopP        float64 `json:"top_p,omitempty" mapstructure:"top_p"`
	Provider    string  `json:"provider,omitempty" mapstructure:"provider"`
	TimeoutSecs int     `json:"timeout_seconds,omitempty" mapstructure:"timeout_seconds"`
}

// ConfigDir returns the roboticus configuration directory.
func ConfigDir() string {
	return filepath.Join(homeDir(), ".roboticus")
}

// ConfigFilePath returns the default config file path.
func ConfigFilePath() string {
	return filepath.Join(ConfigDir(), "roboticus.toml")
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
