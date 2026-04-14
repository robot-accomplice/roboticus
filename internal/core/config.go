package core

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// Config is the top-level application configuration, loaded from TOML.
type Config struct {
	Agent      AgentConfig               `json:"agent" toml:"agent" mapstructure:"agent"`
	Server     ServerConfig              `json:"server" toml:"server" mapstructure:"server"`
	Database   DatabaseConfig            `json:"database" toml:"database" mapstructure:"database"`
	Models     ModelsConfig              `json:"models" toml:"models" mapstructure:"models"`
	Providers  map[string]ProviderConfig `json:"providers" toml:"providers" mapstructure:"providers"`
	Memory     MemoryConfig              `json:"memory" toml:"memory" mapstructure:"memory"`
	Cache      CacheConfig               `json:"cache" toml:"cache" mapstructure:"cache"`
	Treasury   TreasuryConfig            `json:"treasury" toml:"treasury" mapstructure:"treasury"`
	Channels   ChannelsConfig            `json:"channels" toml:"channels" mapstructure:"channels"`
	Security   SecurityConfig            `json:"security" toml:"security" mapstructure:"security"`
	Skills     SkillsConfig              `json:"skills" toml:"skills" mapstructure:"skills"`
	Session    SessionConfig             `json:"session" toml:"session" mapstructure:"session"`
	Wallet     WalletConfig              `json:"wallet" toml:"wallet" mapstructure:"wallet"`
	Plugins    PluginsConfig             `json:"plugins" toml:"plugins" mapstructure:"plugins"`
	Approvals  ApprovalsConfig           `json:"approvals" toml:"approvals" mapstructure:"approvals"`
	Abuse      AbuseConfig               `json:"abuse" toml:"abuse" mapstructure:"abuse"`
	RateLimit  RateLimitConfig           `json:"rate_limit" toml:"rate_limit" mapstructure:"rate_limit"`
	MCP        MCPConfig                 `json:"mcp" toml:"mcp" mapstructure:"mcp"`
	Matrix     MatrixChannelConfig       `json:"matrix" toml:"matrix" mapstructure:"matrix"`
	Sandbox    SandboxCfg                `json:"sandbox" toml:"sandbox" mapstructure:"sandbox"`
	Classifier ClassifierConfig          `json:"classifier" toml:"classifier" mapstructure:"classifier"`
	Planner    PlannerConfig             `json:"planner" toml:"planner" mapstructure:"planner"`
	Themes     ThemesConfig              `json:"themes" toml:"themes" mapstructure:"themes"`
	DKIM       DKIMConfig                `json:"dkim" toml:"dkim" mapstructure:"dkim"`
	CORS       CORSConfig                `json:"cors" toml:"cors" mapstructure:"cors"`
	Revenue       RevenueConfig             `json:"revenue" toml:"revenue" mapstructure:"revenue"`
	Heartbeat     HeartbeatConfig           `json:"heartbeat" toml:"heartbeat" mapstructure:"heartbeat"`

	// New roboticus-compatible sections.
	CircuitBreaker           CircuitBreakerConfig `json:"circuit_breaker" toml:"circuit_breaker" mapstructure:"circuit_breaker"`
	SelfFunding              SelfFundingConfig    `json:"self_funding" toml:"self_funding" mapstructure:"self_funding"`
	Yield                    YieldConfig          `json:"yield" toml:"yield" mapstructure:"yield"`
	A2A                      A2AConfig            `json:"a2a" toml:"a2a" mapstructure:"a2a"`
	Context                  ContextConfig        `json:"context" toml:"context" mapstructure:"context"`
	Browser                  BrowserConfig        `json:"browser" toml:"browser" mapstructure:"browser"`
	Daemon                   DaemonConfig         `json:"daemon" toml:"daemon" mapstructure:"daemon"`
	Update                   UpdateConfig         `json:"update" toml:"update" mapstructure:"update"`
	TierAdapt                TierAdaptConfig      `json:"tier_adapt" toml:"tier_adapt" mapstructure:"tier_adapt"`
	Personality              PersonalityConfig    `json:"personality" toml:"personality" mapstructure:"personality"`
	Digest                   DigestConfig         `json:"digest" toml:"digest" mapstructure:"digest"`
	Learning                 LearningConfig       `json:"learning" toml:"learning" mapstructure:"learning"`
	Multimodal               MultimodalConfig     `json:"multimodal" toml:"multimodal" mapstructure:"multimodal"`
	Knowledge                KnowledgeConfig      `json:"knowledge" toml:"knowledge" mapstructure:"knowledge"`
	Workspace                WorkspaceCfg         `json:"workspace" toml:"workspace" mapstructure:"workspace"`
	Devices                  DeviceConfig         `json:"devices" toml:"devices" mapstructure:"devices"`
	Discovery                DiscoveryConfig      `json:"discovery" toml:"discovery" mapstructure:"discovery"`
	Obsidian                 ObsidianConfig       `json:"obsidian" toml:"obsidian" mapstructure:"obsidian"`
	Backups                  BackupsConfig        `json:"backups" toml:"backups" mapstructure:"backups"`
	ContextBudget            ContextBudgetConfig  `json:"context_budget" toml:"context_budget" mapstructure:"context_budget"`
	DisabledBundledProviders []string             `json:"disabled_bundled_providers" toml:"disabled_bundled_providers" mapstructure:"disabled_bundled_providers"`
}

