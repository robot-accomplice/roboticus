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
	// Core guards.
	r.Register(&EmptyResponseGuard{})
	r.Register(NewContentClassificationGuard())
	r.Register(NewRepetitionGuard())
	r.Register(NewSystemPromptLeakGuard())
	r.Register(NewInternalMarkerGuard())
	// Behavioral guards.
	r.Register(&SubagentClaimGuard{})
	r.Register(&TaskDeferralGuard{})
	r.Register(&InternalJargonGuard{})
	r.Register(&DeclaredActionGuard{})
	// Quality guards.
	r.Register(&LowValueParrotingGuard{})
	r.Register(&NonRepetitionGuardV2{})
	r.Register(&OutputContractGuard{})
	r.Register(&UserEchoGuard{})
	// Truthfulness guards.
	r.Register(&ModelIdentityTruthGuard{})
	r.Register(&CurrentEventsTruthGuard{})
	r.Register(&ExecutionTruthGuard{})
	r.Register(&PersonalityIntegrityGuard{})
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

// Chain materializes a guard chain for the given preset.
func (r *GuardRegistry) Chain(preset GuardSetPreset) *GuardChain {
	switch preset {
	case GuardSetFull, GuardSetCached:
		return r.chainFromNames(
			"empty_response", "content_classification", "repetition",
			"system_prompt_leak", "internal_marker",
			"subagent_claim", "task_deferral", "internal_jargon", "declared_action",
			"low_value_parroting", "non_repetition_v2", "output_contract", "user_echo",
			"model_identity_truth", "current_events_truth", "execution_truth", "personality_integrity",
		)
	case GuardSetStream:
		return r.chainFromNames(
			"empty_response", "subagent_claim", "internal_jargon",
			"personality_integrity", "non_repetition_v2",
		)
	case GuardSetNone:
		return NewGuardChain()
	}
	return NewGuardChain()
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
