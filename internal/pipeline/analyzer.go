package pipeline

import "fmt"

// TurnData holds structured data about a single turn for analysis.
type TurnData struct {
	TurnID             string
	TokenBudget        int64
	SystemPromptTokens int64
	MemoryTokens       int64
	HistoryTokens      int64
	HistoryDepth       int64
	ComplexityLevel    string
	Model              string
	Cost               float64
	TokensIn           int64
	TokensOut          int64
	ToolCallCount      int64
	ToolFailureCount   int64
	ThinkingLength     int64
	HasReasoning       bool
	Cached             bool
}

// SessionData holds structured data about a session for analysis.
type SessionData struct {
	SessionID string
	Turns     []TurnData
	Grades    []SessionGrade
}

// SessionGrade pairs a turn with a quality grade.
type SessionGrade struct {
	TurnID string
	Grade  int
}

// Tip is a structured analysis finding.
type Tip struct {
	Severity   string `json:"severity"` // "critical", "warning", "info"
	Category   string `json:"category"` // "budget", "memory", "prompt", "tools", "cost", "quality"
	RuleName   string `json:"rule_name"`
	Message    string `json:"message"`
	Suggestion string `json:"suggestion"`
}

// ContextAnalyzer runs heuristic analysis on turn and session data.
type ContextAnalyzer struct{}

// NewContextAnalyzer creates a new analyzer.
func NewContextAnalyzer() *ContextAnalyzer {
	return &ContextAnalyzer{}
}

