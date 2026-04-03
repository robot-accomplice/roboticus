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
	MetascoreSelector func(targets []RouteTarget) *ModelProfile
}

// RouteTarget pairs a model name with its provider and tier.
type RouteTarget struct {
	Model    string
	Provider string
	Tier     ModelTier
	IsLocal  bool
	Cost     float64 // cost per 1K output tokens (for sorting)
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

// EnableMetascoreRouting activates runtime-feedback-driven model selection.
func (r *Router) EnableMetascoreRouting(quality *QualityTracker, capacity *CapacityTracker, breakers *BreakerRegistry) {
	r.MetascoreSelector = func(targets []RouteTarget) *ModelProfile {
		profiles := BuildModelProfiles(targets, quality, capacity, breakers)
		return SelectByMetascore(profiles, breakers)
	}
}

// Select picks the best model for a request based on message complexity.
func (r *Router) Select(req *Request) RouteTarget {
	if len(r.models) == 0 {
		return RouteTarget{Model: req.Model}
	}

	// Model override: bypass all routing logic.
	r.overrideMu.RLock()
	ov := r.override
	r.overrideMu.RUnlock()
	if ov != "" {
		// Try to find a matching target for the override model name.
		for _, m := range r.models {
			if m.Model == ov {
				return m
			}
		}
		return RouteTarget{Model: ov}
	}

	// Round-robin: simple rotating selection across all models.
	if r.roundRobin {
		idx := atomic.AddUint64(&r.roundRobinIdx, 1)
		return r.models[idx%uint64(len(r.models))]
	}

	// Metascore routing overrides heuristic selection when enabled.
	if r.MetascoreSelector != nil {
		if p := r.MetascoreSelector(r.models); p != nil {
			return RouteTarget{Model: p.Model, Provider: p.Provider}
		}
	}

	complexity := estimateComplexity(req)
	targetTier := tierForComplexity(complexity)

	// Find best match for the target tier.
	var best *RouteTarget
	for i := range r.models {
		m := &r.models[i]

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
		for i := range r.models {
			m := &r.models[i]
			if m.Tier >= targetTier {
				if best == nil || m.Tier < best.Tier || (m.Tier == best.Tier && r.costAware && m.Cost < best.Cost) {
					best = m
				}
			}
		}
	}

	// Last resort: return first model.
	if best == nil {
		return r.models[0]
	}
	return *best
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
