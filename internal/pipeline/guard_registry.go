package pipeline

// GuardRegistry manages named guards and materializes guard chains from presets.
type GuardRegistry struct {
	guards map[string]Guard
}

// NewGuardRegistry creates an empty guard registry.
func NewGuardRegistry() *GuardRegistry {
	return &GuardRegistry{guards: make(map[string]Guard)}
}

// NewDefaultGuardRegistry creates a registry with all built-in guards registered.
func NewDefaultGuardRegistry() *GuardRegistry {
	r := NewGuardRegistry()

	// Rust-aligned guards (order matches Rust reference chain).
	r.Register(&EmptyResponseGuard{})           // 1
	r.Register(&SubagentClaimGuard{})            // 2
	r.Register(&ExecutionTruthGuard{})           // 3
	r.Register(&ActionVerificationGuard{})       // 4
	r.Register(&TaskDeferralGuard{})             // 5
	r.Register(&OutputContractGuard{})           // 6
	r.Register(&ModelIdentityTruthGuard{})       // 7
	r.Register(&CurrentEventsTruthGuard{})       // 8
	r.Register(&LiteraryQuoteRetryGuard{})       // 9
	r.Register(&PersonalityIntegrityGuard{})     // 10
	r.Register(&InternalJargonGuard{})           // 11
	r.Register(&NonRepetitionGuardV2{})          // 12
	r.Register(&LowValueParrotingGuard{})        // 13
	r.Register(&PerspectiveGuard{})              // 14
	r.Register(&DeclaredActionGuard{})           // 15
	r.Register(&UserEchoGuard{})                 // 16
	r.Register(&InternalProtocolGuard{})         // 17

	// Go-only guards (additive, appended after Rust-aligned set).
	r.Register(NewContentClassificationGuard())
	r.Register(NewRepetitionGuard())
	r.Register(NewSystemPromptLeakGuard())
	r.Register(NewInternalMarkerGuard())
	r.Register(&ExecutionBlockGuard{})
	r.Register(&DelegationMetadataGuard{})
	r.Register(&FilesystemDenialGuard{})
	r.Register(&ConfigProtectionGuard{})

	return r
}

// Register adds a guard to the registry.
func (r *GuardRegistry) Register(g Guard) {
	r.guards[g.Name()] = g
}

// Get returns a guard by name.
func (r *GuardRegistry) Get(name string) (Guard, bool) {
	g, ok := r.guards[name]
	return g, ok
}

// guardsExcludedFromCache lists guards that should not run on cached responses.
// These guards either depend on fresh tool context or are too expensive for cache hits.
var guardsExcludedFromCache = map[string]bool{
	"perspective":         true,
	"declared_action":     true,
	"user_echo":           true,
	"action_verification": true,
}

// Chain materializes a guard chain for the given preset.
// Guard ordering matches the Rust reference chain. Go-only guards are appended
// after the Rust-aligned set so they're additive.
func (r *GuardRegistry) Chain(preset GuardSetPreset) *GuardChain {
	switch preset {
	case GuardSetFull:
		return r.chainFromNames(
			// Rust-aligned order (1–17).
			"empty_response", "subagent_claim", "execution_truth",
			"action_verification", "task_deferral", "output_contract",
			"model_identity_truth", "current_events_truth", "literary_quote_retry",
			"personality_integrity", "internal_jargon", "non_repetition_v2",
			"low_value_parroting", "perspective", "declared_action",
			"user_echo", "internal_protocol",
			// Go-only guards (additive).
			"content_classification", "repetition", "system_prompt_leak",
			"internal_marker", "execution_block", "delegation_metadata",
			"filesystem_denial", "config_protection",
		)
	case GuardSetCached:
		// Rust cached chain: full chain minus PerspectiveGuard, DeclaredActionGuard, UserEchoGuard.
		return r.chainExcluding(guardsExcludedFromCache,
			// Rust-aligned order.
			"empty_response", "subagent_claim", "execution_truth",
			"action_verification", "task_deferral", "output_contract",
			"model_identity_truth", "current_events_truth", "literary_quote_retry",
			"personality_integrity", "internal_jargon", "non_repetition_v2",
			"low_value_parroting", "internal_protocol",
			// Go-only guards (additive).
			"content_classification", "repetition", "system_prompt_leak",
			"internal_marker", "execution_block", "delegation_metadata",
			"filesystem_denial", "config_protection",
		)
	case GuardSetStream:
		// Rust streaming chain: 6 guards.
		return r.chainFromNames(
			"subagent_claim", "current_events_truth", "personality_integrity",
			"internal_jargon", "non_repetition_v2", "internal_protocol",
		)
	case GuardSetNone:
		return NewGuardChain()
	}
	return NewGuardChain()
}

// chainExcluding builds a chain from names, skipping any in the exclusion set.
func (r *GuardRegistry) chainExcluding(exclude map[string]bool, names ...string) *GuardChain {
	var guards []Guard
	for _, name := range names {
		if exclude[name] {
			continue
		}
		if g, ok := r.guards[name]; ok {
			guards = append(guards, g)
		}
	}
	return NewGuardChain(guards...)
}

func (r *GuardRegistry) chainFromNames(names ...string) *GuardChain {
	var guards []Guard
	for _, name := range names {
		if g, ok := r.guards[name]; ok {
			guards = append(guards, g)
		}
	}
	return NewGuardChain(guards...)
}
