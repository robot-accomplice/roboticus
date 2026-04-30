package core

import "path/filepath"

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() Config {
	home := homeDir()
	dataDir := filepath.Join(home, ".roboticus")

	return Config{
		Agent: AgentConfig{
			Name:                        "roboticus",
			ID:                          "roboticus-default",
			Workspace:                   filepath.Join(dataDir, "workspace"),
			AutonomyMaxReactTurns:       10, // Rust parity: 10 turns (was 25)
			AutonomyMaxTurnDurationSecs: 90, // Rust parity: 90s (was 300)
			LogLevel:                    "info",
			DelegationEnabled:           true,
			DelegationMinComplexity:     0.35,
			DelegationMinUtilityMargin:  0.15, // Rust parity
			SpecialistRequiresApproval:  true, // Rust parity
			CompositionPolicy:           "propose",
			SkillCreationRigor:          "validate", // Rust parity: generate|validate|full
			OutputValidationPolicy:      "sample",   // Rust parity: strict|sample|off
			OutputValidationSampleRate:  0.1,        // Rust parity
			MaxOutputRetries:            2,          // Rust parity
			RetirementSuccessThreshold:  0.7,        // Rust parity
			RetirementMinDelegations:    10,         // Rust parity
		},
		Server: ServerConfig{
			Port:                      DefaultServerPort,
			Bind:                      DefaultServerBind,
			LogDir:                    filepath.Join(dataDir, "logs"),
			CronMaxConcurrency:        8,
			LogMaxDays:                7,
			RateLimitRequests:         100,
			RateLimitWindowSecs:       60,
			PerIPRateLimitRequests:    300,
			PerActorRateLimitRequests: 200,
		},
		Database: DatabaseConfig{
			Path: filepath.Join(dataDir, "roboticus.db"),
		},
		Models: ModelsConfig{
			Primary:         "claude-sonnet-4-20250514",
			Policy:          map[string]ModelPolicyConfig{},
			RoleEligibility: map[string]ModelRoleConfig{},
			Routing: RoutingConfig{
				Mode:                   "auto",
				ConfidenceThreshold:    0.9,
				EstimatedOutputTokens:  500,
				AccuracyFloor:          0.0,
				AccuracyMinObs:         10,
				CostAware:              false,
				PerProviderTimeoutSecs: 30,
				MaxTotalInferenceSecs:  120,
				MaxFallbackAttempts:    6,
				LocalFirst:             true,
			},
			TieredInference: TieredInferenceConfig{
				ConfidenceFloor:           0.6,
				EscalationLatencyBudgetMs: 3000,
			},
		},
		Providers:     make(map[string]ProviderConfig),
		ProvidersFile: filepath.Join(dataDir, "providers.toml"),
		Memory: MemoryConfig{
			WorkingBudget:            30,
			EpisodicBudget:           25,
			SemanticBudget:           20,
			ProceduralBudget:         15,
			RelationshipBudget:       10,
			HybridWeightOverride:     0, // 0 = adaptive (corpus-size based)
			DecayHalfLifeDays:        7.0,
			VectorIndexThreshold:     1000,
			RerankerMinScore:         0.1,
			RerankerAuthorityBoost:   1.5,
			RerankerRecencyPenalty:   0.8,
			RerankerCollapseSpread:   0.05,
			LLMRerankerEnabled:       false,
			LLMRerankerMinCandidates: 5,
			LLMRerankerMaxCandidates: 8,
			LLMRerankerKeepTop:       5,
		},
		Cache: CacheConfig{
			Enabled:                true,
			TTLSeconds:             3600,
			SimilarityThreshold:    0.95,
			MaxEntries:             10000,
			CompressionTargetRatio: 0.5,
		},
		Treasury: TreasuryConfig{
			PerPaymentCap:        100.0,  // Rust parity: default_per_payment_cap() = 100.0
			HourlyTransferLimit:  500.0,  // Rust parity: default_hourly_limit() = 500.0
			DailyTransferLimit:   2000.0, // Rust parity: default_daily_limit() = 2000.0
			MinimumReserve:       5.0,    // Rust parity: default_min_reserve() = 5.0
			DailyInferenceBudget: 50.0,   // Rust parity: default_inference_budget() = 50.0
			DailyCap:             2000.0, // Go-unique alias — mirrors DailyTransferLimit
			TransferLimit:        500.0,  // Go-unique alias — mirrors HourlyTransferLimit
			RevenueSwap: RevenueSwapConfig{
				TargetSymbol: "PUSD",
				DefaultChain: "ETH",
			},
		},
		Session: SessionConfig{
			ScopeMode:  "peer",
			TTLSeconds: 86400,
		},
		Wallet: WalletConfig{
			Path:               filepath.Join(dataDir, "wallet.enc"),
			ChainID:            8453,
			RPCURL:             "https://mainnet.base.org",
			BalancePollSeconds: 60,
		},
		Plugins: PluginsConfig{
			Dir:               filepath.Join(dataDir, "plugins"),
			StrictPermissions: true,
			CatalogURL:        "https://roboticus.ai/registry/plugins.json",
		},
		Security: SecurityConfig{
			AllowlistAuthority: "Peer",
			TrustedAuthority:   "Creator",
			APIAuthority:       "Creator",
			Filesystem: FilesystemSecurityConfig{
				WorkspaceOnly:        true,
				DenyOnEmptyAllowlist: true,
			},
		},
		Skills: SkillsConfig{
			WatchMode:            true,
			ScriptTimeoutSeconds: 30,
			ScriptMaxOutputBytes: 1 << 20, // 1 MiB
			AllowedInterpreters:  []string{"sh", "bash", "python3", "node", "ruby", "perl", "pwsh", "gosh"},
			SandboxEnv:           true,
			HotReload:            true,
			ScriptMaxMemoryBytes: 256 << 20, // 256 MiB
			NetworkAllowed:       false,
		},
		CORS: CORSConfig{
			AllowedOrigins: []string{"*"},
			MaxAgeSeconds:  3600,
		},
		Themes: ThemesConfig{
			CatalogURL: "https://roboticus.ai/registry/themes.json",
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
			Headless:       true,
		},
		Daemon: DaemonConfig{
			AutoRestart: false,
		},
		Update: UpdateConfig{
			Enabled:            true,
			CheckIntervalHours: 24,
		},
		TierAdapt: TierAdaptConfig{
			Enabled:           false,
			T2DefaultPreamble: "Be concise and direct. Focus on accuracy.",
			T3T4Passthrough:   true,
		},
		Digest: DigestConfig{
			Enabled:           true,
			MinTurns:          3,
			MaxTokens:         512,
			DecayHalfLifeDays: 7.0,
		},
		Learning: LearningConfig{
			Enabled:                    true,
			MinSequenceLength:          3,
			MinSuccessRatio:            0.7,
			PriorityBoostOnSuccess:     5,
			PriorityDecayOnFailure:     10, // intentional 2:1 asymmetry with boost
			MaxLearnedSkills:           100,
			StaleProceduralDays:        30,
			DeadSkillPriorityThreshold: 0,
		},
		Multimodal: MultimodalConfig{
			VisionEnabled:        false,
			AudioEnabled:         false,
			MaxImageSizeBytes:    10 << 20, // 10 MiB
			MaxAudioSizeBytes:    25 << 20, // 25 MiB
			MaxVideoSizeBytes:    50 << 20, // 50 MiB
			MaxDocumentSizeBytes: 50 << 20, // 50 MiB
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
		Heartbeat: HeartbeatConfig{
			IntervalSeconds:            60,
			TreasuryIntervalSeconds:    300,
			YieldIntervalSeconds:       600,
			MemoryIntervalSeconds:      60,
			MaintenanceIntervalSeconds: 60,
			SessionIntervalSeconds:     60,
			DiscoveryIntervalSeconds:   300,
		},
		ContextBudget: ContextBudgetConfig{
			L0:                8000,
			L1:                8000,
			L2:                16000,
			L3:                32000,
			ChannelMinimum:    "L1",
			SoulMaxContextPct: 0.4,
		},
		// Tool search defaults mirror Rust's hard-coded SearchConfig::default
		// (top_k=15, token_budget=4000, mcp_penalty=0.05). AlwaysInclude is
		// Go-native: it names tools that exist in Go's registry and are the
		// functional analogue of Rust's always_include_operational_tools
		// (crates/roboticus-pipeline/src/core/tool_prune.rs). See System 02
		// Intentional Deviations for the mapping rationale.
		ToolSearch: ToolSearchConfig{
			TopK:              15,
			TokenBudget:       4000,
			MCPLatencyPenalty: 0.05,
			AlwaysInclude: []string{
				"recall_memory",
				"search_memories",
				"get_memory_stats",
				"get_runtime_context",
				"get_subagent_status",
				"introspect",
				"obsidian_write",
				"list-subagent-roster",
				"list-available-skills",
				"compose-skill",
				"compose-subagent",
				"orchestrate-subagents",
				"task-status",
				"list-open-tasks",
			},
		},
	}
}
