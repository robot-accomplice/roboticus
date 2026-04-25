package llm

import (
	"bytes"
	"context"
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"go/types"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// IntentClass categorizes exercise prompts for per-dimension quality tracking.
// Uses int enum — no string comparison bugs possible.
type IntentClass int

// DefaultExercisePassQualityFloor is the default quality threshold for an
// evaluable benchmark row to count as passed. Operators may later expose a
// configurable tolerance, but the runtime default must not call sub-.50 output
// successful.
const DefaultExercisePassQualityFloor = 0.50

const (
	IntentExecution IntentClass = iota
	IntentDelegation
	IntentIntrospection
	IntentConversation
	IntentMemoryRecall
	IntentToolUse
	// IntentCoding (v1.0.6+) — code generation / review / debugging /
	// design. Added when TurboQuant-class models joined the baseline
	// pool: the prior matrix had only 2 prompts involving code (both
	// buried inside IntentExecution), so a model whose pitch is
	// "better at coding" could not be differentiated from its peers.
	// With CODING as a first-class intent, the metascore router gets
	// a coding-quality dimension to route on.
	IntentCoding
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
	case IntentCoding:
		return "CODING"
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
	case "CODING":
		return IntentCoding
	default:
		return IntentExecution
	}
}

// ParseIntentClassStrict converts a string label to an IntentClass and rejects
// unknown values instead of silently falling back to EXECUTION.
func ParseIntentClassStrict(s string) (IntentClass, error) {
	intent := ParseIntentClass(s)
	if !IsValidIntentClass(intent) || !strings.EqualFold(intent.String(), strings.TrimSpace(s)) {
		return IntentExecution, fmt.Errorf("unknown intent class %q", s)
	}
	return intent, nil
}

// AllIntentClasses returns all defined intent classes in order.
func AllIntentClasses() []IntentClass {
	return []IntentClass{
		IntentExecution, IntentDelegation, IntentIntrospection,
		IntentConversation, IntentMemoryRecall, IntentToolUse,
		IntentCoding,
	}
}

// IsValidIntentClass reports whether intent is part of the canonical exercise
// taxonomy.
func IsValidIntentClass(intent IntentClass) bool {
	for _, candidate := range AllIntentClasses() {
		if candidate == intent {
			return true
		}
	}
	return false
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

func ParseComplexityLevel(raw string) (ComplexityLevel, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "trivial":
		return ComplexityTrivial, nil
	case "simple":
		return ComplexitySimple, nil
	case "moderate":
		return ComplexityModerate, nil
	case "complex":
		return ComplexityComplex, nil
	case "expert":
		return ComplexityExpert, nil
	default:
		return ComplexityTrivial, fmt.Errorf("unknown complexity level %q", raw)
	}
}

// ParseExerciseRowSelector parses a canonical benchmark row selector such as
// TOOL_USE:C2. The Cn suffix maps directly to the benchmark complexity row.
func ParseExerciseRowSelector(raw string) (IntentClass, ComplexityLevel, error) {
	selector := strings.ToUpper(strings.TrimSpace(raw))
	parts := strings.Split(selector, ":")
	if len(parts) != 2 {
		return IntentExecution, ComplexityTrivial, fmt.Errorf("invalid exercise row selector %q (want INTENT:Cn)", raw)
	}
	intent, err := ParseIntentClassStrict(parts[0])
	if err != nil {
		return IntentExecution, ComplexityTrivial, err
	}
	level := strings.TrimSpace(parts[1])
	if !strings.HasPrefix(level, "C") || len(level) < 2 {
		return IntentExecution, ComplexityTrivial, fmt.Errorf("invalid exercise row selector %q (want INTENT:Cn)", raw)
	}
	idx, err := strconv.Atoi(strings.TrimPrefix(level, "C"))
	if err != nil || idx < int(ComplexityTrivial) || idx > int(ComplexityExpert) {
		return IntentExecution, ComplexityTrivial, fmt.Errorf("invalid exercise row selector %q (complexity must be C0-C4)", raw)
	}
	return intent, ComplexityLevel(idx), nil
}

// ExercisePrompt is a single synthetic prompt in the exercise matrix.
type ExercisePrompt struct {
	Prompt          string
	Intent          IntentClass
	Complexity      ComplexityLevel
	ScoringContract ExerciseScoringContract
}

type ExerciseScoringMode string

const (
	ScoringModeGeneric        ExerciseScoringMode = ""
	ScoringModeDirectAnswer   ExerciseScoringMode = "direct_answer"
	ScoringModeConversational ExerciseScoringMode = "conversational"
	ScoringModeDirectFact     ExerciseScoringMode = "direct_fact"
)

type ExerciseConcisionPolicy string

const (
	ConcisionNeutral ExerciseConcisionPolicy = ""
	ConcisionPrefer  ExerciseConcisionPolicy = "prefer"
)

type ExerciseToolExpectation string

const (
	ToolExpectationOptional        ExerciseToolExpectation = ""
	ToolExpectationRequired        ExerciseToolExpectation = "required"
	ToolExpectationContraindicated ExerciseToolExpectation = "contraindicated"
)

type ExerciseArtifactExpectation string

const (
	ArtifactExpectationNone         ExerciseArtifactExpectation = ""
	ArtifactExpectationCodeRequired ExerciseArtifactExpectation = "code_required"
)