// MCPConfig holds MCP (Model Context Protocol) server configuration.
type MCPConfig struct {
	Servers []MCPServerEntry `json:"servers" toml:"servers" mapstructure:"servers"`
}

// MCPServerEntry defines an MCP server to connect to.
type MCPServerEntry struct {
	Name          string            `json:"name" toml:"name" mapstructure:"name"`
	Transport     string            `json:"transport" toml:"transport" mapstructure:"transport"` // "stdio" or "sse"
	Command       string            `json:"command" toml:"command" mapstructure:"command"`
	Args          []string          `json:"args" toml:"args" mapstructure:"args"`
	URL           string            `json:"url" toml:"url" mapstructure:"url"`
	Env           map[string]string `json:"env" toml:"env" mapstructure:"env"`
	Enabled       bool              `json:"enabled" toml:"enabled" mapstructure:"enabled"`
	AuthTokenEnv  string            `json:"auth_token_env,omitempty" toml:"auth_token_env" mapstructure:"auth_token_env"`
	ToolAllowlist []string          `json:"tool_allowlist,omitempty" toml:"tool_allowlist" mapstructure:"tool_allowlist"`
}

// ApprovalsConfig controls human-in-the-loop tool gating.
type ApprovalsConfig struct {
	Enabled        bool     `json:"enabled" toml:"enabled" mapstructure:"enabled"`
	GatedTools     []string `json:"gated_tools" toml:"gated_tools" mapstructure:"gated_tools"`
	BlockedTools   []string `json:"blocked_tools" toml:"blocked_tools" mapstructure:"blocked_tools"`
	TimeoutSeconds int      `json:"timeout_seconds" toml:"timeout_seconds" mapstructure:"timeout_seconds"`
}

// AbuseConfig controls the abuse tracking system.
type AbuseConfig struct {
	Enabled             bool    `json:"enabled" toml:"enabled" mapstructure:"enabled"`
	WindowMinutes       int     `json:"window_minutes" toml:"window_minutes" mapstructure:"window_minutes"`
	SlowdownThreshold   float64 `json:"slowdown_threshold" toml:"slowdown_threshold" mapstructure:"slowdown_threshold"`
	QuarantineThreshold float64 `json:"quarantine_threshold" toml:"quarantine_threshold" mapstructure:"quarantine_threshold"`
}

// RateLimitConfig controls per-IP HTTP rate limiting.
type RateLimitConfig struct {
	Enabled           bool `json:"enabled" toml:"enabled" mapstructure:"enabled"`
	RequestsPerWindow int  `json:"requests_per_window" toml:"requests_per_window" mapstructure:"requests_per_window"`
	WindowSeconds     int  `json:"window_seconds" toml:"window_seconds" mapstructure:"window_seconds"`
}