// AnalyzeTurn runs per-turn heuristic rules and returns tips.
func (a *ContextAnalyzer) AnalyzeTurn(td *TurnData) []Tip {
	var tips []Tip

	budget := td.TokenBudget
	if budget == 0 {
		budget = 8192 // default assumption
	}

	// 1. BudgetPressure: utilization > 90%
	totalUsed := td.SystemPromptTokens + td.MemoryTokens + td.HistoryTokens
	if budget > 0 {
		utilization := float64(totalUsed) / float64(budget)
		if utilization > 0.90 {
			tips = append(tips, Tip{
				Severity:   "warning",
				Category:   "budget",
				RuleName:   "BudgetPressure",
				Message:    fmt.Sprintf("Token budget utilization is %.0f%% — context is nearly full.", utilization*100),
				Suggestion: "Reduce system prompt length, lower memory budget, or increase token limit.",
			})
		}
	}

	// 2. SystemPromptHeavy: system tokens > 40% of budget
	if budget > 0 && float64(td.SystemPromptTokens)/float64(budget) > 0.40 {
		tips = append(tips, Tip{
			Severity:   "warning",
			Category:   "prompt",
			RuleName:   "SystemPromptHeavy",
			Message:    fmt.Sprintf("System prompt consumes %.0f%% of the token budget.", float64(td.SystemPromptTokens)/float64(budget)*100),
			Suggestion: "Trim the system prompt or move static context to memory retrieval.",
		})
	}

	// 3. MemoryStarvation: memory contribution < 5% when budget > 4000
	if budget > 4000 && td.MemoryTokens > 0 && float64(td.MemoryTokens)/float64(budget) < 0.05 {
		tips = append(tips, Tip{
			Severity:   "info",
			Category:   "memory",
			RuleName:   "MemoryStarvation",
			Message:    "Memory is using less than 5% of the context budget.",
			Suggestion: "Increase memory budgets or check retrieval relevance thresholds.",
		})
	}

	// 4. ShallowHistory: insufficient conversation depth
	if td.HistoryDepth < 2 && td.TokensIn > 500 {
		tips = append(tips, Tip{
			Severity:   "info",
			Category:   "prompt",
			RuleName:   "ShallowHistory",
			Message:    "Conversation history depth is very low despite substantial input.",
			Suggestion: "This may be a new session or aggressive history pruning.",
		})
	}

	// 5. HighToolDensity: many tool calls in one turn
	if td.ToolCallCount > 5 {
		tips = append(tips, Tip{
			Severity:   "info",
			Category:   "tools",
			RuleName:   "HighToolDensity",
			Message:    fmt.Sprintf("Turn used %d tool calls — high tool density.", td.ToolCallCount),
			Suggestion: "Consider decomposing into subtasks or using batch-capable tools.",
		})
	}

	// 6. ToolFailures: tool call success rate below 80%
	if td.ToolCallCount > 0 && td.ToolFailureCount > 0 {
		failRate := float64(td.ToolFailureCount) / float64(td.ToolCallCount)
		if failRate > 0.20 {
			tips = append(tips, Tip{
				Severity:   "warning",
				Category:   "tools",
				RuleName:   "ToolFailures",
				Message:    fmt.Sprintf("%.0f%% of tool calls failed in this turn.", failRate*100),
				Suggestion: "Check tool configurations and input validation.",
			})
		}
	}

	// 7. ExpensiveTurn: cost outlier
	if td.Cost > 0.05 {
		tips = append(tips, Tip{
			Severity:   "warning",
			Category:   "cost",
			RuleName:   "ExpensiveTurn",
			Message:    fmt.Sprintf("Turn cost $%.4f — above typical threshold.", td.Cost),
			Suggestion: "Route to a cheaper model for simpler tasks or enable caching.",
		})
	}

	// 8. EmptyReasoning: reasoning-capable model with no thinking output
	if td.HasReasoning && td.ThinkingLength == 0 && td.TokensOut > 100 {
		tips = append(tips, Tip{
			Severity:   "info",
			Category:   "quality",
			RuleName:   "EmptyReasoning",
			Message:    "Model supports reasoning but produced no thinking output.",
			Suggestion: "The task may not benefit from extended thinking, or the prompt may suppress it.",
		})
	}

	// 9. HistoryCostDominant: history tokens > 60% of budget
	if budget > 0 && float64(td.HistoryTokens)/float64(budget) > 0.60 {
		tips = append(tips, Tip{
			Severity:   "warning",
			Category:   "budget",
			RuleName:   "HistoryCostDominant",
			Message:    fmt.Sprintf("Conversation history uses %.0f%% of the context budget.", float64(td.HistoryTokens)/float64(budget)*100),
			Suggestion: "Enable more aggressive history compaction or summarization.",
		})
	}

	// 10. LargeOutputRatio: output tokens > 3x input tokens
	if td.TokensIn > 100 && td.TokensOut > 3*td.TokensIn {
		tips = append(tips, Tip{
			Severity:   "info",
			Category:   "cost",
			RuleName:   "LargeOutputRatio",
			Message:    fmt.Sprintf("Output tokens (%d) are %.1fx the input tokens (%d).", td.TokensOut, float64(td.TokensOut)/float64(td.TokensIn), td.TokensIn),
			Suggestion: "Consider constraining max_tokens or using a more concise prompt.",
		})
	}

	// 11. CachedTurnSavings: inform about cache benefit
	if td.Cached {
		tips = append(tips, Tip{
			Severity:   "info",
			Category:   "cost",
			RuleName:   "CachedTurnSavings",
			Message:    "This turn was served from cache — zero inference cost.",
			Suggestion: "Cache is working well for this query pattern.",
		})
	}

	// 12. SystemPromptTax: hidden system prompt cost
	if td.SystemPromptTokens > 2000 && td.Cost > 0 {
		sysCostFraction := float64(td.SystemPromptTokens) / float64(max64(td.TokensIn, 1))
		if sysCostFraction > 0.3 {
			tips = append(tips, Tip{
				Severity:   "info",
				Category:   "cost",
				RuleName:   "SystemPromptTax",
				Message:    fmt.Sprintf("System prompt accounts for ~%.0f%% of input tokens.", sysCostFraction*100),
				Suggestion: "Large system prompts are re-sent every turn — consider trimming or caching.",
			})
		}
	}

	return tips
}

