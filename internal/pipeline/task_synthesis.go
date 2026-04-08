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