// AgentConfig holds agent identity and workspace settings.
// Rust parity: crates/roboticus-core/src/config/agent_paths.rs
type AgentConfig struct {
	Name                        string  `json:"name" toml:"name" mapstructure:"name"`
	ID                          string  `json:"id" toml:"id" mapstructure:"id"`
	Workspace                   string  `json:"workspace" toml:"workspace" mapstructure:"workspace"`
	AutonomyMaxReactTurns       int     `json:"autonomy_max_react_turns" toml:"autonomy_max_react_turns" mapstructure:"autonomy_max_react_turns"`
	AutonomyMaxTurnDurationSecs int     `json:"autonomy_max_turn_duration_seconds" toml:"autonomy_max_turn_duration_seconds" mapstructure:"autonomy_max_turn_duration_seconds"`
	LogLevel                    string  `json:"log_level" toml:"log_level" mapstructure:"log_level"`
	DelegationEnabled           bool    `json:"delegation_enabled" toml:"delegation_enabled" mapstructure:"delegation_enabled"`
	DelegationMinComplexity     float64 `json:"delegation_min_complexity" toml:"delegation_min_complexity" mapstructure:"delegation_min_complexity"`
	DelegationMinUtilityMargin  float64 `json:"delegation_min_utility_margin" toml:"delegation_min_utility_margin" mapstructure:"delegation_min_utility_margin"`     // Rust parity: 0.15 default
	SpecialistRequiresApproval  bool    `json:"specialist_creation_requires_approval" toml:"specialist_creation_requires_approval" mapstructure:"specialist_creation_requires_approval"` // Rust parity: true
	CompositionPolicy           string  `json:"composition_policy" toml:"composition_policy" mapstructure:"composition_policy"`
	SkillCreationRigor          string  `json:"skill_creation_rigor" toml:"skill_creation_rigor" mapstructure:"skill_creation_rigor"`                       // generate|validate|full (Rust parity)
	OutputValidationPolicy      string  `json:"output_validation_policy" toml:"output_validation_policy" mapstructure:"output_validation_policy"`               // strict|sample|off (Rust parity)
	OutputValidationSampleRate  float64 `json:"output_validation_sample_rate" toml:"output_validation_sample_rate" mapstructure:"output_validation_sample_rate"`     // Rust parity: 0.1 default
	MaxOutputRetries            int     `json:"max_output_retries" toml:"max_output_retries" mapstructure:"max_output_retries"`                           // Rust parity: 2 default
	RetirementSuccessThreshold  float64 `json:"retirement_success_threshold" toml:"retirement_success_threshold" mapstructure:"retirement_success_threshold"`       // Rust parity: 0.7 default
	RetirementMinDelegations    int     `json:"retirement_min_delegations" toml:"retirement_min_delegations" mapstructure:"retirement_min_delegations"`           // Rust parity: 10 default
}

// ServerConfig holds HTTP server settings.
type ServerConfig struct {
	Port                      int      `json:"port" toml:"port" mapstructure:"port"`
	Bind                      string   `json:"bind" toml:"bind" mapstructure:"bind"`
	LogDir                    string   `json:"log_dir" toml:"log_dir" mapstructure:"log_dir"`
	CronMaxConcurrency        int      `json:"cron_max_concurrency" toml:"cron_max_concurrency" mapstructure:"cron_max_concurrency"`
	APIKey                    string   `json:"api_key" toml:"api_key" mapstructure:"api_key"`
	LogMaxDays                int      `json:"log_max_days" toml:"log_max_days" mapstructure:"log_max_days"`
	TrustedProxyCIDRs         []string `json:"trusted_proxy_cidrs" toml:"trusted_proxy_cidrs" mapstructure:"trusted_proxy_cidrs"`
	RateLimitRequests         int      `json:"rate_limit_requests" toml:"rate_limit_requests" mapstructure:"rate_limit_requests"`
	RateLimitWindowSecs       int      `json:"rate_limit_window_secs" toml:"rate_limit_window_secs" mapstructure:"rate_limit_window_secs"`
	PerIPRateLimitRequests    int      `json:"per_ip_rate_limit_requests" toml:"per_ip_rate_limit_requests" mapstructure:"per_ip_rate_limit_requests"`
	PerActorRateLimitRequests int      `json:"per_actor_rate_limit_requests" toml:"per_actor_rate_limit_requests" mapstructure:"per_actor_rate_limit_requests"`
}

// DatabaseConfig holds SQLite connection settings.
type DatabaseConfig struct {
	Path string `json:"path" toml:"path" mapstructure:"path"`
}

