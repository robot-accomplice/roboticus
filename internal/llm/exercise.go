package llm

import (
	"context"
	"strings"
	"time"
)

// IntentClass categorizes exercise prompts for per-dimension quality tracking.
type IntentClass string

const (
	IntentExecution     IntentClass = "execution"
	IntentDelegation    IntentClass = "delegation"
	IntentIntrospection IntentClass = "introspection"
	IntentConversation  IntentClass = "conversation"
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
	// Trivial (fast, deterministic)
	{Prompt: "Respond with exactly: OK", Intent: IntentExecution, Complexity: ComplexityTrivial},
	{Prompt: "Say hello.", Intent: IntentConversation, Complexity: ComplexityTrivial},
	{Prompt: "What model are you?", Intent: IntentIntrospection, Complexity: ComplexityTrivial},
	{Prompt: "What time is it?", Intent: IntentDelegation, Complexity: ComplexityTrivial},

	// Simple (short answers, single-step)
	{Prompt: "Count from 1 to 5, one number per line.", Intent: IntentExecution, Complexity: ComplexitySimple},
	{Prompt: "What tools do you have available?", Intent: IntentIntrospection, Complexity: ComplexitySimple},
	{Prompt: "Check your health status.", Intent: IntentDelegation, Complexity: ComplexitySimple},
	{Prompt: "What is the capital of France?", Intent: IntentConversation, Complexity: ComplexitySimple},

	// Moderate (multi-sentence, may require reasoning)
	{Prompt: "Explain the difference between TCP and UDP in 2-3 sentences.", Intent: IntentConversation, Complexity: ComplexityModerate},
	{Prompt: "Describe your memory system and how it works.", Intent: IntentIntrospection, Complexity: ComplexityModerate},
	{Prompt: "Search your workspace for TODO comments and summarize what you find.", Intent: IntentDelegation, Complexity: ComplexityModerate},
	{Prompt: "Write a haiku about programming.", Intent: IntentExecution, Complexity: ComplexityModerate},

	// Complex (multi-step, structured output)
	{Prompt: "Write a shell script that reports disk usage for the top 5 directories by size.", Intent: IntentExecution, Complexity: ComplexityComplex},
	{Prompt: "Compare and contrast REST and GraphQL APIs, covering pros, cons, and use cases.", Intent: IntentConversation, Complexity: ComplexityComplex},
	{Prompt: "Create a scheduled health check that runs every 5 minutes and reports system status.", Intent: IntentDelegation, Complexity: ComplexityComplex},
	{Prompt: "Explain your routing and model selection strategy in detail.", Intent: IntentIntrospection, Complexity: ComplexityComplex},

	// Expert (deep reasoning, architecture-level)
	{Prompt: "Design a config parser in Go with hot-reload support. Show the key types and reload mechanism.", Intent: IntentExecution, Complexity: ComplexityExpert},
	{Prompt: "Explain the CAP theorem and its implications for distributed database design with concrete examples.", Intent: IntentConversation, Complexity: ComplexityExpert},
	{Prompt: "Orchestrate a multi-step security scan: enumerate open ports, check SSL certificates, and test for common vulnerabilities.", Intent: IntentDelegation, Complexity: ComplexityExpert},
	{Prompt: "Audit your own performance: analyze your recent response quality, latency distribution, and model selection patterns.", Intent: IntentIntrospection, Complexity: ComplexityExpert},
}

// ModelBaseline holds pre-computed scores for a common model across all 6
// metascore axes. Used to seed cold-start models so routing works on day 1.
type ModelBaseline struct {
	Model        string
	Efficacy     float64 // quality / correctness
	Cost         float64 // normalized cost (0 = free, 1 = expensive)
	Availability float64 // typically 1.0 for healthy providers
	Locality     float64 // 1.0 = local, 0.0 = cloud
	Confidence   float64 // observation count penalty (starts low)
	Speed        float64 // latency-based speed score
}

// CommonModelBaselines provides cold-start quality estimates for common models.
// These are seeded ONLY when a model has zero real observations — they never
// overwrite real historical data.
var CommonModelBaselines = []ModelBaseline{
	// Cloud models (high quality, high cost, low locality)
	{Model: "gpt-4o", Efficacy: 0.90, Cost: 0.80, Availability: 1.0, Locality: 0.0, Confidence: 0.3, Speed: 0.70},
	{Model: "gpt-4o-mini", Efficacy: 0.75, Cost: 0.30, Availability: 1.0, Locality: 0.0, Confidence: 0.3, Speed: 0.85},
	{Model: "claude-sonnet-4-20250514", Efficacy: 0.92, Cost: 0.75, Availability: 1.0, Locality: 0.0, Confidence: 0.3, Speed: 0.65},
	{Model: "kimi-k2-turbo-preview", Efficacy: 0.78, Cost: 0.40, Availability: 1.0, Locality: 0.0, Confidence: 0.3, Speed: 0.75},

	// Local models (lower quality, zero cost, full locality, variable speed)
	{Model: "qwen2.5:32b", Efficacy: 0.72, Cost: 0.0, Availability: 1.0, Locality: 1.0, Confidence: 0.3, Speed: 0.50},
	{Model: "qwen3.5:35b-a3b", Efficacy: 0.70, Cost: 0.0, Availability: 1.0, Locality: 1.0, Confidence: 0.3, Speed: 0.55},
	{Model: "gemma3:12b", Efficacy: 0.60, Cost: 0.0, Availability: 1.0, Locality: 1.0, Confidence: 0.3, Speed: 0.65},
	{Model: "gemma4", Efficacy: 0.75, Cost: 0.0, Availability: 1.0, Locality: 1.0, Confidence: 0.3, Speed: 0.60},
	{Model: "mixtral:8x7b", Efficacy: 0.65, Cost: 0.0, Availability: 1.0, Locality: 1.0, Confidence: 0.3, Speed: 0.55},
	{Model: "phi4-reasoning:14b", Efficacy: 0.68, Cost: 0.0, Availability: 1.0, Locality: 1.0, Confidence: 0.3, Speed: 0.60},
}

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

// LookupBaseline finds a pre-computed baseline for a model.
// Uses fuzzy matching: exact match first, then suffix match (e.g., "gemma4" matches "ollama/gemma4").
func LookupBaseline(model string) (*ModelBaseline, bool) {
	// Exact match first.
	for i := range CommonModelBaselines {
		if CommonModelBaselines[i].Model == model {
			return &CommonModelBaselines[i], true
		}
	}

	// Strip provider prefix for suffix matching (e.g., "ollama/gemma4" → "gemma4").
	bare := model
	if idx := strings.LastIndex(model, "/"); idx >= 0 {
		bare = model[idx+1:]
	}
	for i := range CommonModelBaselines {
		if CommonModelBaselines[i].Model == bare {
			return &CommonModelBaselines[i], true
		}
		// Also try stripping provider from the baseline model.
		baselineBare := CommonModelBaselines[i].Model
		if idx := strings.LastIndex(baselineBare, "/"); idx >= 0 {
			baselineBare = baselineBare[idx+1:]
		}
		if baselineBare == bare {
			return &CommonModelBaselines[i], true
		}
	}

	return nil, false
}

// SeedFromBaselines populates the quality tracker with pre-computed scores
// for models that have zero real observations. Never overwrites real data.
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
