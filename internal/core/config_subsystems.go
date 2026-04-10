package core

// MultimodalConfig holds multimodal input settings.
type MultimodalConfig struct {
	VisionEnabled        bool   `json:"vision_enabled" mapstructure:"vision_enabled"`
	AudioEnabled         bool   `json:"audio_enabled" mapstructure:"audio_enabled"`
	MaxImageSizeBytes    int64  `json:"max_image_size_bytes" mapstructure:"max_image_size_bytes"`
	MaxAudioSizeBytes    int64  `json:"max_audio_size_bytes" mapstructure:"max_audio_size_bytes"`
	MaxVideoSizeBytes    int64  `json:"max_video_size_bytes" mapstructure:"max_video_size_bytes"`
	MaxDocumentSizeBytes int64  `json:"max_document_size_bytes" mapstructure:"max_document_size_bytes"`
	VisionModel          string `json:"vision_model" mapstructure:"vision_model"`
	TranscriptionModel   string `json:"transcription_model" mapstructure:"transcription_model"`
	AutoTranscribeAudio  bool   `json:"auto_transcribe_audio" mapstructure:"auto_transcribe_audio"`
	AutoDescribeImages   bool   `json:"auto_describe_images" mapstructure:"auto_describe_images"`
}

// SkillsConfig holds skill discovery and sandbox settings.
type SkillsConfig struct {
	Directory            string   `json:"directory" mapstructure:"directory"`
	WatchMode            bool     `json:"watch_mode" mapstructure:"watch_mode"`
	ScriptTimeoutSeconds int      `json:"script_timeout_seconds" mapstructure:"script_timeout_seconds"`
	ScriptMaxOutputBytes int      `json:"script_max_output_bytes" mapstructure:"script_max_output_bytes"`
	AllowedInterpreters  []string `json:"allowed_interpreters" mapstructure:"allowed_interpreters"`
	SandboxEnv           bool     `json:"sandbox_env" mapstructure:"sandbox_env"`
	HotReload            bool     `json:"hot_reload" mapstructure:"hot_reload"`
	ScriptMaxMemoryBytes int64    `json:"script_max_memory_bytes" mapstructure:"script_max_memory_bytes"`
	NetworkAllowed       bool     `json:"network_allowed" mapstructure:"network_allowed"`
	WorkspaceDir         string   `json:"workspace_dir,omitempty" mapstructure:"workspace_dir"`
}

// LearningConfig holds pattern learning settings.
type LearningConfig struct {
	Enabled                    bool    `json:"enabled" mapstructure:"enabled"`
	MinSequenceLength          int     `json:"min_sequence_length" mapstructure:"min_sequence_length"`
	MinSuccessRatio            float64 `json:"min_success_ratio" mapstructure:"min_success_ratio"`
	PriorityBoostOnSuccess     int     `json:"priority_boost_on_success" mapstructure:"priority_boost_on_success"`
	PriorityDecayOnFailure     int     `json:"priority_decay_on_failure" mapstructure:"priority_decay_on_failure"`
	MaxLearnedSkills           int     `json:"max_learned_skills" mapstructure:"max_learned_skills"`
	StaleProceduralDays        int     `json:"stale_procedural_days" mapstructure:"stale_procedural_days"`
	DeadSkillPriorityThreshold int     `json:"dead_skill_priority_threshold" mapstructure:"dead_skill_priority_threshold"`
}

// DigestConfig holds conversation digest settings.
type DigestConfig struct {
	Enabled           bool    `json:"enabled" mapstructure:"enabled"`
	MinTurns          int     `json:"min_turns" mapstructure:"min_turns"`
	MaxTokens         int     `json:"max_tokens" mapstructure:"max_tokens"`
	DecayHalfLifeDays float64 `json:"decay_half_life_days" mapstructure:"decay_half_life_days"`
}