// ModelsConfig holds LLM provider and model settings.
type ModelsConfig struct {
	Primary          string                   `json:"primary" toml:"primary" mapstructure:"primary"`
	Fallback         []string                 `json:"fallbacks,omitempty" toml:"fallbacks" mapstructure:"fallbacks"`
	Routing          RoutingConfig            `json:"routing" toml:"routing" mapstructure:"routing"`
	ModelOverrides   map[string]ModelOverride  `json:"model_overrides,omitempty" toml:"model_overrides" mapstructure:"model_overrides"`
	StreamByDefault  bool                     `json:"stream_by_default" toml:"stream_by_default" mapstructure:"stream_by_default"`
	TieredInference  TieredInferenceConfig    `json:"tiered_inference" toml:"tiered_inference" mapstructure:"tiered_inference"`
	Timeouts         map[string]int           `json:"timeouts,omitempty" toml:"timeouts" mapstructure:"timeouts"`
	ToolBlocklist    []string                 `json:"tool_blocklist,omitempty" toml:"tool_blocklist" mapstructure:"tool_blocklist"`
	ToolAllowlist    []string                 `json:"tool_allowlist,omitempty" toml:"tool_allowlist" mapstructure:"tool_allowlist"`
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
	Mode                   string   `json:"mode" toml:"mode" mapstructure:"mode"`
	ConfidenceThreshold    float64  `json:"confidence_threshold" toml:"confidence_threshold" mapstructure:"confidence_threshold"`
	EstimatedOutputTokens  int      `json:"estimated_output_tokens" toml:"estimated_output_tokens" mapstructure:"estimated_output_tokens"`
	AccuracyFloor          float64  `json:"accuracy_floor" toml:"accuracy_floor" mapstructure:"accuracy_floor"`
	AccuracyMinObs         int      `json:"accuracy_min_obs" toml:"accuracy_min_obs" mapstructure:"accuracy_min_obs"`
	CostWeight             *float64 `json:"cost_weight,omitempty" toml:"cost_weight" mapstructure:"cost_weight"`
	CostAware              bool     `json:"cost_aware" toml:"cost_aware" mapstructure:"cost_aware"`
	CanaryFraction         float64  `json:"canary_fraction" toml:"canary_fraction" mapstructure:"canary_fraction"`
	CanaryModel            string   `json:"canary_model" toml:"canary_model" mapstructure:"canary_model"`
	BlockedModels          []string `json:"blocked_models" toml:"blocked_models" mapstructure:"blocked_models"`
	PerProviderTimeoutSecs int      `json:"per_provider_timeout_seconds" toml:"per_provider_timeout_seconds" mapstructure:"per_provider_timeout_seconds"`
	MaxTotalInferenceSecs  int      `json:"max_total_inference_seconds" toml:"max_total_inference_seconds" mapstructure:"max_total_inference_seconds"`
	MaxFallbackAttempts    int      `json:"max_fallback_attempts" toml:"max_fallback_attempts" mapstructure:"max_fallback_attempts"`
	LocalFirst             bool                `json:"local_first" toml:"local_first" mapstructure:"local_first"`
	Profile                *RoutingProfileData `json:"profile,omitempty" toml:"profile" mapstructure:"profile"` // persisted 6-axis profile (lossless)
}

// RoutingProfileData holds the 6-axis metascore profile for direct persistence.
// Avoids lossy round-trip through derived config fields.
type RoutingProfileData struct {
	Efficacy     float64 `json:"efficacy" toml:"efficacy" mapstructure:"efficacy"`
	Cost         float64 `json:"cost" toml:"cost" mapstructure:"cost"`
	Availability float64 `json:"availability" toml:"availability" mapstructure:"availability"`
	Locality     float64 `json:"locality" toml:"locality" mapstructure:"locality"`
	Confidence   float64 `json:"confidence" toml:"confidence" mapstructure:"confidence"`
	Speed        float64 `json:"speed" toml:"speed" mapstructure:"speed"`
}

// ProviderConfig describes a single LLM provider endpoint.
type ProviderConfig struct {
	URL                 string            `json:"url" toml:"url" mapstructure:"url"`
	Tier                string            `json:"tier" toml:"tier" mapstructure:"tier"`
	Format              string            `json:"format,omitempty" toml:"format" mapstructure:"format"`
	// APIKeyEnv removed — keys must come from the keystore, not environment variables.
	ChatPath            string            `json:"chat_path,omitempty" toml:"chat_path" mapstructure:"chat_path"`
	EmbeddingPath       string            `json:"embedding_path,omitempty" toml:"embedding_path" mapstructure:"embedding_path"`
	EmbeddingModel      string            `json:"embedding_model,omitempty" toml:"embedding_model" mapstructure:"embedding_model"`
	EmbeddingDimensions int               `json:"embedding_dimensions,omitempty" toml:"embedding_dimensions" mapstructure:"embedding_dimensions"`
	IsLocal             bool              `json:"is_local,omitempty" toml:"is_local" mapstructure:"is_local"`
	CostPerInputToken   float64           `json:"cost_per_input_token,omitempty" toml:"cost_per_input_token" mapstructure:"cost_per_input_token"`
	CostPerOutputToken  float64           `json:"cost_per_output_token,omitempty" toml:"cost_per_output_token" mapstructure:"cost_per_output_token"`
	AuthHeader          string            `json:"auth_header,omitempty" toml:"auth_header" mapstructure:"auth_header"`
	ExtraHeaders        map[string]string `json:"extra_headers,omitempty" toml:"extra_headers" mapstructure:"extra_headers"`
	TPMLimit            uint64            `json:"tpm_limit,omitempty" toml:"tpm_limit" mapstructure:"tpm_limit"`
	RPMLimit            uint64            `json:"rpm_limit,omitempty" toml:"rpm_limit" mapstructure:"rpm_limit"`
	AuthMode            string            `json:"auth_mode,omitempty" toml:"auth_mode" mapstructure:"auth_mode"`
	OAuthClientID       string            `json:"oauth_client_id,omitempty" toml:"oauth_client_id" mapstructure:"oauth_client_id"`
	OAuthRedirectURI    string            `json:"oauth_redirect_uri,omitempty" toml:"oauth_redirect_uri" mapstructure:"oauth_redirect_uri"`
	APIKeyRef           string            `json:"api_key_ref,omitempty" toml:"api_key_ref" mapstructure:"api_key_ref"`
	TimeoutSecs         int               `json:"timeout_seconds,omitempty" toml:"timeout_seconds" mapstructure:"timeout_seconds"`
}

