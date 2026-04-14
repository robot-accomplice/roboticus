// router.go implements the retrieval router (Layer 8 of the agentic architecture).
//
// The router decides WHICH memory stores to query and HOW, based on intent
// signals and query characteristics. This replaces the previous approach of
// always querying all tiers with fixed budget allocation.
//
// Design principle: each memory type has a different optimal retrieval method.
// The router matches the query's need to the right store + method.
//
// Working memory is NEVER a router target — it's active state injected
// directly into the prompt, not searched through the retrieval pipeline.

package memory

import (
	"strings"

	"github.com/rs/zerolog/log"
)

// RetrievalTarget specifies a single memory tier to query with a specific mode.
type RetrievalTarget struct {
	Tier   MemoryTier    // which store to query
	Mode   RetrievalMode // how to search it
	Weight float64       // importance weight for fusion (0-1)
	Budget float64       // fraction of total token budget (0-1)
}

// RetrievalPlan is the router's output — an ordered list of tiers to query.
type RetrievalPlan struct {
	Targets []RetrievalTarget
}

// IntentSignal is a simplified intent label + confidence for the router.
// Mirrors agent.IntentResult without importing the agent package.
type IntentSignal struct {
	Label      string
	Confidence float64
}

// Router selects memory tiers and retrieval modes based on intent and query signals.
type Router struct {
	corpusSize int
}

// NewRouter creates a retrieval router.
func NewRouter(corpusSize int) *Router {
	return &Router{corpusSize: corpusSize}
}

// Plan produces a retrieval plan based on the query and intent signals.
// The plan specifies which tiers to query, how to query them, and how
// to allocate the token budget across tiers.
func (r *Router) Plan(query string, intents []IntentSignal) RetrievalPlan {
	// Find dominant intent (highest confidence).
	topIntent := ""
	topConf := 0.0
	for _, intent := range intents {
		log.Trace().Str("intent", intent.Label).Float64("confidence", intent.Confidence).
			Msg("router: evaluating intent")
		if intent.Confidence > topConf {
			topIntent = intent.Label
			topConf = intent.Confidence
		}
	}

	// Route by dominant intent.
	switch topIntent {
	case "memory_query":
		return r.planMemoryQuery()
	case "execution":
		return r.planExecution()
	case "creative":
		return r.planCreative()
	case "current_events":
		return r.planCurrentEvents()
	}

	// Route by keyword signals when intent is ambiguous.
	lower := strings.ToLower(query)

	if containsAny(lower, "when did", "what happened", "last time", "previously", "history") {
		return r.planTemporalQuery()
	}
	if containsAny(lower, "how to", "how do i", "steps to", "process for", "procedure") {
		return r.planProceduralQuery()
	}
	if containsAny(lower, "who is", "relationship", "trust", "interact") {
		return r.planRelationshipQuery()
	}
	if containsAny(lower, "policy", "rule", "compliance", "must", "required", "allowed") {
		return r.planPolicyQuery()
	}
	if containsAny(lower, "debug", "error", "fail", "broke", "crash", "issue", "bug") {
		return r.planDebuggingQuery()
	}

	// Default: semantic + episodic hybrid.
	return r.planDefault()
}

// --- Routing plans for specific query types ---

func (r *Router) planMemoryQuery() RetrievalPlan {
	log.Debug().Msg("router: plan=memory_query → semantic(primary) + episodic(secondary)")
	return RetrievalPlan{Targets: []RetrievalTarget{
		{Tier: TierSemantic, Mode: RetrievalHybrid, Weight: 0.7, Budget: 0.5},
		{Tier: TierEpisodic, Mode: RetrievalHybrid, Weight: 0.3, Budget: 0.3},
		{Tier: TierProcedural, Mode: RetrievalKeyword, Weight: 0.1, Budget: 0.2},
	}}
}

func (r *Router) planExecution() RetrievalPlan {
	log.Debug().Msg("router: plan=execution → procedural(primary) + episodic(secondary)")
	return RetrievalPlan{Targets: []RetrievalTarget{
		{Tier: TierProcedural, Mode: RetrievalKeyword, Weight: 0.6, Budget: 0.4},
		{Tier: TierEpisodic, Mode: RetrievalHybrid, Weight: 0.3, Budget: 0.35},
		{Tier: TierSemantic, Mode: RetrievalHybrid, Weight: 0.1, Budget: 0.25},
	}}
}