type ExerciseArtifactEvaluator string

const (
	ArtifactEvaluatorNone          ExerciseArtifactEvaluator = ""
	ArtifactEvaluatorReverseString ExerciseArtifactEvaluator = "reverse_string"
)

// ExerciseScoringContract carries prompt-specific grading expectations without
// forcing exact wording. It exists to keep concise contract-satisfying answers
// from being penalized by generic verbosity or keyword heuristics.
type ExerciseScoringContract struct {
	Mode                  ExerciseScoringMode
	Concision             ExerciseConcisionPolicy
	ToolExpectation       ExerciseToolExpectation
	ArtifactExpectation   ExerciseArtifactExpectation
	ArtifactLanguageHint  string
	ArtifactEvaluator     ExerciseArtifactEvaluator
	SemanticHints         []string
	SemanticHintThreshold int
}

// ExerciseMatrix contains 35 synthetic prompts: 5 complexity levels × 7
// intent classes. Extends the Rust exercise::EXERCISE_MATRIX with
// IntentMemoryRecall, IntentToolUse, and IntentCoding (beyond-parity).
// The IntentCoding column was added in v1.0.6 when TurboQuant-class
// coding-tuned models joined the baseline pool — see the IntentCoding
// enum docstring for rationale.
var ExerciseMatrix = []ExercisePrompt{
	// ── Trivial (complexity ~0.1) ──────────────────────────────
	{Prompt: "What time is it?", Intent: IntentExecution, Complexity: ComplexityTrivial, ScoringContract: ExerciseScoringContract{
		Mode:                  ScoringModeDirectAnswer,
		Concision:             ConcisionPrefer,
		ToolExpectation:       ToolExpectationContraindicated,
		SemanticHints:         []string{":", "am", "pm", "utc"},
		SemanticHintThreshold: 1,
	}},
	{Prompt: "Say hello.", Intent: IntentDelegation, Complexity: ComplexityTrivial, ScoringContract: ExerciseScoringContract{
		Mode:                  ScoringModeConversational,
		Concision:             ConcisionPrefer,
		ToolExpectation:       ToolExpectationContraindicated,
		SemanticHints:         []string{"hello", "hi", "hey", "greetings"},
		SemanticHintThreshold: 1,
	}},
	{Prompt: "When should you refuse a request instead of attempting it?", Intent: IntentIntrospection, Complexity: ComplexityTrivial},
	{Prompt: "Thanks!", Intent: IntentConversation, Complexity: ComplexityTrivial, ScoringContract: ExerciseScoringContract{
		Mode:                  ScoringModeConversational,
		Concision:             ConcisionPrefer,
		ToolExpectation:       ToolExpectationContraindicated,
		SemanticHints:         []string{"welcome", "glad", "happy", "anytime", "no problem"},
		SemanticHintThreshold: 1,
	}},
	{Prompt: "Do you have any memories stored?", Intent: IntentMemoryRecall, Complexity: ComplexityTrivial},
	{Prompt: "What is 2 + 2?", Intent: IntentToolUse, Complexity: ComplexityTrivial, ScoringContract: ExerciseScoringContract{
		Mode:                  ScoringModeDirectFact,
		Concision:             ConcisionPrefer,
		ToolExpectation:       ToolExpectationContraindicated,
		SemanticHints:         []string{"4", "four"},
		SemanticHintThreshold: 1,
	}}, // Should NOT use tools — pure reasoning
	{Prompt: "In Go, what does `len(slice)` return when the slice is nil?", Intent: IntentCoding, Complexity: ComplexityTrivial, ScoringContract: ExerciseScoringContract{
		Mode:                  ScoringModeDirectFact,
		Concision:             ConcisionPrefer,
		ToolExpectation:       ToolExpectationContraindicated,
		SemanticHints:         []string{"0", "zero"},
		SemanticHintThreshold: 1,
	}},

	// ── Simple (complexity ~0.3) ───────────────────────────────
	{Prompt: "List the files in the workspace directory.", Intent: IntentExecution, Complexity: ComplexitySimple},
	{Prompt: "Check the health of all integrations.", Intent: IntentDelegation, Complexity: ComplexitySimple},
	{Prompt: "What are your biggest limitations when handling complex multi-step tasks?", Intent: IntentIntrospection, Complexity: ComplexitySimple},
	{Prompt: "Explain what you can do in one sentence.", Intent: IntentConversation, Complexity: ComplexitySimple},
	{Prompt: "What do you remember about our last conversation?", Intent: IntentMemoryRecall, Complexity: ComplexitySimple},
	{Prompt: "Show me the contents of the README file.", Intent: IntentToolUse, Complexity: ComplexitySimple, ScoringContract: ExerciseScoringContract{
		ToolExpectation: ToolExpectationRequired,
	}}, // Should use read_file
	{Prompt: "Write a function in any language that reverses a string in-place and explain one edge case to watch for.", Intent: IntentCoding, Complexity: ComplexitySimple, ScoringContract: ExerciseScoringContract{
		ArtifactExpectation: ArtifactExpectationCodeRequired,
		ArtifactEvaluator:   ArtifactEvaluatorReverseString,
	}},

	// ── Moderate (complexity ~0.5) ─────────────────────────────
	{Prompt: "Read the main configuration file and summarize the model settings.", Intent: IntentExecution, Complexity: ComplexityModerate},
	{Prompt: "Search the workspace for any TODO comments and list them.", Intent: IntentDelegation, Complexity: ComplexityModerate},
	{Prompt: "How would you know if you were giving a wrong answer, and what would you do about it?", Intent: IntentIntrospection, Complexity: ComplexityModerate},
	{Prompt: "Compare the advantages of local models versus cloud models for my use case.", Intent: IntentConversation, Complexity: ComplexityModerate},
	{Prompt: "Search your memories for anything about the deployment project. What can you find?", Intent: IntentMemoryRecall, Complexity: ComplexityModerate},
	{Prompt: "Look up how many sessions were created today by querying the database.", Intent: IntentToolUse, Complexity: ComplexityModerate, ScoringContract: ExerciseScoringContract{
		ToolExpectation: ToolExpectationRequired,
	}}, // Should use query_table
	{Prompt: "Given the Go function `func Increment(x *int) { *x++ }`, what goes wrong if x is nil and how should it be fixed?", Intent: IntentCoding, Complexity: ComplexityModerate},

	// ── Complex (complexity ~0.7) ──────────────────────────────
	{Prompt: "Write a shell script that checks disk usage and alerts if any partition is over 90%.", Intent: IntentExecution, Complexity: ComplexityComplex},
	{Prompt: "Create a scheduled task that runs a health check every hour and stores the results.", Intent: IntentDelegation, Complexity: ComplexityComplex},
	{Prompt: "Analyze your recent performance across different task types and suggest which model would handle each best.", Intent: IntentIntrospection, Complexity: ComplexityComplex},
	{Prompt: "Explain the trade-offs between consistency and availability in distributed systems, with examples relevant to my setup.", Intent: IntentConversation, Complexity: ComplexityComplex},
	{Prompt: "I told you something important about palm a few months ago. Use your search_memories tool to look it up and tell me what you find.", Intent: IntentMemoryRecall, Complexity: ComplexityComplex},
	{Prompt: "Find all files in the workspace that were modified in the last 24 hours, read the 3 most recent, and summarize the changes.", Intent: IntentToolUse, Complexity: ComplexityComplex, ScoringContract: ExerciseScoringContract{
		ToolExpectation: ToolExpectationRequired,
	}}, // Should chain bash + read_file
	{Prompt: "Review this Go method for race conditions: `func (c *Cache) Get(k string) string { v := c.data[k]; c.hits++; return v }`. Describe the hazard and propose a concrete fix.", Intent: IntentCoding, Complexity: ComplexityComplex},

	// ── Expert (complexity ~0.9) ───────────────────────────────
	{Prompt: "Refactor the configuration parser to support hot-reload with validation, rollback on failure, and emit structured change events.", Intent: IntentExecution, Complexity: ComplexityExpert},
	{Prompt: "Orchestrate a multi-step workflow: scan all connected services for vulnerabilities, prioritize findings by severity, and generate a remediation plan with estimated effort.", Intent: IntentDelegation, Complexity: ComplexityExpert},
	{Prompt: "Evaluate your own decision-making process over the last 50 turns: where did you make correct tool choices, where did you waste tokens on unnecessary actions, and what patterns should the routing system learn from?", Intent: IntentIntrospection, Complexity: ComplexityExpert},
	{Prompt: "Design a capability-based security model for a multi-tenant agent platform where each tenant has different trust levels, tool access policies, and cost budgets, considering both the authorization and audit requirements.", Intent: IntentConversation, Complexity: ComplexityExpert},
	{Prompt: "Cross-reference your episodic and semantic memories about the infrastructure migration project, then search for any related relationship data about the people involved. Compile a timeline of what happened, who was involved, and what decisions were made.", Intent: IntentMemoryRecall, Complexity: ComplexityExpert},
	{Prompt: "Query the sessions database for the last 10 conversations, analyze the tool call patterns in each, cross-reference with the inference costs table to calculate per-tool cost efficiency, and write a report to the workspace.", Intent: IntentToolUse, Complexity: ComplexityExpert, ScoringContract: ExerciseScoringContract{
		ToolExpectation: ToolExpectationRequired,
	}}, // Should chain query_table + bash + write_file
	{Prompt: "Design a memory-bounded LRU cache in Go with O(1) Get and Put, eviction callbacks, and safe concurrent access. Describe the data structures you'd use and write the core Put method.", Intent: IntentCoding, Complexity: ComplexityExpert, ScoringContract: ExerciseScoringContract{
		ArtifactExpectation:  ArtifactExpectationCodeRequired,
		ArtifactLanguageHint: "go",
	}},
}