// SessionConfig holds session scoping and timeout settings.
type SessionConfig struct {
	ScopeMode     string `json:"scope_mode" toml:"scope_mode" mapstructure:"scope_mode"`
	TTLSeconds    int    `json:"ttl_seconds" toml:"ttl_seconds" mapstructure:"ttl_seconds"`
	ResetSchedule string `json:"reset_schedule,omitempty" toml:"reset_schedule" mapstructure:"reset_schedule"`
}

// MemoryConfig holds memory budget settings as percentages (must sum to 100).
// WorkingBudgetPct is an alias for WorkingBudget for roboticus compatibility.
type MemoryConfig struct {
	WorkingBudget            float64 `json:"working_budget" toml:"working_budget" mapstructure:"working_budget"`
	WorkingBudgetPct         float64 `json:"working_budget_pct,omitempty" toml:"working_budget_pct" mapstructure:"working_budget_pct"`
	EpisodicBudget           float64 `json:"episodic_budget" toml:"episodic_budget" mapstructure:"episodic_budget"`
	SemanticBudget           float64 `json:"semantic_budget" toml:"semantic_budget" mapstructure:"semantic_budget"`
	ProceduralBudget         float64 `json:"procedural_budget" toml:"procedural_budget" mapstructure:"procedural_budget"`
	RelationshipBudget       float64 `json:"relationship_budget" toml:"relationship_budget" mapstructure:"relationship_budget"`
	EmbeddingProvider        string  `json:"embedding_provider,omitempty" toml:"embedding_provider" mapstructure:"embedding_provider"`
	EmbeddingModel           string  `json:"embedding_model,omitempty" toml:"embedding_model" mapstructure:"embedding_model"`
	HybridWeightOverride     float64 `json:"hybrid_weight_override" toml:"hybrid_weight_override" mapstructure:"hybrid_weight_override"` // 0 = adaptive (default); >0 = manual override
	DecayHalfLifeDays        float64 `json:"decay_half_life_days" toml:"decay_half_life_days" mapstructure:"decay_half_life_days"`
	SimilarityThreshold      float64 `json:"similarity_threshold" toml:"similarity_threshold" mapstructure:"similarity_threshold"`
	VectorIndexThreshold     int     `json:"vector_index_threshold" toml:"vector_index_threshold" mapstructure:"vector_index_threshold"` // corpus size for HNSW promotion (default 1000)
}

// CacheConfig holds semantic cache settings.
type CacheConfig struct {
	Enabled                 bool    `json:"enabled" toml:"enabled" mapstructure:"enabled"`
	TTLSeconds              int     `json:"ttl_seconds" toml:"ttl_seconds" mapstructure:"ttl_seconds"`
	SimilarityThreshold     float64 `json:"similarity_threshold" toml:"similarity_threshold" mapstructure:"similarity_threshold"`
	MaxEntries              int     `json:"max_entries" toml:"max_entries" mapstructure:"max_entries"`
	PromptCompression       bool    `json:"prompt_compression" toml:"prompt_compression" mapstructure:"prompt_compression"`
	CompressionTargetRatio  float64 `json:"compression_target_ratio" toml:"compression_target_ratio" mapstructure:"compression_target_ratio"`
}

// TreasuryConfig holds financial policy limits.
type TreasuryConfig struct {
	DailyCap              float64           `json:"daily_cap" toml:"daily_cap" mapstructure:"daily_cap"`
	PerPaymentCap         float64           `json:"per_payment_cap" toml:"per_payment_cap" mapstructure:"per_payment_cap"`
	TransferLimit         float64           `json:"transfer_limit" toml:"transfer_limit" mapstructure:"transfer_limit"`
	MinimumReserve        float64           `json:"minimum_reserve" toml:"minimum_reserve" mapstructure:"minimum_reserve"`
	HourlyTransferLimit   float64           `json:"hourly_transfer_limit" toml:"hourly_transfer_limit" mapstructure:"hourly_transfer_limit"`
	DailyTransferLimit    float64           `json:"daily_transfer_limit" toml:"daily_transfer_limit" mapstructure:"daily_transfer_limit"`
	DailyInferenceBudget  float64           `json:"daily_inference_budget" toml:"daily_inference_budget" mapstructure:"daily_inference_budget"`
	RevenueSwap           RevenueSwapConfig `json:"revenue_swap" toml:"revenue_swap" mapstructure:"revenue_swap"`
}

