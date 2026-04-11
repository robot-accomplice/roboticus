// Revenue opportunity scoring algorithm.
// Rust parity: crates/roboticus-db/src/revenue_scoring.rs
// Implements 3-component scoring (confidence/effort/risk) with feedback signal adjustment.

package db

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
)

// RevenueScoreInput holds the inputs for scoring a revenue opportunity.
type RevenueScoreInput struct {
	Source              string
	Strategy            string
	PayloadJSON         string
	ExpectedRevenueUSDC float64
	RequestID           string // empty = no request ID
}

// RevenueScore holds the computed score for a revenue opportunity.
type RevenueScore struct {
	ConfidenceScore     float64
	EffortScore         float64
	RiskScore           float64
	PriorityScore       float64
	RecommendedApproved bool
	ScoreReason         string
}

// RevenueFeedbackSignal holds aggregated feedback for a strategy.
type RevenueFeedbackSignal struct {
	FeedbackCount int
	AvgGrade      float64
}

// ScoreRevenueOpportunity computes a priority score without feedback.
// Rust parity: score_revenue_opportunity().
func ScoreRevenueOpportunity(input *RevenueScoreInput) RevenueScore {
	return scoreWithFeedback(input, nil)
}

// ScoreRevenueOpportunityWithFeedback computes a priority score with feedback adjustment.
// Rust parity: score_revenue_opportunity_with_feedback().
func ScoreRevenueOpportunityWithFeedback(input *RevenueScoreInput, signal *RevenueFeedbackSignal) RevenueScore {
	return scoreWithFeedback(input, signal)
}

func scoreWithFeedback(input *RevenueScoreInput, signal *RevenueFeedbackSignal) RevenueScore {
	// Guard against NaN/Inf in revenue input.
	revenue := input.ExpectedRevenueUSDC
	if !math.IsInf(revenue, 0) && !math.IsNaN(revenue) {
		// valid
	} else {
		revenue = 0.0
	}

	// Parse payload JSON for scope markers.
	var payload map[string]any
	payloadParseFailed := false
	if err := json.Unmarshal([]byte(input.PayloadJSON), &payload); err != nil {
		payload = nil
		payloadParseFailed = true
	}

	strategy := strings.ToLower(strings.TrimSpace(input.Strategy))
	source := strings.ToLower(strings.TrimSpace(input.Source))

	// Scope marker detection.
	scopeKeys := []string{"repo", "url", "endpoint", "pair", "source_url", "issue", "title"}
	hasScopeMarker := false
	for _, key := range scopeKeys {
		if _, ok := payload[key]; ok {
			hasScopeMarker = true
			break
		}
	}

	// Multi-repo detection.
	multiRepo := false
	if payload != nil {
		if mr, ok := payload["multi_repo"].(bool); ok && mr {
			multiRepo = true
		}
	}
	if action, ok := payload["action"].(string); ok {
		if strings.Contains(strings.ToLower(action), "multi-repo") {
			multiRepo = true
		}
	}

	// Strategy calibrations (Rust parity: exact values from revenue_scoring.rs:86-109).
	var confidence, effort, risk float64
	switch strategy {
	case "oracle_feed":
		confidence, effort, risk = 0.65, 0.35, 0.20
	case "code_review", "small_audit":
		confidence, effort, risk = 0.70, 0.30, 0.15
	case "content", "content_creation":
		confidence, effort, risk = 0.60, 0.35, 0.20
	case "monitoring", "api_monitoring":
		confidence, effort, risk = 0.75, 0.20, 0.10
	case "micro_bounty":
		confidence, effort, risk = 0.55, 0.40, 0.30
	default:
		confidence, effort, risk = 0.45, 0.50, 0.40
	}

	// Adjustments (Rust parity: exact modifiers from revenue_scoring.rs:111-133).
	if input.RequestID != "" {
		confidence += 0.10
	}
	if hasScopeMarker {
		confidence += 0.10
		effort -= 0.10
	} else {
		confidence -= 0.10
		risk += 0.15
	}
	if strings.Contains(source, "trusted") || strings.Contains(source, "board") || strings.Contains(source, "feed") {
		confidence += 0.05
	}
	if revenue >= 5.0 {
		confidence += 0.05
	}
	if revenue > 500.0 {
		risk += 0.10
	}
	if multiRepo {
		effort += 0.15
		risk += 0.10
	}

	// Feedback signal adjustment (Rust parity: revenue_scoring.rs:134-143).
	feedbackCount := 0
	avgGrade := 0.0
	if signal != nil {
		feedbackCount = signal.FeedbackCount
		avgGrade = signal.AvgGrade
		sampleWeight := math.Min(float64(signal.FeedbackCount)/5.0, 1.0)
		gradeWeight := 0.0
		if math.IsInf(signal.AvgGrade, 0) || math.IsNaN(signal.AvgGrade) {
			gradeWeight = 0.0
		} else {
			gradeWeight = clampF64((signal.AvgGrade-3.0)/2.0, -1.0, 1.0)
		}
		confidence += 0.10 * sampleWeight * gradeWeight
		risk -= 0.08 * sampleWeight * gradeWeight
	}

	// Clamp all scores to [0, 1].
	confidence = clampF64(confidence, 0.0, 1.0)
	effort = clampF64(effort, 0.0, 1.0)
	risk = clampF64(risk, 0.0, 1.0)

	// Priority formula (Rust parity: revenue_scoring.rs:148-153).
	revenueWeight := clampF64(revenue/1000.0, 0.0, 1.0)
	priority := ((confidence * 0.45) +
		((1.0 - risk) * 0.25) +
		((1.0 - effort) * 0.15) +
		(revenueWeight * 0.15)) * 100.0

	// Recommendation gate (Rust parity: revenue_scoring.rs:154).
	recommended := confidence >= 0.55 && risk <= 0.60 && effort <= 0.70

	// Score reason string.
	scopeStr := "no"
	if hasScopeMarker {
		scopeStr = "yes"
	}
	multiStr := "no"
	if multiRepo {
		multiStr = "yes"
	}
	avgGradeStr := "n/a"
	if signal != nil {
		avgGradeStr = fmt.Sprintf("%.2f", avgGrade)
	}
	reason := fmt.Sprintf(
		"strategy=%s; confidence=%.2f; risk=%.2f; effort=%.2f; source=%s; scope_marker=%s; multi_repo=%s; feedback_count=%d; feedback_avg=%s",
		strategy, confidence, risk, effort, source, scopeStr, multiStr, feedbackCount, avgGradeStr,
	)
	if payloadParseFailed {
		reason += "; WARNING: payload_json was unparseable"
	}

	return RevenueScore{
		ConfidenceScore:     confidence,
		EffortScore:         effort,
		RiskScore:           risk,
		PriorityScore:       priority,
		RecommendedApproved: recommended,
		ScoreReason:         reason,
	}
}

func clampF64(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
