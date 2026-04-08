package llm

import (
	"context"
	"strings"
	"time"
)

// IntentClass categorizes exercise prompts for per-dimension quality tracking.
type IntentClass string

const (
	IntentExecution     IntentClass = "EXECUTION"
	IntentDelegation    IntentClass = "DELEGATION"
	IntentIntrospection IntentClass = "INTROSPECTION"
	IntentConversation  IntentClass = "CONVERSATION"
)

// ComplexityLevel defines exercise difficulty.
type ComplexityLevel int

const (
	ComplexityTrivial  ComplexityLevel = iota // ~1s expected
	ComplexitySimple                          // ~2-5s expected
	ComplexityModerate                        // ~5-15s expected
	ComplexityComplex                         // ~15-30s expected
	ComplexityExpert                          // ~30-60s expected
)

// ExercisePrompt is a single synthetic prompt in the exercise matrix.
type ExercisePrompt struct {
	Prompt     string
	Intent     IntentClass
	Complexity ComplexityLevel
}

// ExerciseMatrix contains 20 synthetic prompts: 5 complexity levels x 4 intent classes.
// Matches the Rust exercise::EXERCISE_MATRIX for quality baselining.
var ExerciseMatrix = []ExercisePrompt{
	// ── Trivial (complexity ~0.1) ──────────────────────────────
	{Prompt: "What time is it?", Intent: IntentExecution, Complexity: ComplexityTrivial},
	{Prompt: "Say hello.", Intent: IntentDelegation, Complexity: ComplexityTrivial},
	{Prompt: "What model are you?", Intent: IntentIntrospection, Complexity: ComplexityTrivial},
	{Prompt: "Thanks!", Intent: IntentConversation, Complexity: ComplexityTrivial},

	// ── Simple (complexity ~0.3) ───────────────────────────────
	{Prompt: "List the files in the workspace directory.", Intent: IntentExecution, Complexity: ComplexitySimple},
	{Prompt: "Check the health of all integrations.", Intent: IntentDelegation, Complexity: ComplexitySimple},
	{Prompt: "What tools do you have access to?", Intent: IntentIntrospection, Complexity: ComplexitySimple},
	{Prompt: "Explain what you can do in one sentence.", Intent: IntentConversation, Complexity: ComplexitySimple},

	// ── Moderate (complexity ~0.5) ─────────────────────────────
	{Prompt: "Read the main configuration file and summarize the model settings.", Intent: IntentExecution, Complexity: ComplexityModerate},
	{Prompt: "Search the workspace for any TODO comments and list them.", Intent: IntentDelegation, Complexity: ComplexityModerate},
	{Prompt: "What memories do you have about recent conversations?", Intent: IntentIntrospection, Complexity: ComplexityModerate},
	{Prompt: "Compare the advantages of local models versus cloud models for my use case.", Intent: IntentConversation, Complexity: ComplexityModerate},

	// ── Complex (complexity ~0.7) ──────────────────────────────
	{Prompt: "Write a shell script that checks disk usage and alerts if any partition is over 90%.", Intent: IntentExecution, Complexity: ComplexityComplex},
	{Prompt: "Create a scheduled task that runs a health check every hour and stores the results.", Intent: IntentDelegation, Complexity: ComplexityComplex},
	{Prompt: "Analyze your recent performance across different task types and suggest which model would handle each best.", Intent: IntentIntrospection, Complexity: ComplexityComplex},
	{Prompt: "Explain the trade-offs between consistency and availability in distributed systems, with examples relevant to my setup.", Intent: IntentConversation, Complexity: ComplexityComplex},

	// ── Expert (complexity ~0.9) ───────────────────────────────
	{Prompt: "Refactor the configuration parser to support hot-reload with validation, rollback on failure, and emit structured change events.", Intent: IntentExecution, Complexity: ComplexityExpert},
	{Prompt: "Orchestrate a multi-step workflow: scan all connected services for vulnerabilities, prioritize findings by severity, and generate a remediation plan with estimated effort.", Intent: IntentDelegation, Complexity: ComplexityExpert},
	{Prompt: "Evaluate your own decision-making process over the last 50 turns: where did you make correct tool choices, where did you waste tokens on unnecessary actions, and what patterns should the routing system learn from?", Intent: IntentIntrospection, Complexity: ComplexityExpert},
	{Prompt: "Design a capability-based security model for a multi-tenant agent platform where each tenant has different trust levels, tool access policies, and cost budgets, considering both the authorization and audit requirements.", Intent: IntentConversation, Complexity: ComplexityExpert},
}