// ResolveExercisePrompt returns the canonical exercise prompt definition when
// the persisted row matches one in the matrix, preserving any scoring contract
// attached to that prompt. If no exact matrix row matches, it falls back to the
// provided fields so historical rescoring still works for custom rows.
func ResolveExercisePrompt(rawPrompt string, intent IntentClass, complexity ComplexityLevel) ExercisePrompt {
	for _, p := range ExerciseMatrix {
		if p.Prompt == rawPrompt && p.Intent == intent && p.Complexity == complexity {
			return p
		}
	}
	return ExercisePrompt{
		Prompt:     rawPrompt,
		Intent:     intent,
		Complexity: complexity,
	}
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

	// ── CODING priors (v1.0.6+) ──────────────────────────────────────
	// Conservative cold-start estimates per model. These are PRIORS,
	// not measurements — the router's per-turn observations overwrite
	// them as real data accrues. Calibrated against each model's
	// publicly-known strength on coding benchmarks (HumanEval,
	// MBPP-style tasks), scaled to the 0-1 range the metascore uses.
	// Unknown local models (including TurboQuant variants) inherit
	// the flat-0.5 default so their first baseline run is genuinely
	// exploratory, not biased by a guessed prior.
	{Model: "openai/gpt-4o-mini", IntentClass: "CODING", Quality: 0.70},
	{Model: "openai/gpt-4o", IntentClass: "CODING", Quality: 0.85},
	{Model: "anthropic/claude-sonnet", IntentClass: "CODING", Quality: 0.90},
	{Model: "ollama/qwen2.5:32b", IntentClass: "CODING", Quality: 0.70},
	{Model: "ollama/qwen3.5:35b-a3b", IntentClass: "CODING", Quality: 0.65},
	{Model: "ollama/gemma3:13b", IntentClass: "CODING", Quality: 0.45},
	{Model: "ollama/mixtral:8x7b", IntentClass: "CODING", Quality: 0.60},
	{Model: "moonshot/kimi-k2-turbo-preview", IntentClass: "CODING", Quality: 0.75},
	{Model: "ollama/gemma4", IntentClass: "CODING", Quality: 0.55},
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
		result.Quality = ScoreExerciseResponse(p, resp.Content)
		result.Passed = result.Quality >= DefaultExercisePassQualityFloor && result.Error == ""
		results = append(results, result)
		if cb != nil {
			cb(i, result)
		}
	}

	return results
}