// HeartbeatConfig holds heartbeat timing settings.
// Per-domain intervals allow different heartbeat cadences for each subsystem,
// matching the Rust reference's distributed loop architecture.
type HeartbeatConfig struct {
	IntervalSeconds            int `json:"interval_seconds" mapstructure:"interval_seconds"`
	TreasuryIntervalSeconds    int `json:"treasury_interval_seconds" mapstructure:"treasury_interval_seconds"`
	YieldIntervalSeconds       int `json:"yield_interval_seconds" mapstructure:"yield_interval_seconds"`
	MemoryIntervalSeconds      int `json:"memory_interval_seconds" mapstructure:"memory_interval_seconds"`
	MaintenanceIntervalSeconds int `json:"maintenance_interval_seconds" mapstructure:"maintenance_interval_seconds"`
	SessionIntervalSeconds     int `json:"session_interval_seconds" mapstructure:"session_interval_seconds"`
	DiscoveryIntervalSeconds   int `json:"discovery_interval_seconds" mapstructure:"discovery_interval_seconds"`
}

// KnowledgeConfig holds knowledge base settings.
type KnowledgeConfig struct {
	SourcesDir string                 `json:"sources_dir" mapstructure:"sources_dir"`
	Enabled    bool                   `json:"enabled" mapstructure:"enabled"`
	Sources    []KnowledgeSourceEntry `json:"sources" mapstructure:"sources"`
}

// KnowledgeSourceEntry describes a single knowledge source.
type KnowledgeSourceEntry struct {
	Name       string `json:"name" mapstructure:"name"`
	SourceType string `json:"source_type" mapstructure:"source_type"` // "file", "url", "directory"
	Path       string `json:"path,omitempty" mapstructure:"path"`
	URL        string `json:"url,omitempty" mapstructure:"url"`
	MaxChunks  int    `json:"max_chunks" mapstructure:"max_chunks"`
}

// WorkspaceCfg holds workspace indexing settings (distinct from SandboxCfg).
type WorkspaceCfg struct {
	IndexingEnabled bool `json:"indexing_enabled" mapstructure:"indexing_enabled"`
	SoulVersioning  bool `json:"soul_versioning" mapstructure:"soul_versioning"`
	IndexOnStart    bool `json:"index_on_start" mapstructure:"index_on_start"`
	WatchForChanges bool `json:"watch_for_changes" mapstructure:"watch_for_changes"`
}

// ObsidianConfig holds Obsidian vault integration settings.
type ObsidianConfig struct {
	VaultPath               string   `json:"vault_path" mapstructure:"vault_path"`
	Enabled                 bool     `json:"enabled" mapstructure:"enabled"`
	AutoDetectPaths         []string `json:"auto_detect_paths" mapstructure:"auto_detect_paths"`
	SyncOnStart             bool     `json:"sync_on_start" mapstructure:"sync_on_start"`
	AutoSyncIntervalSeconds int      `json:"auto_sync_interval_seconds" mapstructure:"auto_sync_interval_seconds"`
}

// BrowserConfig holds headless browser / CDP settings.
type BrowserConfig struct {
	CDPPort        int    `json:"cdp_port" mapstructure:"cdp_port"`
	TimeoutSeconds int    `json:"timeout_seconds" mapstructure:"timeout_seconds"`
	ProfileDir     string `json:"profile_dir,omitempty" mapstructure:"profile_dir"`
	ExecutablePath string `json:"executable_path,omitempty" mapstructure:"executable_path"`
	Headless       bool   `json:"headless" mapstructure:"headless"`
}

// PersonalityConfig holds personality file paths.
type PersonalityConfig struct {
	OSPath       string `json:"os_path" mapstructure:"os_path"`
	FirmwarePath string `json:"firmware_path" mapstructure:"firmware_path"`
	OperatorPath string `json:"operator_path" mapstructure:"operator_path"`
}

// TierAdaptConfig holds adaptive tier settings for model tiering.
type TierAdaptConfig struct {
	Enabled          bool   `json:"enabled" mapstructure:"enabled"`
	T1StripSystem    bool   `json:"t1_strip_system" mapstructure:"t1_strip_system"`
	T1CondenseTurns  bool   `json:"t1_condense_turns" mapstructure:"t1_condense_turns"`
	T2DefaultPreamble string `json:"t2_default_preamble" mapstructure:"t2_default_preamble"`
	T3T4Passthrough  bool   `json:"t3_t4_passthrough" mapstructure:"t3_t4_passthrough"`
}

// TieredInferenceConfig holds confidence-based model escalation settings.
type TieredInferenceConfig struct {
	Enabled                    bool    `json:"enabled" mapstructure:"enabled"`
	ConfidenceFloor            float64 `json:"confidence_floor" mapstructure:"confidence_floor"`
	EscalationLatencyBudgetMs  int64   `json:"escalation_latency_budget_ms" mapstructure:"escalation_latency_budget_ms"`
}

