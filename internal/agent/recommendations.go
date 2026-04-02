package agent

import "strings"

type Recommendation struct {
	Action     string
	Reason     string
	Confidence float64
	Category   string // "memory", "tool", "skill", "escalation"
}

type RecommendationEngine struct {
	minConfidence float64
}

func NewRecommendationEngine(minConfidence float64) *RecommendationEngine {
	return &RecommendationEngine{minConfidence: minConfidence}
}

func (re *RecommendationEngine) Suggest(input string, state *OperatingState, history []string) []Recommendation {
	var recs []Recommendation
	lower := strings.ToLower(input)

	// Memory-related suggestions
	if strings.Contains(lower, "remember") || strings.Contains(lower, "recall") || strings.Contains(lower, "forgot") {
		recs = append(recs, Recommendation{
			Action:     "search_memory",
			Reason:     "User is asking about past information",
			Confidence: 0.8,
			Category:   "memory",
		})
	}

	// Tool suggestions based on content
	if strings.Contains(lower, "file") || strings.Contains(lower, "read") || strings.Contains(lower, "write") {
		recs = append(recs, Recommendation{
			Action:     "use_filesystem_tools",
			Reason:     "User appears to need file operations",
			Confidence: 0.7,
			Category:   "tool",
		})
	}

	if strings.Contains(lower, "search") || strings.Contains(lower, "find") || strings.Contains(lower, "look up") {
		recs = append(recs, Recommendation{
			Action:     "use_web_search",
			Reason:     "User needs information retrieval",
			Confidence: 0.7,
			Category:   "tool",
		})
	}

	// Escalation suggestion
	if state != nil && state.Confidence > 0 && state.Confidence < 0.4 && state.CanEscalate {
		recs = append(recs, Recommendation{
			Action:     "escalate_model",
			Reason:     "Low confidence on current tier",
			Confidence: 0.9,
			Category:   "escalation",
		})
	}

	// Filter by minimum confidence
	var filtered []Recommendation
	for _, r := range recs {
		if r.Confidence >= re.minConfidence {
			filtered = append(filtered, r)
		}
	}
	return filtered
}
