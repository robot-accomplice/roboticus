package llm

import (
	"strings"
	"sync"
	"sync/atomic"
	"unicode/utf8"
)

// ModelTier classifies models by capability for routing decisions.
type ModelTier int

const (
	TierSmall    ModelTier = iota // fast, cheap, simple tasks
	TierMedium                    // balanced
	TierLarge                     // capable, more expensive
	TierFrontier                  // most capable, highest cost
)

// Router selects the best model for a given request based on complexity
// heuristics. When MetascoreSelector is set, it overrides the heuristic
// path with a runtime-feedback-driven selection.
type Router struct {
	modelsMu sync.RWMutex
	models     []RouteTarget
	costAware  bool
	localFirst bool
	roundRobin bool

	// roundRobinIdx is atomically incremented for round-robin selection.
	roundRobinIdx uint64

	// override, when set, bypasses all routing logic and returns the
	// specified model directly. Protected by overrideMu.
	override   string
	overrideMu sync.RWMutex

	// MetascoreSelector, when non-nil, is used instead of heuristic routing.
	// Set via Router.EnableMetascoreRouting.
	MetascoreSelector func(targets []RouteTarget, req *Request) *ModelProfile

	// routingWeights holds user-configured axis weights for metascore routing.
	// Protected by overrideMu (reused to avoid a second mutex).
	routingWeights *RoutingWeights

	// intentQuality holds per-(model,intent) quality priors and observations so
	// request-aware routing can score candidates for the current task class
	// rather than only against a global average.
	intentQuality *IntentQualityTracker
}

// RouteTarget pairs a model name with its provider and tier.
type RouteTarget struct {
	Model                string
	Provider             string
	Tier                 ModelTier
	IsLocal              bool
	Cost                 float64 // cost per 1K output tokens (for sorting)
	State                string
	PrimaryReasonCode    string
	ReasonCodes          []string
	HumanReason          string
	EvidenceRefs         []string
	PolicySource         string
	OrchestratorEligible bool
	SubagentEligible     bool
	EligibilityReason    string
}

// RouterConfig controls routing behavior.
type RouterConfig struct {
	CostAware  bool
	LocalFirst bool
	RoundRobin bool
}

// NewRouter creates a model router with the given targets.
func NewRouter(targets []RouteTarget, cfg RouterConfig) *Router {
	return &Router{
		models:     targets,
		costAware:  cfg.CostAware,
		localFirst: cfg.LocalFirst,
		roundRobin: cfg.RoundRobin,
	}
}

// Targets returns a copy of the configured routing targets.
// Used by trace annotations to record which models were considered.
func (r *Router) Targets() []RouteTarget {
	r.modelsMu.RLock()
	defer r.modelsMu.RUnlock()
	out := make([]RouteTarget, len(r.models))
	copy(out, r.models)
	return out
}

// ApplyModelPolicies updates lifecycle-state fields on the configured targets.
// Role eligibility is preserved; only model policy data is replaced.
func (r *Router) ApplyModelPolicies(policies map[string]ModelPolicy) {
	r.modelsMu.Lock()
	defer r.modelsMu.Unlock()
	updated := make([]RouteTarget, len(r.models))
	copy(updated, r.models)
	for i := range updated {
		policy := effectiveModelPolicy(updated[i].Model, policies)
		updated[i].State = policy.State
		updated[i].PrimaryReasonCode = policy.PrimaryReasonCode
		updated[i].ReasonCodes = append([]string(nil), policy.ReasonCodes...)
		updated[i].HumanReason = policy.HumanReason
		updated[i].EvidenceRefs = append([]string(nil), policy.EvidenceRefs...)
		updated[i].PolicySource = policy.Source
	}
	r.models = updated
}

// SetOverride forces all subsequent Select calls to return the given model,
// bypassing all routing logic.
func (r *Router) SetOverride(model string) {
	r.overrideMu.Lock()
	r.override = model
	r.overrideMu.Unlock()
}

// ClearOverride removes the model override, restoring normal routing.
func (r *Router) ClearOverride() {
	r.overrideMu.Lock()
	r.override = ""
	r.overrideMu.Unlock()
}

// SetRoutingWeights updates the user-configured axis weights used by metascore
// routing. Pass nil to revert to DefaultRoutingWeights. Thread-safe.
func (r *Router) SetRoutingWeights(w *RoutingWeights) {
	r.overrideMu.Lock()
	r.routingWeights = w
	r.overrideMu.Unlock()
}

// GetRoutingWeights returns a copy of the current routing weights. Thread-safe.
func (r *Router) GetRoutingWeights() RoutingWeights {
	r.overrideMu.RLock()
	defer r.overrideMu.RUnlock()
	if r.routingWeights != nil {
		return *r.routingWeights
	}
	return DefaultRoutingWeights()
}

// EnableMetascoreRouting activates runtime-feedback-driven model selection.
// The selector reads user-configured routing weights (set via SetRoutingWeights)
// at each invocation so that dashboard changes take effect immediately.
func (r *Router) EnableMetascoreRouting(quality *QualityTracker, latency *LatencyTracker, capacity *CapacityTracker, breakers *BreakerRegistry) {
	r.MetascoreSelector = func(targets []RouteTarget, req *Request) *ModelProfile {
		profiles := BuildModelProfiles(targets, quality, latency, capacity, breakers)
		if intentClass := requestIntentClass(req); intentClass != "" && r.intentQuality != nil {
			for i := range profiles {
				profiles[i].Quality = r.intentQuality.EstimatedQualityForIntent(profiles[i].Model, intentClass)
			}
		}
		w := r.GetRoutingWeights()
		return selectByMetascoreForRequest(profiles, w, breakers, req)
	}
}