// ScoreExerciseResponse computes a quality score for an exercise response.
// If a prompt carries an explicit scoring contract, contract satisfaction is
// the primary signal and generic intent/style heuristics are secondary.
// Otherwise the legacy generic heuristic remains the fallback.
func ScoreExerciseResponse(prompt ExercisePrompt, content string) float64 {
	if content == "" {
		return 0.0
	}

	lower := strings.ToLower(content)
	length := len(content)

	// Dimension 2: Intent relevance (0-1) — does the response demonstrate
	// behavior appropriate for the intent class?
	relevanceScore := scoreIntentRelevance(lower, prompt.Intent)

	// Dimension 3: Structural quality (0-1) — coherence markers.
	structureScore := scoreStructure(lower, length)

	score := 0.0
	if hasExplicitScoringContract(prompt.ScoringContract) {
		contractScore := scorePromptContract(prompt, lower, length)
		score = 0.7*contractScore + 0.15*relevanceScore + 0.15*structureScore
	} else {
		// Legacy fallback for prompts that do not yet declare a contract.
		lengthScore := scoreLengthAdequacy(length, prompt.Complexity)
		score = 0.4*lengthScore + 0.3*relevanceScore + 0.3*structureScore
	}

	if prompt.Intent == IntentCoding && prompt.ScoringContract.ArtifactExpectation == ArtifactExpectationCodeRequired {
		artifactScore := scoreCodingArtifact(prompt, content)
		if prompt.ScoringContract.ArtifactEvaluator != ArtifactEvaluatorNone {
			score = 0.85*artifactScore + 0.15*score
		} else {
			score = 0.55*artifactScore + 0.45*score
		}
	}

	if prompt.ScoringContract.ToolExpectation == ToolExpectationRequired && !hasRequiredToolOutcomeEvidence(lower) {
		if hasToolFailureNonCompletion(lower) {
			return minFloat(score, 0.49)
		}
		if hasPromissoryToolIntent(lower) {
			return minFloat(score, 0.35)
		}
		return minFloat(score, 0.49)
	}
	if prompt.ScoringContract.ToolExpectation == ToolExpectationRequired && hasToolFailureNonCompletion(lower) {
		return minFloat(score, 0.49)
	}

	if actionPromptRequiresCompletion(prompt) && hasBlockedActionNonCompletion(lower) {
		if hasDeclaredTaskFailure(lower) || !hasConcreteActionCompletionEvidence(lower) {
			return minFloat(score, 0.49)
		}
	}

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

func hasExplicitScoringContract(c ExerciseScoringContract) bool {
	return c.Mode != "" || c.Concision != "" || c.ToolExpectation != "" ||
		c.ArtifactExpectation != "" || c.ArtifactLanguageHint != "" ||
		c.ArtifactEvaluator != "" || len(c.SemanticHints) > 0
}

func scorePromptContract(prompt ExercisePrompt, lower string, length int) float64 {
	contract := prompt.ScoringContract
	score := 0.4

	if len(contract.SemanticHints) > 0 {
		threshold := contract.SemanticHintThreshold
		if threshold <= 0 {
			threshold = 1
		}
		score += 0.3 * markerScore(lower, contract.SemanticHints, threshold)
	}

	switch contract.Mode {
	case ScoringModeDirectAnswer, ScoringModeDirectFact:
		if shortFormResponse(length, lower) {
			score += 0.15
		}
		if contract.Mode == ScoringModeDirectFact && length <= 32 {
			score += 0.05
		}
	case ScoringModeConversational:
		if shortFormResponse(length, lower) {
			score += 0.1
		}
		if strings.ContainsAny(lower, "!?") {
			score += 0.05
		}
	}

	switch contract.ToolExpectation {
	case ToolExpectationRequired:
		if hasRequiredToolOutcomeEvidence(lower) {
			score += 0.2
		} else if hasPromissoryToolIntent(lower) {
			score -= 0.2
		}
	case ToolExpectationContraindicated:
		if containsAny(lower, []string{
			"read_file", "query_table", "bash", "tool_call", "search_memories",
			"recall_memory", "command output",
		}) {
			score -= 0.25
		} else {
			score += 0.1
		}
	}

	if contract.Concision == ConcisionPrefer {
		switch {
		case length <= preferredConcisionCeiling(prompt.Complexity):
			score += 0.15
		case length <= 2*preferredConcisionCeiling(prompt.Complexity):
			score += 0.05
		default:
			score -= 0.1
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

func hasRequiredToolOutcomeEvidence(lower string) bool {
	if containsAny(lower, []string{
		"observed result", "observed results", "tool output", "command output",
		"query returned", "database returned", "read_file", "query_table",
		"search_memories", "recall_memory", "found ", "found:", "no files",
		"no sessions", "no readme", "not found", "unable to", "could not",
		"couldn't", "failed", "permission denied", "access denied", "blocked",
		"contents:", "modified files", "workspace contains", "the workspace",
		"there were", "there are", "0 sessions", "1 session",
	}) {
		return true
	}
	return regexp.MustCompile(`\b[0-9]+\s+(sessions?|files?|conversations?|rows?|records?)\b`).MatchString(lower)
}

func hasToolFailureNonCompletion(lower string) bool {
	return containsAny(lower, []string{
		"unable to complete", "could not complete", "cannot complete",
		"tool failed", "failed with the error", "failed with error",
		"could not be accessed", "cannot be accessed", "permission denied",
		"access denied", "allowed_paths", "without being able to",
		"prevented me from", "configuration restriction",
	})
}

func hasPromissoryToolIntent(lower string) bool {
	return containsAny(lower, []string{
		"i'll query", "i will query", "i'll check", "i will check",
		"i'll search", "i will search", "i'll look", "i will look",
		"let me query", "let me check", "let me search", "let me look",
		"i can look that up", "i can query", "i can check",
	})
}

func actionPromptRequiresCompletion(prompt ExercisePrompt) bool {
	if prompt.Intent != IntentExecution && prompt.Intent != IntentDelegation {
		return false
	}
	lowerPrompt := strings.ToLower(prompt.Prompt)
	return containsAny(lowerPrompt, []string{
		"create ", "write ", "orchestrate ", "refactor ", "generate ",
		"scan ", "prioritize ", "store ", "emit ", "schedule", "scheduled task",
	})
}

func hasBlockedActionNonCompletion(lower string) bool {
	return containsAny(lower, []string{
		"can't complete", "cannot complete", "could not complete",
		"unable to complete", "i can't write", "i cannot write",
		"i can't execute", "i cannot execute", "i don't have access",
		"i do not have access", "current tool surface only", "only tools available",
		"current tool surface doesn't include", "current tool surface does not include",
		"doesn't include file-write", "does not include file-write",
		"can't directly install", "cannot directly install",
		"tool execution failed", "policy denied", "preventing access",
		"filesystem discovery is blocked", "tools are disabled",
		"cannot inspect", "can't inspect", "instead, i am finalizing",
		"if those tools become available", "if tools become available",
		"would need tools", "need tools that can", "to actually install",
		"to finish this task i need", "i would do", "the task failed",
		"task failed due", "failed due to", "causing the task to fail",
	})
}

func hasDeclaredTaskFailure(lower string) bool {
	return containsAny(lower, []string{
		"the task failed", "task failed due", "failed due to",
		"causing the task to fail",
	})
}

func hasConcreteActionCompletionEvidence(lower string) bool {
	return containsAny(lower, []string{
		"created", "wrote", "stored", "installed", "scheduled", "registered",
		"updated", "refactored", "generated", "saved", "emitted",
		"tool output", "command output", "observed result", "observed results",
		"completed successfully", "successfully created", "successfully wrote",
	})
}

func minFloat(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

func preferredConcisionCeiling(complexity ComplexityLevel) int {
	switch complexity {
	case ComplexityTrivial:
		return 96
	case ComplexitySimple:
		return 160
	default:
		return 240
	}
}

func shortFormResponse(length int, lower string) bool {
	sentences := strings.Count(lower, ".") + strings.Count(lower, "!") + strings.Count(lower, "?")
	return length <= 180 && sentences <= 3
}

func containsAny(lower string, markers []string) bool {
	for _, m := range markers {
		if strings.Contains(lower, m) {
			return true
		}
	}
	return false
}

type extractedCodeArtifact struct {
	Language string
	Code     string
	Fenced   bool
}

var fencedCodeRE = regexp.MustCompile("(?s)```([A-Za-z0-9_+-]*)\\s*\\n(.*?)```")

func extractPrimaryCodeArtifact(content string) extractedCodeArtifact {
	matches := fencedCodeRE.FindAllStringSubmatch(content, -1)
	best := extractedCodeArtifact{}
	for _, match := range matches {
		if len(match) < 3 {
			continue
		}
		code := strings.TrimSpace(match[2])
		if len(code) <= len(best.Code) {
			continue
		}
		best = extractedCodeArtifact{
			Language: strings.ToLower(strings.TrimSpace(match[1])),
			Code:     code,
			Fenced:   true,
		}
	}
	if best.Code != "" {
		return best
	}
	lower := strings.ToLower(content)
	if containsAny(lower, []string{"func ", "def ", "function ", "=>", "class ", "package "}) {
		return extractedCodeArtifact{Code: strings.TrimSpace(content)}
	}
	return extractedCodeArtifact{}
}

func scoreCodingArtifact(prompt ExercisePrompt, content string) float64 {
	artifact := extractPrimaryCodeArtifact(content)
	if artifact.Code == "" {
		return 0.1
	}
	language := strings.TrimSpace(strings.ToLower(artifact.Language))
	if language == "" {
		language = strings.TrimSpace(strings.ToLower(prompt.ScoringContract.ArtifactLanguageHint))
	}
	extractionScore := 0.75
	if artifact.Fenced {
		extractionScore = 1.0
	}
	syntaxScore := scoreCodeSyntax(language, artifact.Code)
	semanticScore := scoreCodeSemantics(prompt.ScoringContract.ArtifactEvaluator, language, artifact.Code)
	score := 0.2*extractionScore + 0.35*syntaxScore + 0.45*semanticScore
	if prompt.ScoringContract.ArtifactEvaluator != ArtifactEvaluatorNone {
		score = 0.15*extractionScore + 0.25*syntaxScore + 0.60*semanticScore
	}
	if score > 1 {
		return 1
	}
	return score
}

func scoreCodeSyntax(language, code string) float64 {
	code = strings.TrimSpace(code)
	if code == "" {
		return 0
	}
	switch language {
	case "go", "golang":
		src := code
		if !strings.Contains(src, "package ") {
			src = "package main\n\n" + src
		}
		if _, err := parser.ParseFile(token.NewFileSet(), "artifact.go", src, parser.AllErrors); err == nil {
			return 1.0
		}
		return 0.2
	default:
		score := 0.35
		if balancedDelimiters(code) {
			score += 0.25
		}
		if containsAny(strings.ToLower(code), []string{"return", "func ", "def ", "function ", "=>", "for ", "if "}) {
			score += 0.2
		}
		if strings.Contains(code, "\n") {
			score += 0.2
		}
		if score > 1 {
			return 1
		}
		return score
	}
}

func scoreCodeSemantics(evaluator ExerciseArtifactEvaluator, language, code string) float64 {
	lower := strings.ToLower(code)
	switch evaluator {
	case ArtifactEvaluatorReverseString:
		switch language {
		case "python":
			executed, passed := evaluatePythonReverseStringIO(code)
			if executed && passed {
				return 1.0
			}
			if executed && !passed {
				return 0.2
			}
			if containsAny(lower, []string{"[::-1]", "reversed(", "join(reversed("}) {
				return 0.75
			}
		case "javascript", "js", "typescript", "ts":
			if containsAny(lower, []string{".split('')", ".split(\"\")", ".reverse()", ".join('')", ".join(\"\")"}) {
				return 1.0
			}
		case "rust":
			if containsAny(lower, []string{"chars().rev().collect()", ".chars()", ".rev()", ".collect()"}) {
				return 1.0
			}
		case "go", "golang":
			return scoreGoReverseStringFunctional(code)
		default:
			if containsAny(lower, []string{"reverse", "rev", "swap"}) {
				return 0.65
			}
		}
		return 0.2
	default:
		if containsAny(lower, []string{"func ", "def ", "function ", "return"}) {
			return 0.7
		}
		return 0.4
	}
}

func scoreGoReverseStringFunctional(code string) float64 {
	src := strings.TrimSpace(code)
	if src == "" {
		return 0
	}
	if !strings.Contains(src, "package ") {
		src = "package main\n\n" + src
	}

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "artifact.go", src, parser.AllErrors)
	if err != nil {
		return 0.1
	}

	executed, passed := evaluateGoReverseStringIO(src)
	if executed && passed {
		return 1.0
	}

	score := 0.0
	if goArtifactTypeChecks(fset, file) {
		score += 0.25
	} else {
		score += 0.05
	}
	if hasStringToStringFunction(file) {
		score += 0.2
	}

	lower := strings.ToLower(src)
	reversalSignals := 0
	if containsAny(lower, []string{"[]rune", "[]byte"}) {
		score += 0.15
		reversalSignals++
	}
	if containsAny(lower, []string{"i < j", "i<j", "len(runes)-1", "len(bytes)-1"}) {
		score += 0.15
		reversalSignals++
	}
	if containsAny(lower, []string{
		"runes[i], runes[j] = runes[j], runes[i]",
		"bytes[i], bytes[j] = bytes[j], bytes[i]",
	}) || hasTupleSwap(file) {
		score += 0.2
		reversalSignals++
	}
	if containsAny(lower, []string{"return string(runes)", "return string(bytes)"}) {
		score += 0.05
	}
	if reversalSignals == 0 && score > 0.35 {
		score = 0.35
	}
	if executed && !passed && score > 0.2 {
		score = 0.2
	}
	if score > 1 {
		return 1
	}
	return score
}

func evaluateGoReverseStringIO(src string) (bool, bool) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "artifact.go", src, parser.AllErrors)
	if err != nil {
		return false, false
	}
	fnName, fnSource, ok := extractGoStringToStringFunction(fset, file)
	if !ok {
		return false, false
	}

	dir, err := os.MkdirTemp("", "roboticus-code-eval-*")
	if err != nil {
		return false, false
	}
	defer func() { _ = os.RemoveAll(dir) }()

	sourcePath := filepath.Join(dir, "artifact_test.go")
	testSource := fmt.Sprintf(`package main

import "testing"

%s

func TestReverseStringContract(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"", ""},
		{"a", "a"},
		{"ab", "ba"},
		{"robot", "tobor"},
		{"hello world", "dlrow olleh"},
	}
	for _, tc := range cases {
		if got := %s(tc.in); got != tc.want {
			t.Fatalf("%s(%%q) = %%q, want %%q", tc.in, got, tc.want)
		}
	}
}
`, fnSource, fnName, fnName)
	if err := os.WriteFile(sourcePath, []byte(testSource), 0o600); err != nil {
		return false, false
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "go", "test", ".")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GO111MODULE=off")
	if err := cmd.Run(); err != nil {
		return true, false
	}
	return true, true
}

func extractGoStringToStringFunction(fset *token.FileSet, file *ast.File) (string, string, bool) {
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Recv != nil || fn.Name == nil || fn.Type == nil || fn.Body == nil {
			continue
		}
		if !goFuncIsStringToString(fn) {
			continue
		}
		var buf bytes.Buffer
		if err := printer.Fprint(&buf, fset, fn); err != nil {
			continue
		}
		return fn.Name.Name, buf.String(), true
	}
	return "", "", false
}

func evaluatePythonReverseStringIO(code string) (bool, bool) {
	fnSource, fnName, callMode, ok := extractPythonSingleArgFunction(code)
	if !ok {
		return false, false
	}
	if containsAny(strings.ToLower(fnSource), []string{
		"import ", "__", "open(", "exec(", "eval(", "compile(",
		"globals(", "locals(", "subprocess", "os.", "sys.",
	}) {
		return false, false
	}

	dir, err := os.MkdirTemp("", "roboticus-python-eval-*")
	if err != nil {
		return false, false
	}
	defer func() { _ = os.RemoveAll(dir) }()

	sourcePath := filepath.Join(dir, "artifact_eval.py")
	var testSource string
	if callMode == "list_in_place" {
		testSource = fmt.Sprintf(`%s

cases = [
    ("", ""),
    ("a", "a"),
    ("ab", "ba"),
    ("robot", "tobor"),
    ("hello world", "dlrow olleh"),
]
for _inp, _want in cases:
    _arg = list(_inp)
    _ret = %s(_arg)
    if _ret is None:
        _got = "".join(_arg)
    elif isinstance(_ret, list):
        _got = "".join(_ret)
    else:
        _got = _ret
    if _got != _want:
        raise SystemExit(f"%s({_inp!r}) = {_got!r}, want {_want!r}")
`, fnSource, fnName, fnName)
	} else {
		testSource = fmt.Sprintf(`%s

cases = [
    ("", ""),
    ("a", "a"),
    ("ab", "ba"),
    ("robot", "tobor"),
    ("hello world", "dlrow olleh"),
]
for _inp, _want in cases:
    _got = %s(_inp)
    if _got != _want:
        raise SystemExit(f"%s({_inp!r}) = {_got!r}, want {_want!r}")
`, fnSource, fnName, fnName)
	}
	if err := os.WriteFile(sourcePath, []byte(testSource), 0o600); err != nil {
		return false, false
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "python3", sourcePath)
	cmd.Dir = dir
	if err := cmd.Run(); err != nil {
		return true, false
	}
	return true, true
}

func extractPythonSingleArgFunction(code string) (string, string, string, bool) {
	lines := strings.Split(strings.TrimSpace(code), "\n")
	start := -1
	name := ""
	callMode := "string_return"
	re := regexp.MustCompile(`^def\s+([A-Za-z_][A-Za-z0-9_]*)\s*\(\s*[A-Za-z_][A-Za-z0-9_]*(?:\s*:\s*[^)]*)?\s*\)\s*(?:->\s*[^:]+)?\s*:`)
	for i, line := range lines {
		match := re.FindStringSubmatch(strings.TrimSpace(line))
		if len(match) == 2 {
			start = i
			name = match[1]
			defLower := strings.ToLower(line)
			if strings.Contains(defLower, "list") || strings.Contains(defLower, "none") {
				callMode = "list_in_place"
			}
			break
		}
	}
	if start < 0 {
		return "", "", "", false
	}

	block := []string{strings.TrimRight(lines[start], " \t")}
	for _, line := range lines[start+1:] {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			block = append(block, "")
			continue
		}
		if !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "\t") {
			break
		}
		block = append(block, strings.TrimRight(line, " \t"))
	}
	if len(block) < 2 {
		return "", "", "", false
	}
	return strings.Join(block, "\n"), name, callMode, true
}

