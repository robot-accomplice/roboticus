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

	// Context budget tier (L0-L3). Higher tiers allow more history + memory.
	// Matches Rust's ContextBudgetConfig tiers.
	BudgetTier int // 0=L0 (minimal), 1=L1 (standard), 2=L2 (extended), 3=L3 (maximum)

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
// PresetAPI returns the standard HTTP API preset (full pipeline, creator authority).
//
// Stage rationale for non-default values:
//   SpecialistControls: false  — API clients manage their own specialist UX
//   SkillFirstEnabled:  false  — skill-first routing only for interactive channels
//   NicknameRefinement: true   — web sessions display nicknames
//   InjectDiagnostics:  true   — API clients benefit from diagnostic hints
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
		BudgetTier:             1, // L1: standard
		PostTurnIngest:         true,
		NicknameRefinement:     true,
		InjectDiagnostics:      true,
		ChannelLabel:           "api",
	}
}

// PresetStreaming returns the SSE streaming preset (reduced guards, no nickname).
//
// Stage rationale for non-default values:
//   GuardSet:           GuardSetStream (6 guards) — retry-capable guards excluded from streaming
//   NicknameRefinement: false  — can't update session nickname mid-stream
//   SkillFirstEnabled:  false  — skill-first routing only for interactive channels
//   SpecialistControls: false  — API clients manage their own specialist UX
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
		BudgetTier:             1, // L1: standard
		PostTurnIngest:         true,
		NicknameRefinement:     false,
		InjectDiagnostics:      true,
		ChannelLabel:           "streaming",
	}
}

// PresetChannel returns the channel adapter preset (full pipeline, channel auth).
//
// Stage rationale for non-default values:
//   SpecialistControls: true   — channels have interactive specialist creation UX
//   SkillFirstEnabled:  true   — trigger-based skills on channel interactions
//   NicknameRefinement: false  — channels don't show session nicknames
//   InjectDiagnostics:  false  — diagnostic hints are API-specific
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
		BudgetTier:             1, // L1: channel minimum
		PostTurnIngest:         true,
		NicknameRefinement:     false,
		InjectDiagnostics:      false,
		ChannelLabel:           platform,
	}
}

// PresetCron returns the scheduled task preset (self-generated authority, minimal).
//
// Stage rationale for non-default values:
//   DedupTracking:      false  — scheduler guarantees uniqueness
//   DelegatedExecution: false  — cron tasks are self-contained
//   SpecialistControls: false  — no interactive specialist creation UX
//   ShortcutsEnabled:   false  — cron tasks are machine-generated; ack shortcuts don't apply
//   SkillFirstEnabled:  false  — cron tasks are self-contained
//   NicknameRefinement: false  — cron sessions are ephemeral
//   InjectDiagnostics:  false  — no user to see diagnostics
//   CronDelegationWrap: true   — prepend subagent delegation context
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
		BudgetTier:             0, // L0: minimal (cron tasks are self-contained)
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

// ResolveSecurityClaim resolves a full SecurityClaim using the core resolvers.
// This replaces the former ResolveAuthority which only returned an AuthorityLevel.
// The full claim carries source tracking for audit and ceiling enforcement.
func ResolveSecurityClaim(mode AuthorityMode, claim *ChannelClaimContext) core.SecurityClaim {
	sec := core.DefaultClaimSecurityConfig()

	switch mode {
	case AuthorityAPIKey:
		return core.ResolveAPIClaim(false, "api", sec)
	case AuthoritySelfGen:
		return core.SecurityClaim{
			Authority: core.AuthoritySelfGenerated,
			Sources:   []core.ClaimSource{},
			Ceiling:   core.AuthorityCreator,
			SenderID:  "cron",
			Channel:   "cron",
		}
	case AuthorityChannel:
		if claim == nil {
			return core.SecurityClaim{
				Authority: core.AuthorityExternal,
				Sources:   []core.ClaimSource{core.ClaimSourceAnonymous},
				Ceiling:   core.AuthorityCreator,
				Channel:   "unknown",
			}
		}
		return core.ResolveChannelClaim(&core.ChannelClaimContext{
			SenderID:            claim.SenderID,
			ChatID:              claim.ChatID,
			Channel:             claim.Platform,
			SenderInAllowlist:   claim.SenderInAllowlist,
			AllowlistConfigured: claim.AllowlistConfigured,
			TrustedSenderIDs:    claim.TrustedSenderIDs,
		}, sec)
	}
	return core.SecurityClaim{
		Authority: core.AuthorityExternal,
		Sources:   []core.ClaimSource{core.ClaimSourceAnonymous},
		Ceiling:   core.AuthorityCreator,
	}
}

// ResolveAuthority maps AuthorityMode to an AuthorityLevel.
// Convenience wrapper over ResolveSecurityClaim for callers that only need the level.
func ResolveAuthority(mode AuthorityMode, claim *ChannelClaimContext) core.AuthorityLevel {
	return ResolveSecurityClaim(mode, claim).Authority
}
