package pipeline

import "roboticus/internal/core"

// InferenceMode controls whether the pipeline uses the ReAct loop or SSE streaming.
type InferenceMode int

const (
	InferenceStandard  InferenceMode = iota // Full ReAct loop with tool use
	InferenceStreaming                      // SSE streaming, no ReAct
)

// SessionResolutionMode controls how sessions are resolved.
type SessionResolutionMode int

const (
	SessionFromBody    SessionResolutionMode = iota // Client provides session_id in request
	SessionFromChannel                              // Derived from (platform, chat_id)
	SessionDedicated                                // One-off session (cron jobs)
)

// GuardSetPreset selects which guard chain to apply.
type GuardSetPreset int

const (
	GuardSetFull   GuardSetPreset = iota // All guards (10)
	GuardSetCached                       // Guards for cache hits
	GuardSetStream                       // Reduced set for SSE (6)
	GuardSetNone                         // No guards
)

// AuthorityMode determines how RBAC authority is resolved.
type AuthorityMode int

const (
	AuthorityAPIKey  AuthorityMode = iota // Resolved from API key claims
	AuthorityChannel                      // Resolved from channel sender context
	AuthoritySelfGen                      // Self-generated (cron, internal)
)

// Config declares which pipeline stages are active. Every boolean flag
// corresponds to a stage that is either fully on or fully off — no branching
// within stages. This is the core of the connector-factory pattern: connectors
// select a preset, the pipeline executes uniformly.
type Config struct {
	// Input defense.
	InjectionDefense bool // L1/L2 injection detection and sanitization
	DedupTracking    bool // Track in-flight duplicates, reject concurrent identical requests

	// Session.
	SessionResolution SessionResolutionMode

	// Pre-inference.
	DecompositionGate      bool // Evaluate decomposition for multi-agent delegation
	DelegatedExecution     bool // Execute orchestrate-subagents tool before LLM
	SpecialistControls     bool // Handle specialist creation control flows
	ShortcutsEnabled       bool // Try execution shortcuts before LLM
	SkillFirstEnabled      bool // Match input against skill triggers
	ShortFollowupExpansion bool // Expand short reactions by prepending prior context

	// Inference.
	InferenceMode InferenceMode
	GuardSet      GuardSetPreset // Guard set for fresh inference
	CacheGuardSet GuardSetPreset // Guard set for cached responses
	CacheEnabled  bool

	// Authority.
	AuthorityMode AuthorityMode

	// Post-inference.
	PostTurnIngest     bool // Background memory ingestion after turn
	NicknameRefinement bool // Background LLM-driven session naming

	// Model routing overrides.
	ModelOverride    string // Force a specific model, bypassing router
	PreferLocalModel bool   // Prefer local models over cloud when quality is comparable

	// Task/planner controls.
	TaskOperatingState      string // Operator-injected task context (e.g., "maintenance")
	BackgroundBudget        int    // Token budget for background/low-priority turns
	CronDelegationWrap      bool   // Wrap cron-triggered turns in delegation context
	BotCommandDispatch      bool   // Dispatch bot commands to tool executor directly
	SessionNicknameOverride string // Override auto-generated session nickname

	// Output.
	InjectDiagnostics bool   // Inject diagnostics metadata into system prompt
	ChannelLabel      string // Human-readable label for logging/cost tracking
}

// PresetAPI returns the standard API preset (full pipeline, standard inference).
func PresetAPI() Config {
	return Config{
		InjectionDefense:       true,
		DedupTracking:          true,
		SessionResolution:      SessionFromBody,
		DecompositionGate:      true,
		DelegatedExecution:     true,
		SpecialistControls:     false,
		ShortcutsEnabled:       true,
		SkillFirstEnabled:      false,
		ShortFollowupExpansion: true,
		InferenceMode:          InferenceStandard,
		GuardSet:               GuardSetFull,
		CacheGuardSet:          GuardSetCached,
		CacheEnabled:           true,
		AuthorityMode:          AuthorityAPIKey,
		PostTurnIngest:         true,
		NicknameRefinement:     true,
		InjectDiagnostics:      true,
		ChannelLabel:           "api",
	}
}