// WalletConfig holds crypto wallet settings.
type WalletConfig struct {
	Path               string `json:"path" toml:"path" mapstructure:"path"`
	ChainID            uint64 `json:"chain_id" toml:"chain_id" mapstructure:"chain_id"`
	RPCURL             string `json:"rpc_url" toml:"rpc_url" mapstructure:"rpc_url"`
	BalancePollSeconds int    `json:"balance_poll_seconds" toml:"balance_poll_seconds" mapstructure:"balance_poll_seconds"` // 0 = disabled, default 60
}

// PluginsConfig holds plugin discovery settings.
type PluginsConfig struct {
	Dir               string   `json:"dir" toml:"dir" mapstructure:"dir"`
	Allow             []string `json:"allow,omitempty" toml:"allow" mapstructure:"allow"`
	Deny              []string `json:"deny,omitempty" toml:"deny" mapstructure:"deny"`
	StrictPermissions bool     `json:"strict_permissions" toml:"strict_permissions" mapstructure:"strict_permissions"`
	CatalogURL        string   `json:"catalog_url" toml:"catalog_url" mapstructure:"catalog_url"`
}

// ChannelsConfig holds channel adapter settings.
// Rust parity: each channel has its own rich sub-struct with full configuration.
// Legacy flat fields are preserved for backwards compatibility.
type ChannelsConfig struct {
	// Rich per-channel configs (Rust parity: runtime_core.rs).
	Telegram *TelegramConfig `json:"telegram,omitempty" toml:"telegram" mapstructure:"telegram"`
	WhatsApp *WhatsAppConfig `json:"whatsapp,omitempty" toml:"whatsapp" mapstructure:"whatsapp"`
	Discord  *DiscordConfig  `json:"discord,omitempty" toml:"discord" mapstructure:"discord"`
	Signal   *SignalConfig   `json:"signal,omitempty" toml:"signal" mapstructure:"signal"`
	Email    *EmailConfig    `json:"email,omitempty" toml:"email" mapstructure:"email"`
	Voice    *VoiceConfig    `json:"voice,omitempty" toml:"voice" mapstructure:"voice"`

	// Legacy flat fields removed — keys come from keystore, channel tokens
	// are referenced via token_ref in per-channel configs.
}

// TelegramConfig holds Telegram bot adapter settings.
type TelegramConfig struct {
	Enabled            bool    `json:"enabled" toml:"enabled" mapstructure:"enabled"`
	// TokenEnv removed — tokens come from keystore via token_ref.
	TokenRef           string  `json:"token_ref,omitempty" toml:"token_ref" mapstructure:"token_ref"`
	AllowedChatIDs     []int64 `json:"allowed_chat_ids,omitempty" toml:"allowed_chat_ids" mapstructure:"allowed_chat_ids"`
	PollTimeoutSeconds int     `json:"poll_timeout_seconds" toml:"poll_timeout_seconds" mapstructure:"poll_timeout_seconds"`
	WebhookMode        bool    `json:"webhook_mode" toml:"webhook_mode" mapstructure:"webhook_mode"`
	WebhookPath        string  `json:"webhook_path,omitempty" toml:"webhook_path" mapstructure:"webhook_path"`
	WebhookSecret      string  `json:"webhook_secret,omitempty" toml:"webhook_secret" mapstructure:"webhook_secret"`
}

// WhatsAppConfig holds WhatsApp Cloud API adapter settings.
type WhatsAppConfig struct {
	Enabled        bool     `json:"enabled" toml:"enabled" mapstructure:"enabled"`
	// TokenEnv removed — tokens come from keystore via token_ref.
	TokenRef       string   `json:"token_ref,omitempty" toml:"token_ref" mapstructure:"token_ref"`
	PhoneNumberID  string   `json:"phone_number_id" toml:"phone_number_id" mapstructure:"phone_number_id"`
	VerifyToken    string   `json:"verify_token" toml:"verify_token" mapstructure:"verify_token"`
	AllowedNumbers []string `json:"allowed_numbers,omitempty" toml:"allowed_numbers" mapstructure:"allowed_numbers"`
	AppSecret      string   `json:"app_secret,omitempty" toml:"app_secret" mapstructure:"app_secret"`
}

// DiscordConfig holds Discord bot adapter settings.
type DiscordConfig struct {
	Enabled         bool     `json:"enabled" toml:"enabled" mapstructure:"enabled"`
	// TokenEnv removed — tokens come from keystore via token_ref.
	TokenRef        string   `json:"token_ref,omitempty" toml:"token_ref" mapstructure:"token_ref"`
	ApplicationID   string   `json:"application_id" toml:"application_id" mapstructure:"application_id"`
	AllowedGuildIDs []string `json:"allowed_guild_ids,omitempty" toml:"allowed_guild_ids" mapstructure:"allowed_guild_ids"`
}