func (r *Router) planTemporalQuery() RetrievalPlan {
	log.Debug().Msg("router: plan=temporal → episodic(primary) + semantic(secondary)")
	return RetrievalPlan{Targets: []RetrievalTarget{
		{Tier: TierEpisodic, Mode: RetrievalHybrid, Weight: 0.7, Budget: 0.5},
		{Tier: TierSemantic, Mode: RetrievalHybrid, Weight: 0.2, Budget: 0.3},
		{Tier: TierRelationship, Mode: RetrievalKeyword, Weight: 0.1, Budget: 0.2},
	}}
}

func (r *Router) planProceduralQuery() RetrievalPlan {
	log.Debug().Msg("router: plan=procedural → procedural(primary) + semantic(secondary)")
	return RetrievalPlan{Targets: []RetrievalTarget{
		{Tier: TierProcedural, Mode: RetrievalKeyword, Weight: 0.6, Budget: 0.4},
		{Tier: TierSemantic, Mode: RetrievalHybrid, Weight: 0.3, Budget: 0.35},
		{Tier: TierEpisodic, Mode: RetrievalRecency, Weight: 0.1, Budget: 0.25},
	}}
}

func (r *Router) planRelationshipQuery() RetrievalPlan {
	log.Debug().Msg("router: plan=relationship → relationship(primary) + episodic(secondary)")
	return RetrievalPlan{Targets: []RetrievalTarget{
		{Tier: TierRelationship, Mode: RetrievalKeyword, Weight: 0.6, Budget: 0.4},
		{Tier: TierEpisodic, Mode: RetrievalHybrid, Weight: 0.3, Budget: 0.35},
		{Tier: TierSemantic, Mode: RetrievalHybrid, Weight: 0.1, Budget: 0.25},
	}}
}

func (r *Router) planPolicyQuery() RetrievalPlan {
	log.Debug().Msg("router: plan=policy → semantic(canonical, primary)")
	return RetrievalPlan{Targets: []RetrievalTarget{
		{Tier: TierSemantic, Mode: RetrievalKeyword, Weight: 0.9, Budget: 0.7},
		{Tier: TierProcedural, Mode: RetrievalKeyword, Weight: 0.1, Budget: 0.3},
	}}
}

func (r *Router) planDebuggingQuery() RetrievalPlan {
	log.Debug().Msg("router: plan=debugging → episodic(primary) + procedural + semantic")
	return RetrievalPlan{Targets: []RetrievalTarget{
		{Tier: TierEpisodic, Mode: RetrievalHybrid, Weight: 0.5, Budget: 0.35},
		{Tier: TierProcedural, Mode: RetrievalKeyword, Weight: 0.3, Budget: 0.30},
		{Tier: TierSemantic, Mode: RetrievalHybrid, Weight: 0.2, Budget: 0.35},
	}}
}

func (r *Router) planCreative() RetrievalPlan {
	// Creative tasks need minimal retrieval — mainly semantic for style/tone.
	log.Debug().Msg("router: plan=creative → semantic(light)")
	return RetrievalPlan{Targets: []RetrievalTarget{
		{Tier: TierSemantic, Mode: RetrievalHybrid, Weight: 0.7, Budget: 0.5},
		{Tier: TierEpisodic, Mode: RetrievalRecency, Weight: 0.3, Budget: 0.5},
	}}
}

func (r *Router) planCurrentEvents() RetrievalPlan {
	// Current events: episodic (what did we discuss?) + semantic (known facts).
	log.Debug().Msg("router: plan=current_events → episodic + semantic")
	return RetrievalPlan{Targets: []RetrievalTarget{
		{Tier: TierEpisodic, Mode: RetrievalRecency, Weight: 0.6, Budget: 0.5},
		{Tier: TierSemantic, Mode: RetrievalHybrid, Weight: 0.4, Budget: 0.5},
	}}
}

func (r *Router) planDefault() RetrievalPlan {
	log.Debug().Msg("router: plan=default → semantic + episodic balanced")
	return RetrievalPlan{Targets: []RetrievalTarget{
		{Tier: TierSemantic, Mode: RetrievalHybrid, Weight: 0.4, Budget: 0.35},
		{Tier: TierEpisodic, Mode: RetrievalHybrid, Weight: 0.35, Budget: 0.35},
		{Tier: TierProcedural, Mode: RetrievalKeyword, Weight: 0.15, Budget: 0.15},
		{Tier: TierRelationship, Mode: RetrievalKeyword, Weight: 0.1, Budget: 0.15},
	}}
}

// containsAny returns true if s contains any of the substrings.
func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}
