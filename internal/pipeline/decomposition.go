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
	Decision    DecompositionDecision
	Subtasks    []string // for delegation
	Rationale   string
	Specialists []SpecialistProposal // for specialist proposals (Wave 8, #82)
}

// SpecialistProposal recommends creating a specialist agent for a subtask (Wave 8, #82).
type SpecialistProposal struct {
	Name            string  // Proposed specialist name (e.g., "security-auditor")
	Query           string  // The subtask query for this specialist
	EstimatedTokens int     // Estimated token cost for this subtask
	UtilityMargin   float64 // Expected utility margin from utilityMargin()
}

// utilityMargin computes the expected utility of delegating to a specialist (Wave 8, #83).
// Formula: (complexity * 0.5) + (subtasks-1) * 0.12 + fit_ratio * 0.45 - (0.25 + subtasks * 0.04)
// Returns a value where positive means delegation is worthwhile.
func utilityMargin(complexity float64, subtasks int, fitRatio float64) float64 {
	return (complexity * 0.5) +
		float64(subtasks-1)*0.12 +
		fitRatio*0.45 -
		(0.25 + float64(subtasks)*0.04)
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
				query := content[:min(100, len(content))]
				margin := utilityMargin(0.8, 1, 0.7)
				return DecompositionResult{
					Decision:  DecompSpecialistProposal,
					Subtasks:  []string{domain + ": " + query},
					Rationale: "complex domain-specific request; specialist recommended",
					Specialists: []SpecialistProposal{{
						Name:            domain + "-specialist",
						Query:           query,
						EstimatedTokens: 4096,
						UtilityMargin:   margin,
					}},
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
