package llm

import (
	"context"
	"strings"
	"time"
)

// IntentClass categorizes exercise prompts for per-dimension quality tracking.
// Uses int enum — no string comparison bugs possible.
type IntentClass int

const (
	IntentExecution IntentClass = iota
	IntentDelegation
	IntentIntrospection
	IntentConversation
	IntentMemoryRecall
	IntentToolUse
)

// String returns the canonical label for this intent class.
func (ic IntentClass) String() string {
	switch ic {
	case IntentExecution:
		return "EXECUTION"
	case IntentDelegation:
		return "DELEGATION"
	case IntentIntrospection:
		return "INTROSPECTION"
	case IntentConversation:
		return "CONVERSATION"
	case IntentMemoryRecall:
		return "MEMORY_RECALL"
	case IntentToolUse:
		return "TOOL_USE"
	default:
		return "UNKNOWN"
	}
}

// ParseIntentClass converts a string label to an IntentClass.
// Case-insensitive. Returns IntentExecution as fallback for unknown strings.
func ParseIntentClass(s string) IntentClass {
	switch strings.ToUpper(s) {
	case "EXECUTION":
		return IntentExecution
	case "DELEGATION":
		return IntentDelegation
	case "INTROSPECTION":
		return IntentIntrospection
	case "CONVERSATION":
		return IntentConversation
	case "MEMORY_RECALL":
		return IntentMemoryRecall
	case "TOOL_USE":
		return IntentToolUse
	default:
		return IntentExecution
	}
}

// AllIntentClasses returns all defined intent classes in order.
func AllIntentClasses() []IntentClass {
	return []IntentClass{IntentExecution, IntentDelegation, IntentIntrospection, IntentConversation, IntentMemoryRecall, IntentToolUse}
}

// ComplexityLevel defines exercise difficulty.
type ComplexityLevel int

const (
	ComplexityTrivial  ComplexityLevel = iota // ~1s expected
	ComplexitySimple                          // ~2-5s expected
	ComplexityModerate                        // ~5-15s expected
	ComplexityComplex                         // ~15-30s expected
	ComplexityExpert                          // ~30-60s expected
)

// String returns the complexity label.
func (c ComplexityLevel) String() string {
	switch c {
	case ComplexityTrivial:
		return "trivial"
	case ComplexitySimple:
		return "simple"
	case ComplexityModerate:
		return "moderate"
	case ComplexityComplex:
		return "complex"
	case ComplexityExpert:
		return "expert"
	default:
		return "unknown"
	}
}

// ExercisePrompt is a single synthetic prompt in the exercise matrix.
type ExercisePrompt struct {
	Prompt     string
	Intent     IntentClass
	Complexity ComplexityLevel
}