// AnalyzeSession runs session-level heuristic rules and returns tips.
func (a *ContextAnalyzer) AnalyzeSession(sd *SessionData) []Tip {
	var tips []Tip
	n := len(sd.Turns)
	if n == 0 {
		return tips
	}

	// 1. ContextDrift: detect memory churn across turns
	var budgetUtilizations []float64
	for _, t := range sd.Turns {
		if t.TokenBudget > 0 {
			used := float64(t.SystemPromptTokens+t.MemoryTokens+t.HistoryTokens) / float64(t.TokenBudget)
			budgetUtilizations = append(budgetUtilizations, used)
		}
	}
	if len(budgetUtilizations) > 3 {
		variance := calcVariance(budgetUtilizations)
		if variance > 0.1 {
			tips = append(tips, Tip{
				Severity:   "warning",
				Category:   "memory",
				RuleName:   "ContextDrift",
				Message:    fmt.Sprintf("Context budget utilization varies significantly across turns (variance=%.2f).", variance),
				Suggestion: "Memory churn may be causing inconsistent context. Check retrieval relevance.",
			})
		}
	}

	// 2. FrequentEscalation: model switching
	models := make(map[string]int)
	for _, t := range sd.Turns {
		if t.Model != "" {
			models[t.Model]++
		}
	}
	if len(models) > 3 {
		tips = append(tips, Tip{
			Severity:   "warning",
			Category:   "quality",
			RuleName:   "ModelChurn",
			Message:    fmt.Sprintf("Session used %d different models — excessive switching.", len(models)),
			Suggestion: "Pin a primary model or adjust routing thresholds.",
		})
	}

	// 3. CostAcceleration: cost trending upward
	if n >= 4 {
		firstHalf := avgCost(sd.Turns[:n/2])
		secondHalf := avgCost(sd.Turns[n/2:])
		if secondHalf > firstHalf*1.5 && secondHalf > 0.01 {
			tips = append(tips, Tip{
				Severity:   "warning",
				Category:   "cost",
				RuleName:   "CostAcceleration",
				Message:    fmt.Sprintf("Inference cost is accelerating: first half avg $%.4f, second half avg $%.4f.", firstHalf, secondHalf),
				Suggestion: "Context growth or model escalation may be driving costs. Consider compaction.",
			})
		}
	}

	// 4. ToolSuccessRate: aggregate tool failure rate
	var totalTools, totalFails int64
	for _, t := range sd.Turns {
		totalTools += t.ToolCallCount
		totalFails += t.ToolFailureCount
	}
	if totalTools > 5 && totalFails > 0 {
		failRate := float64(totalFails) / float64(totalTools)
		if failRate > 0.15 {
			tips = append(tips, Tip{
				Severity:   "warning",
				Category:   "tools",
				RuleName:   "ToolSuccessRate",
				Message:    fmt.Sprintf("Session tool failure rate is %.0f%% (%d/%d).", failRate*100, totalFails, totalTools),
				Suggestion: "Investigate failing tool patterns — may indicate config or permission issues.",
			})
		}
	}

	// 5. QualityDeclining: grades trending down
	if len(sd.Grades) >= 3 {
		firstAvg := avgGrades(sd.Grades[:len(sd.Grades)/2])
		lastAvg := avgGrades(sd.Grades[len(sd.Grades)/2:])
		if firstAvg-lastAvg > 0.8 {
			tips = append(tips, Tip{
				Severity:   "warning",
				Category:   "quality",
				RuleName:   "QualityDeclining",
				Message:    fmt.Sprintf("Quality grades are declining: first half avg %.1f, second half avg %.1f.", firstAvg, lastAvg),
				Suggestion: "Context degradation or model fatigue may be a factor.",
			})
		}
	}

	// 6. UnderutilizedMemory: low memory token usage across session
	var avgMemFrac float64
	var memCount int
	for _, t := range sd.Turns {
		if t.TokenBudget > 0 {
			avgMemFrac += float64(t.MemoryTokens) / float64(t.TokenBudget)
			memCount++
		}
	}
	if memCount > 0 {
		avgMemFrac /= float64(memCount)
		if avgMemFrac < 0.03 && n > 5 {
			tips = append(tips, Tip{
				Severity:   "info",
				Category:   "memory",
				RuleName:   "UnderutilizedMemory",
				Message:    fmt.Sprintf("Average memory utilization is only %.1f%% across %d turns.", avgMemFrac*100, n),
				Suggestion: "Memory retrieval may be undertuned. Check embedding quality and retrieval thresholds.",
			})
		}
	}

	// 7. CostQualityMismatch: high cost but low grades
	var totalCost float64
	for _, t := range sd.Turns {
		totalCost += t.Cost
	}
	avgGrade := avgGrades(sd.Grades)
	if totalCost > 0.10 && avgGrade > 0 && avgGrade < 3.0 {
		tips = append(tips, Tip{
			Severity:   "critical",
			Category:   "cost",
			RuleName:   "CostQualityMismatch",
			Message:    fmt.Sprintf("Session cost $%.4f with average grade %.1f — poor cost-quality tradeoff.", totalCost, avgGrade),
			Suggestion: "The model may be overqualified for this task, or the prompt needs refinement.",
		})
	}

	// 8. LowCoverageWarning: sparse evaluation data
	if n > 10 && len(sd.Grades) < n/3 {
		tips = append(tips, Tip{
			Severity:   "info",
			Category:   "quality",
			RuleName:   "LowCoverageWarning",
			Message:    fmt.Sprintf("Only %d/%d turns have feedback grades — limited analysis confidence.", len(sd.Grades), n),
			Suggestion: "Enable automatic quality scoring or encourage user feedback.",
		})
	}

	return tips
}

// Helper functions.

func calcVariance(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	mean := 0.0
	for _, x := range xs {
		mean += x
	}
	mean /= float64(len(xs))
	variance := 0.0
	for _, x := range xs {
		d := x - mean
		variance += d * d
	}
	return variance / float64(len(xs))
}