// SignalConfig holds Signal messenger adapter settings.
type SignalConfig struct {
	Enabled        bool     `json:"enabled" toml:"enabled" mapstructure:"enabled"`
	PhoneNumber    string   `json:"phone_number" toml:"phone_number" mapstructure:"phone_number"`
	DaemonURL      string   `json:"daemon_url" toml:"daemon_url" mapstructure:"daemon_url"`
	AllowedNumbers []string `json:"allowed_numbers,omitempty" toml:"allowed_numbers" mapstructure:"allowed_numbers"`
}

// EmailConfig holds email (IMAP/SMTP) adapter settings.
type EmailConfig struct {
	Enabled            bool     `json:"enabled" toml:"enabled" mapstructure:"enabled"`
	IMAPHost           string   `json:"imap_host" toml:"imap_host" mapstructure:"imap_host"`
	IMAPPort           int      `json:"imap_port" toml:"imap_port" mapstructure:"imap_port"`
	SMTPHost           string   `json:"smtp_host" toml:"smtp_host" mapstructure:"smtp_host"`
	SMTPPort           int      `json:"smtp_port" toml:"smtp_port" mapstructure:"smtp_port"`
	Username           string   `json:"username" toml:"username" mapstructure:"username"`
	// PasswordEnv removed — passwords come from keystore.
	FromAddress        string   `json:"from_address" toml:"from_address" mapstructure:"from_address"`
	AllowedSenders     []string `json:"allowed_senders,omitempty" toml:"allowed_senders" mapstructure:"allowed_senders"`
	PollIntervalSecs   int      `json:"poll_interval_seconds" toml:"poll_interval_seconds" mapstructure:"poll_interval_seconds"`
	OAuth2TokenEnv     string   `json:"oauth2_token_env,omitempty" toml:"oauth2_token_env" mapstructure:"oauth2_token_env"`
	UseOAuth2          bool     `json:"use_oauth2" toml:"use_oauth2" mapstructure:"use_oauth2"`
	IMAPIdleEnabled    bool     `json:"imap_idle_enabled" toml:"imap_idle_enabled" mapstructure:"imap_idle_enabled"`
}

// VoiceConfig holds voice channel adapter settings.
type VoiceConfig struct {
	Enabled  bool   `json:"enabled" toml:"enabled" mapstructure:"enabled"`
	STTModel string `json:"stt_model,omitempty" toml:"stt_model" mapstructure:"stt_model"`
	TTSModel string `json:"tts_model,omitempty" toml:"tts_model" mapstructure:"tts_model"`
	TTSVoice string `json:"tts_voice,omitempty" toml:"tts_voice" mapstructure:"tts_voice"`
}

// SecurityConfig holds filesystem and sandbox settings.
// NOTE: workspace_only and deny_on_empty_allowlist live ONLY in Filesystem sub-config.
// Top-level fields removed to eliminate contradictory dual-config.
type SecurityConfig struct {
	AllowedPaths         []string `json:"allowed_paths" toml:"allowed_paths" mapstructure:"allowed_paths"`
	ProtectedPaths       []string `json:"protected_paths" toml:"protected_paths" mapstructure:"protected_paths"`
	ExtraProtectedPaths  []string `json:"extra_protected_paths,omitempty" toml:"extra_protected_paths" mapstructure:"extra_protected_paths"`
	InterpreterAllow     []string `json:"interpreter_allow" toml:"interpreter_allow" mapstructure:"interpreter_allow"`
	ScriptAllowedPaths   []string `json:"script_allowed_paths" toml:"script_allowed_paths" mapstructure:"script_allowed_paths"`
	ThreatCautionCeiling string   `json:"threat_caution_ceiling,omitempty" toml:"threat_caution_ceiling" mapstructure:"threat_caution_ceiling"`
	AllowlistAuthority   string   `json:"allowlist_authority,omitempty" toml:"allowlist_authority" mapstructure:"allowlist_authority"`
	TrustedAuthority     string   `json:"trusted_authority,omitempty" toml:"trusted_authority" mapstructure:"trusted_authority"`
	APIAuthority         string   `json:"api_authority,omitempty" toml:"api_authority" mapstructure:"api_authority"`
	SandboxRequired      bool     `json:"sandbox_required" toml:"sandbox_required" mapstructure:"sandbox_required"`
	ScriptFsConfinement  bool     `json:"script_fs_confinement" toml:"script_fs_confinement" mapstructure:"script_fs_confinement"` // Confine scripts to workspace directory
	// TrustedSenderIDs lists sender/chat IDs that receive Creator authority via
	// the SecurityClaim resolver's TrustedAuthority grant. Matches Rust's
	// channels.trusted_sender_ids configuration.
	TrustedSenderIDs []string `json:"trusted_sender_ids,omitempty" toml:"trusted_sender_ids" mapstructure:"trusted_sender_ids"`
	// Filesystem is a nested security section for fine-grained filesystem access control.
	// Mirrors Rust's security.filesystem configuration.
	Filesystem FilesystemSecurityConfig `json:"filesystem" toml:"filesystem" mapstructure:"filesystem"`
}