// ExerciseMatrix contains 30 synthetic prompts: 5 complexity levels x 6 intent classes.
// Extends the Rust exercise::EXERCISE_MATRIX with IntentMemoryRecall and IntentToolUse (beyond-parity).
var ExerciseMatrix = []ExercisePrompt{
	// ── Trivial (complexity ~0.1) ──────────────────────────────
	{Prompt: "What time is it?", Intent: IntentExecution, Complexity: ComplexityTrivial},
	{Prompt: "Say hello.", Intent: IntentDelegation, Complexity: ComplexityTrivial},
	{Prompt: "When should you refuse a request instead of attempting it?", Intent: IntentIntrospection, Complexity: ComplexityTrivial},
	{Prompt: "Thanks!", Intent: IntentConversation, Complexity: ComplexityTrivial},
	{Prompt: "Do you have any memories stored?", Intent: IntentMemoryRecall, Complexity: ComplexityTrivial},
	{Prompt: "What is 2 + 2?", Intent: IntentToolUse, Complexity: ComplexityTrivial}, // Should NOT use tools — pure reasoning

	// ── Simple (complexity ~0.3) ───────────────────────────────
	{Prompt: "List the files in the workspace directory.", Intent: IntentExecution, Complexity: ComplexitySimple},
	{Prompt: "Check the health of all integrations.", Intent: IntentDelegation, Complexity: ComplexitySimple},
	{Prompt: "What are your biggest limitations when handling complex multi-step tasks?", Intent: IntentIntrospection, Complexity: ComplexitySimple},
	{Prompt: "Explain what you can do in one sentence.", Intent: IntentConversation, Complexity: ComplexitySimple},
	{Prompt: "What do you remember about our last conversation?", Intent: IntentMemoryRecall, Complexity: ComplexitySimple},
	{Prompt: "Show me the contents of the README file.", Intent: IntentToolUse, Complexity: ComplexitySimple}, // Should use read_file

	// ── Moderate (complexity ~0.5) ─────────────────────────────
	{Prompt: "Read the main configuration file and summarize the model settings.", Intent: IntentExecution, Complexity: ComplexityModerate},
	{Prompt: "Search the workspace for any TODO comments and list them.", Intent: IntentDelegation, Complexity: ComplexityModerate},
	{Prompt: "How would you know if you were giving a wrong answer, and what would you do about it?", Intent: IntentIntrospection, Complexity: ComplexityModerate},
	{Prompt: "Compare the advantages of local models versus cloud models for my use case.", Intent: IntentConversation, Complexity: ComplexityModerate},
	{Prompt: "Search your memories for anything about the deployment project. What can you find?", Intent: IntentMemoryRecall, Complexity: ComplexityModerate},
	{Prompt: "Look up how many sessions were created today by querying the database.", Intent: IntentToolUse, Complexity: ComplexityModerate}, // Should use query_table

	// ── Complex (complexity ~0.7) ──────────────────────────────
	{Prompt: "Write a shell script that checks disk usage and alerts if any partition is over 90%.", Intent: IntentExecution, Complexity: ComplexityComplex},
	{Prompt: "Create a scheduled task that runs a health check every hour and stores the results.", Intent: IntentDelegation, Complexity: ComplexityComplex},
	{Prompt: "Analyze your recent performance across different task types and suggest which model would handle each best.", Intent: IntentIntrospection, Complexity: ComplexityComplex},
	{Prompt: "Explain the trade-offs between consistency and availability in distributed systems, with examples relevant to my setup.", Intent: IntentConversation, Complexity: ComplexityComplex},
	{Prompt: "I told you something important about palm a few months ago. Use your search_memories tool to look it up and tell me what you find.", Intent: IntentMemoryRecall, Complexity: ComplexityComplex},
	{Prompt: "Find all files in the workspace that were modified in the last 24 hours, read the 3 most recent, and summarize the changes.", Intent: IntentToolUse, Complexity: ComplexityComplex}, // Should chain bash + read_file

	// ── Expert (complexity ~0.9) ───────────────────────────────
	{Prompt: "Refactor the configuration parser to support hot-reload with validation, rollback on failure, and emit structured change events.", Intent: IntentExecution, Complexity: ComplexityExpert},
	{Prompt: "Orchestrate a multi-step workflow: scan all connected services for vulnerabilities, prioritize findings by severity, and generate a remediation plan with estimated effort.", Intent: IntentDelegation, Complexity: ComplexityExpert},
	{Prompt: "Evaluate your own decision-making process over the last 50 turns: where did you make correct tool choices, where did you waste tokens on unnecessary actions, and what patterns should the routing system learn from?", Intent: IntentIntrospection, Complexity: ComplexityExpert},
	{Prompt: "Design a capability-based security model for a multi-tenant agent platform where each tenant has different trust levels, tool access policies, and cost budgets, considering both the authorization and audit requirements.", Intent: IntentConversation, Complexity: ComplexityExpert},
	{Prompt: "Cross-reference your episodic and semantic memories about the infrastructure migration project, then search for any related relationship data about the people involved. Compile a timeline of what happened, who was involved, and what decisions were made.", Intent: IntentMemoryRecall, Complexity: ComplexityExpert},
	{Prompt: "Query the sessions database for the last 10 conversations, analyze the tool call patterns in each, cross-reference with the inference costs table to calculate per-tool cost efficiency, and write a report to the workspace.", Intent: IntentToolUse, Complexity: ComplexityExpert}, // Should chain query_table + bash + write_file
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
	{Model: "openai/gpt-4o-mini", IntentClass: "MEMORY_RECALL", Quality: 0.65},
	{Model: "openai/gpt-4o-mini", IntentClass: "TOOL_USE", Quality: 0.60},
	// GPT-4o: strong across the board
	{Model: "openai/gpt-4o", IntentClass: "EXECUTION", Quality: 0.85},
	{Model: "openai/gpt-4o", IntentClass: "DELEGATION", Quality: 0.80},
	{Model: "openai/gpt-4o", IntentClass: "INTROSPECTION", Quality: 0.80},
	{Model: "openai/gpt-4o", IntentClass: "CONVERSATION", Quality: 0.90},
	{Model: "openai/gpt-4o", IntentClass: "MEMORY_RECALL", Quality: 0.80},
	{Model: "openai/gpt-4o", IntentClass: "TOOL_USE", Quality: 0.85},
	// Claude Sonnet: very strong at complex tasks
	{Model: "anthropic/claude-sonnet", IntentClass: "EXECUTION", Quality: 0.85},
	{Model: "anthropic/claude-sonnet", IntentClass: "DELEGATION", Quality: 0.85},
	{Model: "anthropic/claude-sonnet", IntentClass: "INTROSPECTION", Quality: 0.80},
	{Model: "anthropic/claude-sonnet", IntentClass: "CONVERSATION", Quality: 0.90},
	{Model: "anthropic/claude-sonnet", IntentClass: "MEMORY_RECALL", Quality: 0.85},
	{Model: "anthropic/claude-sonnet", IntentClass: "TOOL_USE", Quality: 0.90},
	// Qwen 2.5 32B: solid local model
	{Model: "ollama/qwen2.5:32b", IntentClass: "EXECUTION", Quality: 0.75},
	{Model: "ollama/qwen2.5:32b", IntentClass: "DELEGATION", Quality: 0.60},
	{Model: "ollama/qwen2.5:32b", IntentClass: "INTROSPECTION", Quality: 0.70},
	{Model: "ollama/qwen2.5:32b", IntentClass: "CONVERSATION", Quality: 0.75},
	{Model: "ollama/qwen2.5:32b", IntentClass: "MEMORY_RECALL", Quality: 0.55},
	{Model: "ollama/qwen2.5:32b", IntentClass: "TOOL_USE", Quality: 0.65},
	// Qwen 3.5 35B: capable local model
	{Model: "ollama/qwen3.5:35b-a3b", IntentClass: "EXECUTION", Quality: 0.70},
	{Model: "ollama/qwen3.5:35b-a3b", IntentClass: "DELEGATION", Quality: 0.55},
	{Model: "ollama/qwen3.5:35b-a3b", IntentClass: "INTROSPECTION", Quality: 0.65},
	{Model: "ollama/qwen3.5:35b-a3b", IntentClass: "CONVERSATION", Quality: 0.70},
	{Model: "ollama/qwen3.5:35b-a3b", IntentClass: "MEMORY_RECALL", Quality: 0.50},
	{Model: "ollama/qwen3.5:35b-a3b", IntentClass: "TOOL_USE", Quality: 0.55},
	// Gemma 3 13B: lightweight local — weak tool selection
	{Model: "ollama/gemma3:13b", IntentClass: "EXECUTION", Quality: 0.60},
	{Model: "ollama/gemma3:13b", IntentClass: "DELEGATION", Quality: 0.40},
	{Model: "ollama/gemma3:13b", IntentClass: "INTROSPECTION", Quality: 0.55},
	{Model: "ollama/gemma3:13b", IntentClass: "CONVERSATION", Quality: 0.65},
	{Model: "ollama/gemma3:13b", IntentClass: "MEMORY_RECALL", Quality: 0.30},
	{Model: "ollama/gemma3:13b", IntentClass: "TOOL_USE", Quality: 0.35},
	// Mixtral 8x7B: good reasoning, moderate tool use
	{Model: "ollama/mixtral:8x7b", IntentClass: "EXECUTION", Quality: 0.65},
	{Model: "ollama/mixtral:8x7b", IntentClass: "DELEGATION", Quality: 0.50},
	{Model: "ollama/mixtral:8x7b", IntentClass: "INTROSPECTION", Quality: 0.60},
	{Model: "ollama/mixtral:8x7b", IntentClass: "CONVERSATION", Quality: 0.70},
	{Model: "ollama/mixtral:8x7b", IntentClass: "MEMORY_RECALL", Quality: 0.45},
	{Model: "ollama/mixtral:8x7b", IntentClass: "TOOL_USE", Quality: 0.50},
	// Kimi K2: capable cloud model — good at tool calls, memory recall proven
	{Model: "moonshot/kimi-k2-turbo-preview", IntentClass: "EXECUTION", Quality: 0.75},
	{Model: "moonshot/kimi-k2-turbo-preview", IntentClass: "DELEGATION", Quality: 0.60},
	{Model: "moonshot/kimi-k2-turbo-preview", IntentClass: "INTROSPECTION", Quality: 0.65},
	{Model: "moonshot/kimi-k2-turbo-preview", IntentClass: "CONVERSATION", Quality: 0.75},
	{Model: "moonshot/kimi-k2-turbo-preview", IntentClass: "MEMORY_RECALL", Quality: 0.70},
	{Model: "moonshot/kimi-k2-turbo-preview", IntentClass: "TOOL_USE", Quality: 0.70},
	// Gemma 4: primary local model — confabulates instead of calling tools
	{Model: "ollama/gemma4", IntentClass: "EXECUTION", Quality: 0.65},
	{Model: "ollama/gemma4", IntentClass: "DELEGATION", Quality: 0.45},
	{Model: "ollama/gemma4", IntentClass: "INTROSPECTION", Quality: 0.60},
	{Model: "ollama/gemma4", IntentClass: "CONVERSATION", Quality: 0.70},
	{Model: "ollama/gemma4", IntentClass: "MEMORY_RECALL", Quality: 0.25},
	{Model: "ollama/gemma4", IntentClass: "TOOL_USE", Quality: 0.30},
}

