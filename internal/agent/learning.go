package agent

import "strings"

// LearnedPattern captures a recurring behaviour pattern extracted from agent turns.
type LearnedPattern struct {
	ID           string
	Pattern      string // what was learned
	Source       string // which turn/session
	SuccessCount int
	FailureCount int
}

// LearningExtractor extracts and tracks patterns from agent turn content.
type LearningExtractor struct {
	patterns map[string]*LearnedPattern
}

// NewLearningExtractor creates an empty LearningExtractor.
func NewLearningExtractor() *LearningExtractor {
	return &LearningExtractor{patterns: make(map[string]*LearnedPattern)}
}

// ExtractFromTurn analyses a turn and its tool results, returning discovered patterns.
func (le *LearningExtractor) ExtractFromTurn(turnContent string, toolResults []string, success bool) []LearnedPattern {
	var extracted []LearnedPattern
	// Extract tool usage patterns
	for _, result := range toolResults {
		if success && len(result) > 0 {
			pattern := LearnedPattern{
				Pattern:      "successful_tool_use",
				Source:       truncateForLearning(turnContent, 100),
				SuccessCount: 1,
			}
			extracted = append(extracted, pattern)
		}
	}
	// Extract query patterns from content
	if strings.Contains(strings.ToLower(turnContent), "how to") {
		extracted = append(extracted, LearnedPattern{
			Pattern: "procedural_query",
			Source:  truncateForLearning(turnContent, 100),
		})
	}
	return extracted
}

// Register stores a pattern under its ID for outcome tracking.
func (le *LearningExtractor) Register(p LearnedPattern) {
	le.patterns[p.ID] = &p
}

// RecordOutcome increments success or failure counters for a registered pattern.
func (le *LearningExtractor) RecordOutcome(patternID string, success bool) {
	p, ok := le.patterns[patternID]
	if !ok {
		return
	}
	if success {
		p.SuccessCount++
	} else {
		p.FailureCount++
	}
}

// SuccessRate returns the fraction of successful outcomes for a registered pattern.
// Returns 0 if the pattern is unknown or has no recorded outcomes.
func (le *LearningExtractor) SuccessRate(patternID string) float64 {
	p, ok := le.patterns[patternID]
	if !ok {
		return 0
	}
	total := p.SuccessCount + p.FailureCount
	if total == 0 {
		return 0
	}
	return float64(p.SuccessCount) / float64(total)
}

func truncateForLearning(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max]
}
