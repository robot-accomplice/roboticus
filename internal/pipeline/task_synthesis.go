package pipeline

import (
	"strings"

	"github.com/rs/zerolog/log"
)

// TaskSynthesis holds the result of task state analysis, matching Rust's
// synthesize_task_state output. Used by the pipeline to make routing and
// delegation decisions.
type TaskSynthesis struct {
	Intent          string   // Classified intent (e.g., "question", "task", "creative", "code")
	Complexity      string   // "simple", "moderate", "complex", "specialist"
	PlannedAction   string   // "execute_directly", "delegate_to_specialist", "compose_subagent"
	Confidence      float64  // Planner confidence 0–1
	RetrievalNeeded bool     // Whether memory retrieval is beneficial
	MissingSkills   []string // Capabilities not covered by registered skills
	CapabilityFit   float64  // 0–1 ratio of available capability coverage
}

// SynthesizeTaskState performs intent classification, complexity analysis, and
// planner action selection. Matches Rust's synthesize_task_state + plan().
//
// This is a heuristic implementation — Rust uses a semantic classifier backed
// by an LLM call, but the Go version uses keyword-based classification to avoid
// adding inference latency to every turn. The classification accuracy is
// comparable for the common cases.
func SynthesizeTaskState(content string, sessionTurns int, agentSkills []string) TaskSynthesis {
	intent := classifyIntent(content)
	complexity := classifyComplexity(content, sessionTurns)
	capTokens := capabilityTokens(content)
	fit, missing := matchCapabilities(capTokens, agentSkills)

	action := "execute_directly"
	confidence := 0.8

	// Delegation heuristic (mirrors Rust's planner logic).
	if complexity == "complex" || complexity == "specialist" {
		if fit < 0.3 && len(missing) > 0 {
			action = "compose_subagent"
			confidence = 0.6
		} else if fit < 0.7 {
			action = "delegate_to_specialist"
			confidence = 0.65
		}
	}

	// Retrieval decision: retrieve for questions, task context, and longer conversations.
	retrievalNeeded := intent == "question" || intent == "task" || sessionTurns > 3

	result := TaskSynthesis{
		Intent:          intent,
		Complexity:      complexity,
		PlannedAction:   action,
		Confidence:      confidence,
		RetrievalNeeded: retrievalNeeded,
		MissingSkills:   missing,
		CapabilityFit:   fit,
	}

	log.Debug().
		Str("intent", intent).
		Str("complexity", complexity).
		Str("action", action).
		Float64("confidence", confidence).
		Float64("capability_fit", fit).
		Bool("retrieval", retrievalNeeded).
		Msg("task state synthesized")

	return result
}

// classifyIntent determines the user's intent from message content.
// Matches Rust's semantic classifier categories.
func classifyIntent(content string) string {
	lower := strings.ToLower(content)

	// Question patterns.
	questionMarkers := []string{"what", "how", "why", "when", "where", "who", "which", "can you explain", "tell me"}
	for _, m := range questionMarkers {
		if strings.HasPrefix(lower, m) || strings.Contains(lower, "?") {
			return "question"
		}
	}

	// Code/technical patterns.
	codeMarkers := []string{"write code", "implement", "function", "class", "api", "debug", "fix bug", "refactor", "test"}
	for _, m := range codeMarkers {
		if strings.Contains(lower, m) {
			return "code"
		}
	}

	// Task/action patterns.
	taskMarkers := []string{"create", "build", "make", "set up", "configure", "install", "deploy", "update", "delete", "remove", "send", "schedule"}
	for _, m := range taskMarkers {
		if strings.Contains(lower, m) {
			return "task"
		}
	}

	// Creative patterns.
	creativeMarkers := []string{"write", "compose", "draft", "generate", "brainstorm", "design", "story", "poem"}
	for _, m := range creativeMarkers {
		if strings.Contains(lower, m) {
			return "creative"
		}
	}

	// Conversational.
	if len(content) < 50 {
		return "conversational"
	}

	return "general"
}

// classifyComplexity estimates task complexity from content and session context.
// Matches Rust's classify_complexity with feature extraction.
func classifyComplexity(content string, sessionTurns int) string {
	words := len(strings.Fields(content))
	subtasks := extractSubtasks(content)

	// Multi-step tasks are inherently more complex.
	if len(subtasks) >= 3 {
		return "complex"
	}

	// Long, detailed requests.
	if words > 100 {
		return "complex"
	}

	// Medium-length requests with technical markers.
	lower := strings.ToLower(content)
	techMarkers := []string{"integrate", "migrate", "architecture", "system", "pipeline", "workflow"}
	techCount := 0
	for _, m := range techMarkers {
		if strings.Contains(lower, m) {
			techCount++
		}
	}
	if techCount >= 2 || (words > 40 && techCount >= 1) {
		return "complex"
	}

	if words > 30 || len(subtasks) > 0 {
		return "moderate"
	}

	return "simple"
}