// CommonModelBaselines provides the legacy 6-axis view for display purposes.
// Derived from CommonIntentBaselines by averaging intent-class quality scores.
var CommonModelBaselines = buildLegacyBaselines()

// BaselineQualityInfo holds aggregated quality data for a model from baselines.
type BaselineQualityInfo struct {
	Known       bool               // true if model has baseline data
	AvgQuality  float64            // average across all intent classes
	ByIntent    map[string]float64 // per-intent quality scores
	BestIntent  string             // intent class with highest quality
	WorstIntent string             // intent class with lowest quality
}

// LookupBaselineQuality returns aggregated quality info for a model from
// CommonIntentBaselines. Tries exact match first, then bare model name
// (without provider prefix). Returns Known=false if no baseline data exists.
func LookupBaselineQuality(model string) BaselineQualityInfo {
	byIntent := make(map[string]float64)
	for _, b := range CommonIntentBaselines {
		if b.Model == model {
			byIntent[b.IntentClass] = b.Quality
		}
	}
	// Fallback: try bare model name (strip provider/).
	if len(byIntent) == 0 {
		bare := model
		if idx := strings.Index(model, "/"); idx >= 0 {
			bare = model[idx+1:]
		}
		for _, b := range CommonIntentBaselines {
			bBare := b.Model
			if idx := strings.Index(b.Model, "/"); idx >= 0 {
				bBare = b.Model[idx+1:]
			}
			if bBare == bare {
				byIntent[b.IntentClass] = b.Quality
			}
		}
	}
	if len(byIntent) == 0 {
		return BaselineQualityInfo{Known: false, AvgQuality: 0}
	}

	sum := 0.0
	bestQ, worstQ := 0.0, 1.0
	var bestI, worstI string
	for intent, q := range byIntent {
		sum += q
		if q >= bestQ {
			bestQ = q
			bestI = intent
		}
		if q <= worstQ {
			worstQ = q
			worstI = intent
		}
	}
	return BaselineQualityInfo{
		Known:       true,
		AvgQuality:  sum / float64(len(byIntent)),
		ByIntent:    byIntent,
		BestIntent:  bestI,
		WorstIntent: worstI,
	}
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

// ExerciseCallback is called after each prompt completes, enabling per-prompt
// persistence for interrupt resilience. May be nil.
type ExerciseCallback func(index int, result ExerciseResult)

// RunExercise executes the exercise matrix against a completer and returns results.
// If onResult is non-nil, it's called after each prompt completes (for persistence).
func RunExercise(ctx context.Context, completer Completer, model string, prompts []ExercisePrompt, onResult ...ExerciseCallback) []ExerciseResult {
	var cb ExerciseCallback
	if len(onResult) > 0 {
		cb = onResult[0]
	}

	results := make([]ExerciseResult, 0, len(prompts))

	for i, p := range prompts {
		// Check for context cancellation between prompts.
		if ctx.Err() != nil {
			result := ExerciseResult{Prompt: p, Error: "context cancelled"}
			results = append(results, result)
			if cb != nil {
				cb(i, result)
			}
			continue
		}

		start := time.Now()
		req := &Request{
			Messages:   []Message{{Role: "user", Content: p.Prompt}},
			MaxTokens:  1024,
			Model:      model,
			NoEscalate: true, // Measure this model's raw capability, no fallback.
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
			if cb != nil {
				cb(i, result)
			}
			continue
		}

		result.Content = resp.Content
		result.Quality = scoreExerciseResponse(p, resp.Content)
		result.Passed = result.Quality >= 0.3 && result.Error == ""
		results = append(results, result)
		if cb != nil {
			cb(i, result)
		}
	}

	return results
}

// scoreExerciseResponse computes a quality score for an exercise response.
// Combines length adequacy (40%), intent relevance (30%), and structural
// quality (30%) into a single 0-1 score.
func scoreExerciseResponse(prompt ExercisePrompt, content string) float64 {
	if content == "" {
		return 0.0
	}

	lower := strings.ToLower(content)
	length := len(content)

	// Dimension 1: Length adequacy (0-1) — does the response meet the
	// minimum length expectation for this complexity level?
	lengthScore := scoreLengthAdequacy(length, prompt.Complexity)

	// Dimension 2: Intent relevance (0-1) — does the response demonstrate
	// behavior appropriate for the intent class?
	relevanceScore := scoreIntentRelevance(lower, prompt.Intent)

	// Dimension 3: Structural quality (0-1) — coherence markers.
	structureScore := scoreStructure(lower, length)

	// Weighted combination.
	score := 0.4*lengthScore + 0.3*relevanceScore + 0.3*structureScore

	// Memory recall: apply confabulation penalty at top level.
	// A fluent, well-structured fabrication should still score poorly.
	if prompt.Intent == IntentMemoryRecall {
		confabMarkers := []string{"as i recall", "from our previous", "i remember that",
			"based on our history", "in our last conversation", "you mentioned"}
		confabHits := 0
		for _, m := range confabMarkers {
			if strings.Contains(lower, m) {
				confabHits++
			}
		}
		hasToolEvidence := strings.Contains(lower, "search_memories") ||
			strings.Contains(lower, "recall_memory") ||
			strings.Contains(lower, "tool")
		if confabHits >= 2 && !hasToolEvidence {
			score *= 0.4 // Heavy penalty: fluent confabulation is the worst failure mode.
		}
	}

	return score
}

// scoreLengthAdequacy scores response length relative to complexity expectations.
func scoreLengthAdequacy(length int, complexity ComplexityLevel) float64 {
	// Minimum expected lengths per complexity tier.
	thresholds := map[ComplexityLevel][2]int{
		ComplexityTrivial:  {1, 10},
		ComplexitySimple:   {20, 80},
		ComplexityModerate: {60, 200},
		ComplexityComplex:  {120, 400},
		ComplexityExpert:   {200, 600},
	}
	minLen, goodLen := thresholds[complexity][0], thresholds[complexity][1]
	if length < minLen {
		return float64(length) / float64(minLen) * 0.3
	}
	if length >= goodLen {
		return 1.0
	}
	return 0.5 + 0.5*float64(length-minLen)/float64(goodLen-minLen)
}

// scoreIntentRelevance checks whether the response contains markers appropriate
// for the requested intent class (tool calls, delegation language, introspection, etc.).
func scoreIntentRelevance(lower string, intent IntentClass) float64 {
	switch intent {
	case IntentExecution:
		// Execution responses should mention tool usage, actions, results, or data.
		markers := []string{"tool", "result", "file", "directory", "output", "command",
			"executed", "running", "error", "success", "list", "found", "created"}
		return markerScore(lower, markers, 2)

	case IntentDelegation:
		// Delegation responses should mention forwarding, checking, or coordination.
		markers := []string{"check", "health", "status", "integration", "delegate",
			"forward", "agent", "task", "assigned", "search", "scan"}
		return markerScore(lower, markers, 2)

	case IntentIntrospection:
		// Introspection responses should demonstrate genuine self-assessment:
		// acknowledging limitations, reasoning about own behavior, identifying
		// failure modes, not just listing capabilities from the system prompt.
		strengthMarkers := []string{"limitation", "unable", "cannot", "difficult",
			"challenge", "risk", "mistake", "wrong", "uncertain", "confidence",
			"trade-off", "tradeoff", "depends", "context", "judgment",
			"improve", "weakness", "failure", "careful", "verify"}
		score := markerScore(lower, strengthMarkers, 3) * 0.6

		// Some self-reference is expected but shouldn't dominate.
		selfMarkers := []string{"i ", "my ", "i'm", "i can", "i would"}
		score += markerScore(lower, selfMarkers, 1) * 0.2

		// Specificity: references to concrete behaviors, not abstract platitudes.
		specificMarkers := []string{"tool", "query", "file", "memory", "session",
			"token", "latency", "cost", "escalat", "fallback"}
		score += markerScore(lower, specificMarkers, 2) * 0.2

		return score

	case IntentConversation:
		// Conversation responses should be warm, natural, and engaged.
		markers := []string{"!", "glad", "happy", "welcome", "thank", "help",
			"sure", "of course", "here", "let me", "can "}
		return markerScore(lower, markers, 1)

	case IntentMemoryRecall:
		// Memory recall responses should demonstrate actual memory retrieval behavior:
		// calling search_memories or recall_memory, referencing found/not-found results,
		// and being honest about what was or wasn't found. Confabulation (fabricating
		// memories without tool calls) should score poorly.
		score := 0.0

		// Positive: evidence of tool use for memory search.
		toolMarkers := []string{"search_memories", "recall_memory", "memory_index",
			"tool_call", "search", "recall", "found", "no memories", "not found",
			"stored", "memory store"}
		score += 0.5 * markerScore(lower, toolMarkers, 2)

		// Positive: honesty markers (admitting what is/isn't available).
		honestyMarkers := []string{"i found", "i don't have", "no results",
			"no memories found", "not stored", "let me search", "searching"}
		score += 0.3 * markerScore(lower, honestyMarkers, 1)

		score += 0.2 // Base credit for responding at all.
		if score < 0 {
			score = 0
		}
		if score > 1 {
			score = 1
		}
		return score

	case IntentToolUse:
		// Tool use responses should demonstrate correct tool selection judgment:
		// using the right tool for the job (read_file for reading, query_table for DB,
		// bash for shell commands) and NOT using tools when pure reasoning suffices.
		score := 0.0

		// Positive: evidence of deliberate tool selection.
		toolSelectionMarkers := []string{"read_file", "write_file", "query_table",
			"bash", "tool_call", "list_dir", "search", "executed",
			"result", "output", "command", "queried", "rows"}
		score += 0.5 * markerScore(lower, toolSelectionMarkers, 2)

		// Positive: structured output from tool execution.
		structuredOutputMarkers := []string{"found", "returned", "entries", "total",
			"shows", "contains", "results", "created", "modified"}
		score += 0.3 * markerScore(lower, structuredOutputMarkers, 1)

		score += 0.2 // Base credit for responding.
		if score > 1 {
			score = 1
		}
		return score

	default:
		return 0.5
	}
}

// markerScore computes a score based on how many relevant markers are present.
// threshold is the number of markers needed for full score.
func markerScore(lower string, markers []string, threshold int) float64 {
	hits := 0
	for _, m := range markers {
		if strings.Contains(lower, m) {
			hits++
		}
	}
	if hits >= threshold {
		return 1.0
	}
	if hits > 0 {
		return 0.5 + 0.5*float64(hits)/float64(threshold)
	}
	return 0.2 // Some credit for responding at all.
}

// scoreStructure evaluates response coherence: sentence structure, absence
// of degenerate output (repetition, gibberish).
func scoreStructure(lower string, length int) float64 {
	score := 0.5 // Base score for any non-empty response.

	// Bonus: contains sentence-ending punctuation (coherent prose).
	if strings.ContainsAny(lower, ".!?") {
		score += 0.2
	}

	// Bonus: contains multiple sentences (structured response).
	sentences := strings.Count(lower, ".") + strings.Count(lower, "!") + strings.Count(lower, "?")
	if sentences >= 2 {
		score += 0.15
	}

	// Penalty: excessive repetition (degenerate output).
	if length > 100 {
		// Check if any 20-char substring repeats 3+ times.
		chunk := lower
		if len(chunk) > 500 {
			chunk = chunk[:500]
		}
		for i := 0; i+20 < len(chunk); i += 20 {
			sub := chunk[i : i+20]
			if strings.Count(chunk, sub) >= 3 {
				score -= 0.3
				break
			}
		}
	}

	// Penalty: refusal to answer (common with smaller models).
	refusals := []string{"i cannot", "i can't", "i'm unable", "i don't have the ability",
		"as an ai", "as a language model", "i'm not able"}
	for _, r := range refusals {
		if strings.Contains(lower, r) {
			score -= 0.2
			break
		}
	}

	if score < 0 {
		score = 0
	}
	if score > 1 {
		score = 1
	}
	return score
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