// ModelBaseline holds pre-computed scores for a common model across all 6
// metascore axes. Retained for display purposes (e.g., `models baseline` output).
type ModelBaseline struct {
	Model        string
	Efficacy     float64 // quality / correctness
	Cost         float64 // normalized cost (0 = free, 1 = expensive)
	Availability float64 // typically 1.0 for healthy providers
	Locality     float64 // 1.0 = local, 0.0 = cloud
	Confidence   float64 // observation count penalty (starts low)
	Speed        float64 // latency-based speed score
}

// IntentBaseline is a single (model, intent_class) → quality score tuple.
// Matches the Rust COMMON_MODEL_BASELINES format for per-intent seeding.
type IntentBaseline struct {
	Model       string
	IntentClass string
	Quality     float64
}

// CommonIntentBaselines provides cold-start per-(model, intent_class) quality
// estimates. These are seeded into the IntentQualityTracker at startup so the
// metascore router has differentiated priors instead of a flat 0.5 default.
// Matches Rust's exercise::COMMON_MODEL_BASELINES.
var CommonIntentBaselines = []IntentBaseline{
	// GPT-4o-mini: good at simple tasks, weaker on complex delegation
	{Model: "openai/gpt-4o-mini", IntentClass: "EXECUTION", Quality: 0.70},
	{Model: "openai/gpt-4o-mini", IntentClass: "DELEGATION", Quality: 0.50},
	{Model: "openai/gpt-4o-mini", IntentClass: "INTROSPECTION", Quality: 0.65},
	{Model: "openai/gpt-4o-mini", IntentClass: "CONVERSATION", Quality: 0.80},
	// GPT-4o: strong across the board
	{Model: "openai/gpt-4o", IntentClass: "EXECUTION", Quality: 0.85},
	{Model: "openai/gpt-4o", IntentClass: "DELEGATION", Quality: 0.80},
	{Model: "openai/gpt-4o", IntentClass: "INTROSPECTION", Quality: 0.80},
	{Model: "openai/gpt-4o", IntentClass: "CONVERSATION", Quality: 0.90},
	// Claude Sonnet: very strong at complex tasks
	{Model: "anthropic/claude-sonnet", IntentClass: "EXECUTION", Quality: 0.85},
	{Model: "anthropic/claude-sonnet", IntentClass: "DELEGATION", Quality: 0.85},
	{Model: "anthropic/claude-sonnet", IntentClass: "INTROSPECTION", Quality: 0.80},
	{Model: "anthropic/claude-sonnet", IntentClass: "CONVERSATION", Quality: 0.90},
	// Qwen 2.5 32B: solid local model
	{Model: "ollama/qwen2.5:32b", IntentClass: "EXECUTION", Quality: 0.75},
	{Model: "ollama/qwen2.5:32b", IntentClass: "DELEGATION", Quality: 0.60},
	{Model: "ollama/qwen2.5:32b", IntentClass: "INTROSPECTION", Quality: 0.70},
	{Model: "ollama/qwen2.5:32b", IntentClass: "CONVERSATION", Quality: 0.75},
	// Qwen 3.5 35B: capable local model
	{Model: "ollama/qwen3.5:35b-a3b", IntentClass: "EXECUTION", Quality: 0.70},
	{Model: "ollama/qwen3.5:35b-a3b", IntentClass: "DELEGATION", Quality: 0.55},
	{Model: "ollama/qwen3.5:35b-a3b", IntentClass: "INTROSPECTION", Quality: 0.65},
	{Model: "ollama/qwen3.5:35b-a3b", IntentClass: "CONVERSATION", Quality: 0.70},
	// Gemma 3 13B: lightweight local
	{Model: "ollama/gemma3:13b", IntentClass: "EXECUTION", Quality: 0.60},
	{Model: "ollama/gemma3:13b", IntentClass: "DELEGATION", Quality: 0.40},
	{Model: "ollama/gemma3:13b", IntentClass: "INTROSPECTION", Quality: 0.55},
	{Model: "ollama/gemma3:13b", IntentClass: "CONVERSATION", Quality: 0.65},
	// Mixtral 8x7B: good reasoning, moderate tool use
	{Model: "ollama/mixtral:8x7b", IntentClass: "EXECUTION", Quality: 0.65},
	{Model: "ollama/mixtral:8x7b", IntentClass: "DELEGATION", Quality: 0.50},
	{Model: "ollama/mixtral:8x7b", IntentClass: "INTROSPECTION", Quality: 0.60},
	{Model: "ollama/mixtral:8x7b", IntentClass: "CONVERSATION", Quality: 0.70},
	// Kimi K2: capable cloud model
	{Model: "moonshot/kimi-k2-turbo-preview", IntentClass: "EXECUTION", Quality: 0.75},
	{Model: "moonshot/kimi-k2-turbo-preview", IntentClass: "DELEGATION", Quality: 0.60},
	{Model: "moonshot/kimi-k2-turbo-preview", IntentClass: "INTROSPECTION", Quality: 0.65},
	{Model: "moonshot/kimi-k2-turbo-preview", IntentClass: "CONVERSATION", Quality: 0.75},
}