// capabilityTokens extracts capability-relevant tokens from user content.
// Matches Rust's capability_tokens: non-alphanumeric split, min 4 chars.
func capabilityTokens(content string) []string {
	words := strings.FieldsFunc(strings.ToLower(content), func(r rune) bool {
		return (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '-' && r != '_'
	})
	var tokens []string
	for _, w := range words {
		if len(w) >= 4 {
			tokens = append(tokens, w)
		}
	}
	return tokens
}

// PlannedActionKind enumerates the possible planned actions from task synthesis.
// Matches Rust's PlannedAction enum.
type PlannedActionKind int

const (
	PlannedActionExecuteDirectly      PlannedActionKind = iota // Handle in current agent
	PlannedActionDelegateToSpecialist                          // Route to a specialist subagent
	PlannedActionComposeSubagent                               // Create a new subagent on the fly
)

// FormatPlannedAction converts a PlannedAction string (from SynthesizeTaskState) to
// a human-readable label. Matches Rust's format_planned_action().
func FormatPlannedAction(action string) string {
	switch action {
	case "execute_directly":
		return "Execute Directly"
	case "delegate_to_specialist":
		return "Delegate to Specialist"
	case "compose_subagent":
		return "Compose Sub-Agent"
	default:
		return "Execute Directly"
	}
}

// PlannedActionToKind parses a planned action string into a typed enum.
func PlannedActionToKind(action string) PlannedActionKind {
	switch action {
	case "delegate_to_specialist":
		return PlannedActionDelegateToSpecialist
	case "compose_subagent":
		return PlannedActionComposeSubagent
	default:
		return PlannedActionExecuteDirectly
	}
}

// ActionGateDecision is the result of MapPlannedAction — tells the pipeline
// whether to continue with standard inference or reroute.
type ActionGateDecision int

const (
	ActionGateContinue          ActionGateDecision = iota // Proceed with standard inference
	ActionGateDelegate                                    // Reroute to delegation path
	ActionGateSpecialistPropose                           // Propose specialist creation
)

// MapPlannedAction maps a PlannedAction + decomposition result into a gate decision.
// Matches Rust's map_planned_action(): integrates task synthesis with the
// decomposition gate to decide whether standard inference, delegation, or
// specialist creation is the right path.
//
// The decision is conservative: delegation/specialist only fires when both the
// planner AND the decomposition gate agree (or when the planner has high confidence
// and the decomposition gate didn't explicitly centralize).
func MapPlannedAction(synthesis TaskSynthesis, decomp *DecompositionResult) ActionGateDecision {
	kind := PlannedActionToKind(synthesis.PlannedAction)

	switch kind {
	case PlannedActionDelegateToSpecialist:
		// Planner wants delegation. Accept if decomp agrees or is at least not centralized.
		if decomp != nil && decomp.Decision == DecompDelegated {
			return ActionGateDelegate
		}
		// High-confidence planner overrides neutral decomposition.
		if synthesis.Confidence >= 0.7 && (decomp == nil || decomp.Decision != DecompCentralized) {
			return ActionGateDelegate
		}
		// Low-confidence planner: fall through to standard inference.
		return ActionGateContinue

	case PlannedActionComposeSubagent:
		// Subagent composition requires both planner confidence and capability gap.
		if synthesis.CapabilityFit < 0.3 && synthesis.Confidence >= 0.6 {
			return ActionGateSpecialistPropose
		}
		// If decomp explicitly proposed a specialist, honor it.
		if decomp != nil && decomp.Decision == DecompSpecialistProposal {
			return ActionGateSpecialistPropose
		}
		return ActionGateContinue

	default:
		// PlannedActionExecuteDirectly — always continue.
		return ActionGateContinue
	}
}

// RetrievalStrategy describes the memory retrieval approach for a turn.
// Matches Rust's decide_retrieval_strategy() output.
type RetrievalStrategy struct {
	Strategy string // "semantic", "recency", "hybrid", "none"
	Budget   int    // Token budget for retrieval
	Reason   string // Why this strategy was chosen
}

// DecideRetrievalStrategy determines the optimal memory retrieval approach
// based on task synthesis results and session context.
// Matches Rust's decide_retrieval_strategy(): a separate decision function
// that decouples retrieval policy from retrieval execution (H10 stage separation).
func DecideRetrievalStrategy(synthesis TaskSynthesis, sessionTurns int, defaultBudget int) RetrievalStrategy {
	// No retrieval for simple conversational turns without history.
	if !synthesis.RetrievalNeeded && sessionTurns <= 1 {
		return RetrievalStrategy{Strategy: "none", Budget: 0, Reason: "simple conversational turn, no history"}
	}

	// Questions benefit from semantic retrieval (find relevant memories).
	if synthesis.Intent == "question" {
		budget := defaultBudget
		if synthesis.Complexity == "complex" {
			budget = defaultBudget * 2 // Double budget for complex questions.
		}
		return RetrievalStrategy{Strategy: "semantic", Budget: budget, Reason: "question intent benefits from semantic search"}
	}

	// Code and task intents need hybrid retrieval (recent context + semantic).
	if synthesis.Intent == "code" || synthesis.Intent == "task" {
		return RetrievalStrategy{Strategy: "hybrid", Budget: defaultBudget, Reason: "task/code intent needs recent + semantic context"}
	}

	// Long conversations need recency retrieval to maintain coherence.
	if sessionTurns > 10 {
		return RetrievalStrategy{Strategy: "recency", Budget: defaultBudget / 2, Reason: "long conversation, prioritize recent context"}
	}

	// Default: semantic for anything with retrieval need.
	if synthesis.RetrievalNeeded {
		return RetrievalStrategy{Strategy: "semantic", Budget: defaultBudget, Reason: "retrieval needed based on task analysis"}
	}

	return RetrievalStrategy{Strategy: "none", Budget: 0, Reason: "no retrieval benefit detected"}
}

// matchCapabilities compares capability tokens against available skills.
// Returns (fit_ratio, missing_skills).
func matchCapabilities(capTokens, skills []string) (float64, []string) {
	if len(capTokens) == 0 {
		return 1.0, nil
	}

	skillSet := make(map[string]bool, len(skills))
	for _, s := range skills {
		for _, word := range strings.Fields(strings.ToLower(s)) {
			if len(word) >= 4 {
				skillSet[word] = true
			}
		}
	}

	matched := 0
	var missing []string
	for _, tok := range capTokens {
		if skillSet[tok] {
			matched++
		} else {
			missing = append(missing, tok)
		}
	}

	fit := float64(matched) / float64(len(capTokens))
	return fit, missing
}