// PresetStreaming returns the streaming API preset (SSE, reduced guards).
func PresetStreaming() Config {
	return Config{
		InjectionDefense:       true,
		DedupTracking:          true,
		SessionResolution:      SessionFromBody,
		DecompositionGate:      true,
		DelegatedExecution:     true,
		SpecialistControls:     false,
		ShortcutsEnabled:       true,
		SkillFirstEnabled:      false,
		ShortFollowupExpansion: true,
		InferenceMode:          InferenceStreaming,
		GuardSet:               GuardSetStream,
		CacheGuardSet:          GuardSetNone,
		CacheEnabled:           true,
		AuthorityMode:          AuthorityAPIKey,
		PostTurnIngest:         true,
		NicknameRefinement:     false,
		InjectDiagnostics:      true,
		ChannelLabel:           "streaming",
	}
}

// PresetChannel returns the channel adapter preset (full pipeline, channel auth).
func PresetChannel(platform string) Config {
	return Config{
		InjectionDefense:       true,
		DedupTracking:          true,
		SessionResolution:      SessionFromChannel,
		DecompositionGate:      true,
		DelegatedExecution:     true,
		SpecialistControls:     true,
		ShortcutsEnabled:       true,
		SkillFirstEnabled:      true,
		ShortFollowupExpansion: true,
		InferenceMode:          InferenceStandard,
		GuardSet:               GuardSetFull,
		CacheGuardSet:          GuardSetCached,
		CacheEnabled:           true,
		AuthorityMode:          AuthorityChannel,
		PostTurnIngest:         true,
		NicknameRefinement:     false,
		InjectDiagnostics:      false,
		ChannelLabel:           platform,
	}
}

// PresetCron returns the cron/scheduled task preset (self-generated authority, no dedup).
func PresetCron() Config {
	return Config{
		InjectionDefense:       true,
		DedupTracking:          false,
		SessionResolution:      SessionDedicated,
		DecompositionGate:      true,
		DelegatedExecution:     false,
		SpecialistControls:     false,
		ShortcutsEnabled:       true,
		SkillFirstEnabled:      false,
		ShortFollowupExpansion: false,
		InferenceMode:          InferenceStandard,
		GuardSet:               GuardSetFull,
		CacheGuardSet:          GuardSetCached,
		CacheEnabled:           true,
		AuthorityMode:          AuthoritySelfGen,
		PostTurnIngest:         true,
		NicknameRefinement:     false,
		InjectDiagnostics:      false,
		ChannelLabel:           "cron",
		CronDelegationWrap:     true,
	}
}

// ChannelClaimContext carries channel-specific authority data for ChannelClaim resolution.
type ChannelClaimContext struct {
	SenderID            string
	ChatID              string
	Platform            string
	SenderInAllowlist   bool
	AllowlistConfigured bool
	TrustedSenderIDs    []string
}

// ResolveAuthority maps AuthorityMode to an AuthorityLevel.
func ResolveAuthority(mode AuthorityMode, claim *ChannelClaimContext) core.AuthorityLevel {
	switch mode {
	case AuthorityAPIKey:
		return core.AuthorityCreator // API keys are fully trusted
	case AuthoritySelfGen:
		return core.AuthoritySelfGenerated
	case AuthorityChannel:
		if claim == nil {
			return core.AuthorityExternal
		}
		if claim.SenderInAllowlist {
			return core.AuthorityCreator
		}
		for _, trusted := range claim.TrustedSenderIDs {
			if trusted == claim.SenderID {
				return core.AuthorityPeer
			}
		}
		return core.AuthorityExternal
	}
	return core.AuthorityExternal
}