func goArtifactTypeChecks(fset *token.FileSet, file *ast.File) bool {
	var typeErr error
	conf := types.Config{
		Error: func(err error) {
			if typeErr == nil {
				typeErr = err
			}
		},
	}
	_, err := conf.Check("artifact", fset, []*ast.File{file}, nil)
	return err == nil && typeErr == nil
}

func hasStringToStringFunction(file *ast.File) bool {
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if ok && goFuncIsStringToString(fn) {
			return true
		}
	}
	return false
}

func goFuncIsStringToString(fn *ast.FuncDecl) bool {
	if fn == nil || fn.Type == nil || fn.Type.Params == nil || fn.Type.Results == nil {
		return false
	}
	if len(fn.Type.Params.List) != 1 || len(fn.Type.Results.List) != 1 {
		return false
	}
	param, ok := fn.Type.Params.List[0].Type.(*ast.Ident)
	if !ok || param.Name != "string" {
		return false
	}
	result, ok := fn.Type.Results.List[0].Type.(*ast.Ident)
	return ok && result.Name == "string"
}

func hasTupleSwap(file *ast.File) bool {
	found := false
	ast.Inspect(file, func(node ast.Node) bool {
		assign, ok := node.(*ast.AssignStmt)
		if !ok || len(assign.Lhs) != 2 || len(assign.Rhs) != 2 {
			return true
		}
		leftA, leftAOK := assign.Lhs[0].(*ast.IndexExpr)
		leftB, leftBOK := assign.Lhs[1].(*ast.IndexExpr)
		rightA, rightAOK := assign.Rhs[0].(*ast.IndexExpr)
		rightB, rightBOK := assign.Rhs[1].(*ast.IndexExpr)
		if !leftAOK || !leftBOK || !rightAOK || !rightBOK {
			return true
		}
		if astExprString(leftA.X) == astExprString(rightB.X) &&
			astExprString(leftB.X) == astExprString(rightA.X) &&
			astExprString(leftA.Index) == astExprString(rightB.Index) &&
			astExprString(leftB.Index) == astExprString(rightA.Index) {
			found = true
			return false
		}
		return true
	})
	return found
}

