package pipeline

import (
	"strings"
	"unicode"
)

// DecompositionDecision represents the outcome of the decomposition gate.
type DecompositionDecision int

const (
	DecompCentralized        DecompositionDecision = iota // Single agent handles it
	DecompDelegated                                       // Multi-agent delegation
	DecompSpecialistProposal                              // Propose specialist creation
)

// String returns the decision name.
func (d DecompositionDecision) String() string {
	switch d {
	case DecompCentralized:
		return "centralized"
	case DecompDelegated:
		return "delegated"
	case DecompSpecialistProposal:
		return "specialist_proposal"
	default:
		return "unknown"
	}
}

// DecompositionResult holds the gate evaluation result.
type DecompositionResult struct {
	Decision  DecompositionDecision
	Subtasks  []string // for delegation
	Rationale string
}

// EvaluateDecomposition decides whether a request needs multi-agent delegation.
// Uses heuristics to detect multi-part requests that could benefit from parallel execution.
func EvaluateDecomposition(content string, sessionTurns int) DecompositionResult {
	// Detect numbered lists or bullet points as potential subtasks.
	subtasks := extractSubtasks(content)

	// Long messages with 3+ distinct subtasks may benefit from delegation.
	if len(subtasks) >= 3 && len(content) > 200 {
		return DecompositionResult{
			Decision:  DecompDelegated,
			Subtasks:  subtasks,
			Rationale: "detected multi-part request with independent subtasks",
		}
	}

	// Very complex requests (long content + many prior turns) might need a specialist.
	if len(content) > 1000 && sessionTurns > 10 {
		// Check for domain-specific keywords suggesting specialist need.
		lower := strings.ToLower(content)
		specialists := map[string]string{
			"financial analysis":  "finance",
			"code review":         "code-review",
			"security audit":      "security",
			"data analysis":       "data",
			"system architecture": "architecture",
		}
		for phrase, domain := range specialists {
			if strings.Contains(lower, phrase) {
				return DecompositionResult{
					Decision:  DecompSpecialistProposal,
					Subtasks:  []string{domain + ": " + content[:min(100, len(content))]},
					Rationale: "complex domain-specific request; specialist recommended",
				}
			}
		}
	}

	return DecompositionResult{
		Decision:  DecompCentralized,
		Rationale: "standard single-agent execution",
	}
}

// extractSubtasks identifies numbered or bulleted items in the content.
func extractSubtasks(content string) []string {
	lines := strings.Split(content, "\n")
	var tasks []string

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}

		// Numbered list: "1. ...", "2) ...", etc.
		if len(trimmed) >= 3 && unicode.IsDigit(rune(trimmed[0])) {
			rest := strings.TrimLeft(trimmed, "0123456789")
			if len(rest) > 0 && (rest[0] == '.' || rest[0] == ')') {
				task := strings.TrimSpace(rest[1:])
				if task != "" {
					tasks = append(tasks, task)
				}
				continue
			}
		}

		// Bulleted list: "- ...", "* ...", "• ..."
		if trimmed[0] == '-' || trimmed[0] == '*' {
			task := strings.TrimSpace(trimmed[1:])
			if task != "" {
				tasks = append(tasks, task)
			}
		}
	}
	return tasks
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