// SetIntentQualityTracker installs the per-intent quality tracker used by
// request-aware metascore routing. Thread safety requirements match the rest
// of the router wiring: this is set during service construction or tests.
func (r *Router) SetIntentQualityTracker(iq *IntentQualityTracker) {
	r.intentQuality = iq
}

// Select picks the best model for a request based on message complexity.
func (r *Router) Select(req *Request) RouteTarget {
	models := r.Targets()
	if len(models) == 0 {
		return RouteTarget{Model: req.Model}
	}

	// Model override: bypass all routing logic.
	r.overrideMu.RLock()
	ov := r.override
	r.overrideMu.RUnlock()
	if ov != "" {
		// Try to find a matching target for the override model name.
		for _, m := range models {
			if m.Model == ov {
				return m
			}
		}
		return RouteTarget{Model: ov}
	}

	// Round-robin: simple rotating selection across all models.
	if r.roundRobin {
		targets := filterTargetsForRole(models, req)
		if len(targets) == 0 {
			targets = models
		}
		idx := atomic.AddUint64(&r.roundRobinIdx, 1)
		return targets[idx%uint64(len(targets))]
	}

	// Metascore routing overrides heuristic selection when enabled.
	if r.MetascoreSelector != nil {
		targets := filterTargetsForRole(models, req)
		if len(targets) == 0 {
			targets = models
		}
		if p := r.MetascoreSelector(targets, req); p != nil {
			return RouteTarget{Model: p.Model, Provider: p.Provider}
		}
	}

	targets := filterTargetsForRole(models, req)
	if len(targets) == 0 {
		targets = models
	}

	complexity := estimateComplexity(req)
	targetTier := tierForComplexity(complexity)

	// Find best match for the target tier.
	var best *RouteTarget
	for i := range targets {
		m := &targets[i]

		// Local-first preference.
		if r.localFirst && m.IsLocal && m.Tier >= targetTier {
			return *m
		}

		if m.Tier == targetTier {
			if best == nil || (r.costAware && m.Cost < best.Cost) {
				best = m
			}
		}
	}

	// Fallback: find closest tier upward.
	if best == nil {
		for i := range targets {
			m := &targets[i]
			if m.Tier >= targetTier {
				if best == nil || m.Tier < best.Tier || (m.Tier == best.Tier && r.costAware && m.Cost < best.Cost) {
					best = m
				}
			}
		}
	}

	// Last resort: return first model.
	if best == nil {
		return targets[0]
	}
	return *best
}

func normalizeAgentRole(role string) string {
	switch strings.TrimSpace(strings.ToLower(role)) {
	case "subagent":
		return "subagent"
	default:
		return "orchestrator"
	}
}

func routeTargetEligibleForRole(target RouteTarget, role string) bool {
	if !liveRoutingAllowedForState(target.State) {
		return false
	}
	if !target.OrchestratorEligible && !target.SubagentEligible {
		return true
	}
	switch normalizeAgentRole(role) {
	case "subagent":
		return target.SubagentEligible
	default:
		return target.OrchestratorEligible
	}
}

func filterTargetsForRole(targets []RouteTarget, req *Request) []RouteTarget {
	if len(targets) == 0 {
		return nil
	}
	role := "orchestrator"
	if req != nil {
		role = normalizeAgentRole(req.AgentRole)
	}
	filtered := make([]RouteTarget, 0, len(targets))
	for _, target := range targets {
		if routeTargetEligibleForRole(target, role) {
			filtered = append(filtered, target)
		}
	}
	return filtered
}

// Complexity is a 0.0–1.0 score indicating how hard a request is.
type Complexity float64

// estimateComplexity uses heuristics on the request to estimate difficulty.
func estimateComplexity(req *Request) Complexity {
	var score float64

	// Factor 1: Message length (longer = more complex).
	totalChars := 0
	for _, m := range req.Messages {
		totalChars += utf8.RuneCountInString(m.Content)
	}
	switch {
	case totalChars > 10000:
		score += 0.3
	case totalChars > 3000:
		score += 0.2
	case totalChars > 500:
		score += 0.1
	}

	// Factor 2: Number of messages (more turns = more context to track).
	switch {
	case len(req.Messages) > 20:
		score += 0.2
	case len(req.Messages) > 8:
		score += 0.1
	}

	// Factor 3: Tool usage (tool calls require reasoning).
	if len(req.Tools) > 0 {
		score += 0.15
		if len(req.Tools) > 5 {
			score += 0.1
		}
	}

	// Factor 4: Content signals.
	lastMsg := ""
	if len(req.Messages) > 0 {
		lastMsg = strings.ToLower(req.Messages[len(req.Messages)-1].Content)
	}

	complexitySignals := []string{
		"analyze", "compare", "evaluate", "design", "architect",
		"implement", "refactor", "debug", "optimize", "explain why",
		"trade-off", "security", "performance",
	}
	for _, signal := range complexitySignals {
		if strings.Contains(lastMsg, signal) {
			score += 0.05
			break
		}
	}

	simpleSignals := []string{
		"hello", "hi", "thanks", "yes", "no", "ok",
		"what time", "what date", "how are you",
	}
	for _, signal := range simpleSignals {
		if strings.Contains(lastMsg, signal) && totalChars < 100 {
			score -= 0.2
			break
		}
	}

	// Clamp to [0, 1].
	if score < 0 {
		score = 0
	}
	if score > 1 {
		score = 1
	}
	return Complexity(score)
}

// tierForComplexity maps a complexity score to a model tier.
func tierForComplexity(c Complexity) ModelTier {
	switch {
	case c >= 0.7:
		return TierFrontier
	case c >= 0.4:
		return TierLarge
	case c >= 0.2:
		return TierMedium
	default:
		return TierSmall
	}
}