// CommonModelBaselines provides the legacy 6-axis view for display purposes.
// Derived from CommonIntentBaselines by averaging intent-class quality scores.
var CommonModelBaselines = buildLegacyBaselines()

// ExerciseResult holds the outcome of a single exercise prompt.
type ExerciseResult struct {
	Prompt    ExercisePrompt
	Content   string
	LatencyMs int64
	Quality   float64 // 0-1 scored by validator
	Passed    bool
	Error     string
}

// RunExercise executes the exercise matrix against a completer and returns results.
func RunExercise(ctx context.Context, completer Completer, model string, prompts []ExercisePrompt) []ExerciseResult {
	results := make([]ExerciseResult, 0, len(prompts))

	for _, p := range prompts {
		start := time.Now()
		req := &Request{
			Messages:  []Message{{Role: "user", Content: p.Prompt}},
			MaxTokens: 1024,
			Model:     model,
		}

		resp, err := completer.Complete(ctx, req)
		latencyMs := time.Since(start).Milliseconds()

		result := ExerciseResult{
			Prompt:    p,
			LatencyMs: latencyMs,
		}

		if err != nil {
			result.Error = err.Error()
			results = append(results, result)
			continue
		}

		result.Content = resp.Content
		result.Quality = scoreExerciseResponse(p, resp.Content)
		result.Passed = result.Quality >= 0.3 && result.Error == ""
		results = append(results, result)
	}

	return results
}

// scoreExerciseResponse computes a quality score for an exercise response.
func scoreExerciseResponse(prompt ExercisePrompt, content string) float64 {
	if content == "" {
		return 0.0
	}

	lower := strings.ToLower(content)
	length := len(content)

	switch prompt.Complexity {
	case ComplexityTrivial:
		// Trivial: just needs to respond with something relevant.
		if length > 0 {
			return 0.8
		}
		return 0.0

	case ComplexitySimple:
		// Simple: needs substantive content (>20 chars).
		if length > 20 {
			return 0.8
		}
		if length > 0 {
			return 0.4
		}
		return 0.0

	case ComplexityModerate:
		// Moderate: needs decent length and some structure.
		if length > 100 {
			return 0.9
		}
		if length > 40 {
			return 0.6
		}
		return 0.3

	case ComplexityComplex:
		// Complex: needs substantial content.
		if length > 200 {
			return 0.9
		}
		if length > 80 {
			return 0.6
		}
		return 0.3

	case ComplexityExpert:
		// Expert: needs long, structured response.
		if length > 400 {
			return 0.95
		}
		if length > 150 {
			return 0.7
		}
		return 0.3
	}

	// Fallback: score by length relative to complexity.
	_ = lower // suppress unused warning for future validators
	return min(1.0, float64(length)/200.0)
}