func astExprString(expr ast.Expr) string {
	var buf bytes.Buffer
	_ = printer.Fprint(&buf, token.NewFileSet(), expr)
	return buf.String()
}

func balancedDelimiters(code string) bool {
	pairs := map[rune]rune{')': '(', ']': '[', '}': '{'}
	stack := make([]rune, 0, len(code))
	for _, ch := range code {
		switch ch {
		case '(', '[', '{':
			stack = append(stack, ch)
		case ')', ']', '}':
			if len(stack) == 0 || stack[len(stack)-1] != pairs[ch] {
				return false
			}
			stack = stack[:len(stack)-1]
		}
	}
	return len(stack) == 0
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

	case IntentCoding:
		// Coding responses should demonstrate real code understanding — not
		// just English prose about programming. Scoring layers:
		//   (a) Syntactic-code markers (code fences, parens, braces,
		//       language keywords) prove the model actually produced or
		//       referenced code, not a prose description.
		//   (b) Code-concept markers (pointer, nil, slice, map, struct,
		//       interface, function) prove the model is in "code mode"
		//       vs. generic "explain a concept" mode.
		//   (c) Quality markers (race, deadlock, mutex, goroutine,
		//       O(1), O(n), idempotent, rollback, edge case) prove the
		//       model is reasoning about correctness, concurrency, and
		//       complexity — not just producing code-shaped text.
		// Heuristic-based scoring can't fully evaluate code correctness
		// (that would require compilation + test execution), but the
		// keyword layers catch the broad quality signal that separates
		// coding-tuned models from generalist models.
		score := 0.0

		// Layer (a): syntactic-code markers. Check for code-fence
		// presence as a raw substring (not lowered) since the lower
		// version is what markerScore sees.
		codeShapeMarkers := []string{"```", "func ", "function ", "def ", "fn ",
			"return", "{", "}", "()", "=>"}
		score += 0.35 * markerScore(lower, codeShapeMarkers, 2)

		// Layer (b): code-concept markers. These words virtually never
		// appear in non-code prose.
		codeConceptMarkers := []string{"nil", "null", "pointer", "slice", "array",
			"map", "struct", "interface", "method", "variable", "argument",
			"parameter", "loop", "iterate", "closure", "string",
			"boolean", "recursion", "callback", "lambda"}
		score += 0.35 * markerScore(lower, codeConceptMarkers, 3)

		// Layer (c): quality-reasoning markers. A response that surfaces
		// race conditions, complexity bounds, or edge cases is doing
		// the kind of thinking coding-tuned models are supposed to do.
		qualityMarkers := []string{"race", "deadlock", "mutex", "lock",
			"goroutine", "thread", "concurrent", "atomic", "o(1)", "o(n)",
			"idempotent", "rollback", "edge case", "nil pointer",
			"off-by-one", "overflow", "underflow", "leak", "safe",
			"validate", "invariant", "complexity", "panic"}
		score += 0.30 * markerScore(lower, qualityMarkers, 2)

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
		key := canonicalIntentClassKey(b.Model, b.IntentClass)
		if key.Model == "" || key.IntentClass == "" {
			continue
		}
		iq.mu.RLock()
		rb, exists := iq.intents[key]
		hasData := exists && rb.count > 0
		iq.mu.RUnlock()

		if hasData {
			continue
		}
		quality := b.Quality
		if quality < 0 {
			quality = 0
		}
		if quality > 1 {
			quality = 1
		}
		iq.mu.Lock()
		iq.priors[key] = quality
		iq.mu.Unlock()
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