// IsWorkspaceConfined returns true if filesystem workspace_only is set.
func (s SecurityConfig) IsWorkspaceConfined() bool {
	return s.Filesystem.WorkspaceOnly
}

// RevenueConfig holds revenue settlement settings.
type RevenueConfig struct {
	Enabled           bool    `json:"enabled" toml:"enabled" mapstructure:"enabled"`
	TaxRate           float64 `json:"tax_rate" toml:"tax_rate" mapstructure:"tax_rate"`
	DestinationWallet string  `json:"destination_wallet" toml:"destination_wallet" mapstructure:"destination_wallet"`
}

// CircuitBreakerConfig holds circuit breaker settings.
type CircuitBreakerConfig struct {
	Threshold          int `json:"threshold" toml:"threshold" mapstructure:"threshold"`
	WindowSeconds      int `json:"window_seconds" toml:"window_seconds" mapstructure:"window_seconds"`
	CooldownSeconds    int `json:"cooldown_seconds" toml:"cooldown_seconds" mapstructure:"cooldown_seconds"`
	MaxCooldownSeconds int `json:"max_cooldown_seconds" toml:"max_cooldown_seconds" mapstructure:"max_cooldown_seconds"`
}

// SelfFundingTaxConfig holds self-funding tax settings.
type SelfFundingTaxConfig struct {
	Enabled           bool    `json:"enabled" toml:"enabled" mapstructure:"enabled"`
	Rate              float64 `json:"rate" toml:"rate" mapstructure:"rate"`
	DestinationWallet string  `json:"destination_wallet" toml:"destination_wallet" mapstructure:"destination_wallet"`
}

// SelfFundingConfig holds self-funding settings.
type SelfFundingConfig struct {
	Tax SelfFundingTaxConfig `json:"tax" toml:"tax" mapstructure:"tax"`
}

// YieldConfig holds DeFi yield settings.
type YieldConfig struct {
	Enabled             bool    `json:"enabled" toml:"enabled" mapstructure:"enabled"`
	Protocol            string  `json:"protocol" toml:"protocol" mapstructure:"protocol"`
	Chain               string  `json:"chain" toml:"chain" mapstructure:"chain"`
	MinDeposit          float64 `json:"min_deposit" toml:"min_deposit" mapstructure:"min_deposit"`
	WithdrawalThreshold float64 `json:"withdrawal_threshold" toml:"withdrawal_threshold" mapstructure:"withdrawal_threshold"`
	ChainRPCURL         string  `json:"chain_rpc_url" toml:"chain_rpc_url" mapstructure:"chain_rpc_url"`
	PoolAddress         string  `json:"pool_address" toml:"pool_address" mapstructure:"pool_address"`
	USDCAddress         string  `json:"usdc_address" toml:"usdc_address" mapstructure:"usdc_address"`
	ATokenAddress       string  `json:"atoken_address" toml:"atoken_address" mapstructure:"atoken_address"`
}

// A2AConfig holds agent-to-agent protocol settings.
type A2AConfig struct {
	Enabled                bool `json:"enabled" toml:"enabled" mapstructure:"enabled"`
	MaxMessageSize         int  `json:"max_message_size" toml:"max_message_size" mapstructure:"max_message_size"`
	RateLimitPerPeer       int  `json:"rate_limit_per_peer" toml:"rate_limit_per_peer" mapstructure:"rate_limit_per_peer"`
	SessionTimeoutSeconds  int  `json:"session_timeout_seconds" toml:"session_timeout_seconds" mapstructure:"session_timeout_seconds"`
	RequireOnChainIdentity bool `json:"require_on_chain_identity" toml:"require_on_chain_identity" mapstructure:"require_on_chain_identity"`
	NonceTTLSeconds        int  `json:"nonce_ttl_seconds" toml:"nonce_ttl_seconds" mapstructure:"nonce_ttl_seconds"`
}

// ModelOverride holds per-model override settings.
type ModelOverride struct {
	MaxTokens   int     `json:"max_tokens,omitempty" toml:"max_tokens" mapstructure:"max_tokens"`
	Temperature float64 `json:"temperature,omitempty" toml:"temperature" mapstructure:"temperature"`
	TopP        float64 `json:"top_p,omitempty" toml:"top_p" mapstructure:"top_p"`
	Provider    string  `json:"provider,omitempty" toml:"provider" mapstructure:"provider"`
	TimeoutSecs int     `json:"timeout_seconds,omitempty" toml:"timeout_seconds" mapstructure:"timeout_seconds"`
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