// RevenueSwapConfig holds revenue swap execution settings.
type RevenueSwapConfig struct {
	TargetSymbol string                  `json:"target_symbol" mapstructure:"target_symbol"`
	DefaultChain string                  `json:"default_chain" mapstructure:"default_chain"`
	Chains       []RevenueSwapChainConfig `json:"chains" mapstructure:"chains"`
}

// RevenueSwapChainConfig holds per-chain swap settings.
type RevenueSwapChainConfig struct {
	Chain                  string `json:"chain" mapstructure:"chain"`
	TargetContractAddress  string `json:"target_contract_address" mapstructure:"target_contract_address"`
	SwapContractAddress    string `json:"swap_contract_address,omitempty" mapstructure:"swap_contract_address"`
}

// UpdateConfig holds auto-update settings.
type UpdateConfig struct {
	Enabled            bool `json:"enabled" mapstructure:"enabled"`
	CheckIntervalHours int  `json:"check_interval_hours" mapstructure:"check_interval_hours"`
}

// DaemonConfig holds background daemon settings.
type DaemonConfig struct {
	AutoRestart bool   `json:"auto_restart" mapstructure:"auto_restart"`
	PIDFile     string `json:"pid_file" mapstructure:"pid_file"`
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

// DeviceConfig holds device pairing settings.
type DeviceConfig struct {
	PairingEnabled bool `json:"pairing_enabled" mapstructure:"pairing_enabled"`
}

// DiscoveryConfig holds network discovery settings.
type DiscoveryConfig struct {
	MDNSEnabled bool `json:"mdns_enabled" mapstructure:"mdns_enabled"`
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

// BudgetForTier returns the token budget for a given tier (0=L0, 1=L1, 2=L2, 3=L3).
func (c ContextBudgetConfig) BudgetForTier(tier int) int {
	switch tier {
	case 0:
		return c.L0
	case 1:
		return c.L1
	case 2:
		return c.L2
	case 3:
		return c.L3
	default:
		return c.L1
	}
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

// CORSConfig holds cross-origin request settings.
type CORSConfig struct {
	AllowedOrigins []string `json:"allowed_origins" mapstructure:"allowed_origins"`
	MaxAgeSeconds  int      `json:"max_age_seconds" mapstructure:"max_age_seconds"`
}

// MatrixChannelConfig holds Matrix homeserver connection settings.
// Rust parity: runtime_core.rs MatrixConfig.
type MatrixChannelConfig struct {
	Enabled            bool     `json:"enabled" mapstructure:"enabled"`
	HomeserverURL      string   `json:"homeserver_url" mapstructure:"homeserver_url"`
	AccessTokenEnv     string   `json:"access_token_env" mapstructure:"access_token_env"`
	AccessToken        string   `json:"access_token" mapstructure:"access_token"`
	DeviceID           string   `json:"device_id" mapstructure:"device_id"`
	AllowedRooms       []string `json:"allowed_rooms" mapstructure:"allowed_rooms"`
	AutoJoin           bool     `json:"auto_join" mapstructure:"auto_join"`
	SyncTimeoutSeconds int      `json:"sync_timeout_seconds" mapstructure:"sync_timeout_seconds"`
	E2EEEnabled        bool     `json:"e2ee_enabled" mapstructure:"e2ee_enabled"`
	DeviceStorePath    string   `json:"device_store_path,omitempty" mapstructure:"device_store_path"`
	DeviceDisplayName  string   `json:"device_display_name" mapstructure:"device_display_name"`
}

// FilesystemSecurityConfig holds fine-grained filesystem access control settings.
// Nested under SecurityConfig as the "filesystem" sub-section.
type FilesystemSecurityConfig struct {
	ToolAllowedPaths     []string `json:"tool_allowed_paths" mapstructure:"tool_allowed_paths"`
	ScriptAllowedPaths   []string `json:"script_allowed_paths" mapstructure:"script_allowed_paths"`
	WorkspaceOnly        bool     `json:"workspace_only" mapstructure:"workspace_only"`
	DenyOnEmptyAllowlist bool     `json:"deny_on_empty_allowlist" mapstructure:"deny_on_empty_allowlist"`
}