// LookupBaseline finds a pre-computed legacy baseline for a model.
// Uses fuzzy matching: exact match first, then suffix match (e.g., "gemma3:13b" matches "ollama/gemma3:13b").
func LookupBaseline(model string) (*ModelBaseline, bool) {
	baselines := CommonModelBaselines

	// Exact match first.
	for i := range baselines {
		if baselines[i].Model == model {
			return &baselines[i], true
		}
	}

	// Strip provider prefix for suffix matching (e.g., "ollama/gemma3:13b" → "gemma3:13b").
	bare := model
	if idx := strings.LastIndex(model, "/"); idx >= 0 {
		bare = model[idx+1:]
	}
	for i := range baselines {
		if baselines[i].Model == bare {
			return &baselines[i], true
		}
		// Also try stripping provider from the baseline model.
		baselineBare := baselines[i].Model
		if idx := strings.LastIndex(baselineBare, "/"); idx >= 0 {
			baselineBare = baselineBare[idx+1:]
		}
		if baselineBare == bare {
			return &baselines[i], true
		}
	}

	return nil, false
}

// LookupIntentBaselines returns all per-intent baselines for a model.
// Uses fuzzy matching consistent with LookupBaseline.
func LookupIntentBaselines(model string) []IntentBaseline {
	bare := model
	if idx := strings.LastIndex(model, "/"); idx >= 0 {
		bare = model[idx+1:]
	}

	var result []IntentBaseline
	for _, ib := range CommonIntentBaselines {
		if ib.Model == model {
			result = append(result, ib)
			continue
		}
		ibBare := ib.Model
		if idx := strings.LastIndex(ib.Model, "/"); idx >= 0 {
			ibBare = ib.Model[idx+1:]
		}
		if ibBare == bare {
			result = append(result, ib)
		}
	}
	return result
}

// SeedFromBaselines populates the quality tracker with per-model averages from
// the per-intent baselines. Never overwrites real data. Kept for backward compat.
func (qt *QualityTracker) SeedFromBaselines(baselines []ModelBaseline) int {
	seeded := 0
	for _, b := range baselines {
		if qt.HasObservations(b.Model) {
			continue
		}
		qt.Record(b.Model, b.Efficacy)
		seeded++
	}
	return seeded
}

// SeedIntentBaselines populates the IntentQualityTracker with per-(model, intent_class)
// quality scores from CommonIntentBaselines. Matches Rust's seed_from_baselines behavior:
// only seeds cells that have no existing observations.
func (iq *IntentQualityTracker) SeedIntentBaselines(baselines []IntentBaseline) int {
	seeded := 0
	for _, b := range baselines {
		// Check if this (model, intentClass) cell already has observations.
		key := IntentClassKey{Model: b.Model, IntentClass: b.IntentClass}
		iq.mu.RLock()
		rb, exists := iq.intents[key]
		hasData := exists && rb.count > 0
		iq.mu.RUnlock()

		if hasData {
			continue
		}
		iq.RecordWithIntent(b.Model, b.IntentClass, b.Quality)
		seeded++
	}
	return seeded
}

// buildLegacyBaselines derives 6-axis ModelBaseline entries from CommonIntentBaselines
// by averaging quality scores per model. Non-quality axes are set to sensible defaults
// based on whether the model is local (ollama/) or cloud.
func buildLegacyBaselines() []ModelBaseline {
	type acc struct {
		sum   float64
		count int
	}
	byModel := make(map[string]*acc)
	// Preserve insertion order.
	var order []string
	for _, ib := range CommonIntentBaselines {
		a, ok := byModel[ib.Model]
		if !ok {
			a = &acc{}
			byModel[ib.Model] = a
			order = append(order, ib.Model)
		}
		a.sum += ib.Quality
		a.count++
	}

	result := make([]ModelBaseline, 0, len(order))
	for _, model := range order {
		a := byModel[model]
		avgQ := a.sum / float64(a.count)

		isLocal := strings.HasPrefix(model, "ollama/")
		locality := 0.0
		cost := 0.5
		speed := 0.7
		if isLocal {
			locality = 1.0
			cost = 0.0
			speed = 0.55
		}

		// Strip provider for the Model field to match legacy behavior.
		bare := model
		if idx := strings.LastIndex(model, "/"); idx >= 0 {
			bare = model[idx+1:]
		}

		result = append(result, ModelBaseline{
			Model:        bare,
			Efficacy:     avgQ,
			Cost:         cost,
			Availability: 1.0,
			Locality:     locality,
			Confidence:   0.3,
			Speed:        speed,
		})
	}
	return result
}