func avgCost(turns []TurnData) float64 {
	if len(turns) == 0 {
		return 0
	}
	total := 0.0
	for _, t := range turns {
		total += t.Cost
	}
	return total / float64(len(turns))
}

func avgGrades(grades []SessionGrade) float64 {
	if len(grades) == 0 {
		return 0
	}
	total := 0.0
	for _, g := range grades {
		total += float64(g.Grade)
	}
	return total / float64(len(grades))
}

func max64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

// BuildTurnAnalysisPrompt constructs an LLM prompt from turn data and heuristic tips.
func BuildTurnAnalysisPrompt(td *TurnData, tips []Tip) string {
	s := fmt.Sprintf("Analyze this inference turn and provide specific remediation advice.\n\n"+
		"Turn metrics:\n- Model: %s\n- Tokens in: %d, out: %d\n- Cost: $%.4f\n- Token budget: %d\n"+
		"- System prompt tokens: %d\n- Memory tokens: %d\n- History tokens: %d (depth %d)\n"+
		"- Tool calls: %d (failures: %d)\n- Complexity: %s\n- Cached: %v\n\n",
		td.Model, td.TokensIn, td.TokensOut, td.Cost, td.TokenBudget,
		td.SystemPromptTokens, td.MemoryTokens, td.HistoryTokens, td.HistoryDepth,
		td.ToolCallCount, td.ToolFailureCount, td.ComplexityLevel, td.Cached)

	if len(tips) > 0 {
		s += "Heuristic findings:\n"
		for _, tip := range tips {
			s += fmt.Sprintf("- [%s/%s] %s: %s\n", tip.Severity, tip.Category, tip.RuleName, tip.Message)
		}
	} else {
		s += "No heuristic issues detected.\n"
	}

	s += "\nProvide:\n1. A brief assessment of this turn's efficiency\n2. Specific actions to improve cost, quality, or latency\n3. Any configuration changes recommended\n\nBe concise and actionable. Use markdown formatting."
	return s
}

// BuildSessionAnalysisPrompt constructs an LLM prompt from session data and heuristic tips.
func BuildSessionAnalysisPrompt(sessionID string, turns []TurnData, tips []Tip, grades []SessionGrade) string {
	var totalCost float64
	var totalTokIn, totalTokOut int64
	models := make(map[string]int)
	for _, t := range turns {
		totalCost += t.Cost
		totalTokIn += t.TokensIn
		totalTokOut += t.TokensOut
		if t.Model != "" {
			models[t.Model]++
		}
	}

	s := fmt.Sprintf("Analyze this agent session and provide specific remediation advice.\n\n"+
		"Session: %s\n- Turns: %d\n- Total tokens in: %d, out: %d\n- Total cost: $%.4f\n- Models used: %d distinct\n",
		sessionID, len(turns), totalTokIn, totalTokOut, totalCost, len(models))

	if len(grades) > 0 {
		var sum float64
		for _, g := range grades {
			sum += float64(g.Grade)
		}
		s += fmt.Sprintf("- Average quality grade: %.1f/5 (%d graded turns)\n", sum/float64(len(grades)), len(grades))
	}

	if len(tips) > 0 {
		s += "\nHeuristic findings:\n"
		for _, tip := range tips {
			s += fmt.Sprintf("- [%s/%s] %s: %s\n", tip.Severity, tip.Category, tip.RuleName, tip.Message)
		}
		s += "\nTop suggestions:\n"
		count := 0
		for _, tip := range tips {
			if tip.Suggestion != "" && count < 3 {
				s += fmt.Sprintf("- %s\n", tip.Suggestion)
				count++
			}
		}
	} else {
		s += "\nNo heuristic issues detected.\n"
	}

	s += "\nProvide:\n1. Overall session health assessment\n2. Root cause analysis for any degradation\n3. Specific configuration or behavioral changes recommended\n4. Priority of remediation actions\n\nBe concise and actionable. Use markdown formatting."
	return s
}

// BuildHeuristicSummary creates a markdown summary from tips (fallback when LLM unavailable).
func BuildHeuristicSummary(tips []Tip) string {
	if len(tips) == 0 {
		return "No issues detected. Turn appears healthy."
	}
	var critCount, warnCount int
	for _, tip := range tips {
		switch tip.Severity {
		case "critical":
			critCount++
		case "warning":
			warnCount++
		}
	}
	s := fmt.Sprintf("Heuristic analysis: %d critical, %d warnings, %d info.\n\n", critCount, warnCount, len(tips)-critCount-warnCount)
	for _, tip := range tips {
		s += fmt.Sprintf("**%s** (%s): %s\n  → %s\n\n", tip.RuleName, tip.Severity, tip.Message, tip.Suggestion)
	}
	return s
}
