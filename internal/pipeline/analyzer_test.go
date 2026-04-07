package pipeline

import (
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// BuildTurnAnalysisPrompt tests
// ---------------------------------------------------------------------------

func TestBuildTurnAnalysisPrompt_IncludesAllMetricFields(t *testing.T) {
	td := &TurnData{
		TurnID:             "t-1",
		TokenBudget:        8192,
		SystemPromptTokens: 2000,
		MemoryTokens:       500,
		HistoryTokens:      1000,
		HistoryDepth:       5,
		ComplexityLevel:    "high",
		Model:              "gpt-4",
		Cost:               0.03,
		TokensIn:           3500,
		TokensOut:          800,
		ToolCallCount:      3,
		ToolFailureCount:   1,
		ThinkingLength:     200,
		HasReasoning:       true,
		Cached:             false,
	}
	prompt := BuildTurnAnalysisPrompt(td, nil)

	requiredFields := []string{
		"gpt-4",      // Model
		"3500",       // TokensIn
		"800",        // TokensOut
		"0.0300",     // Cost
		"8192",       // TokenBudget
		"2000",       // SystemPromptTokens
		"500",        // MemoryTokens
		"1000",       // HistoryTokens
		"depth 5",    // HistoryDepth
		"3",          // ToolCallCount
		"failures: 1",// ToolFailureCount
		"high",       // ComplexityLevel
		"false",      // Cached
	}

	for _, field := range requiredFields {
		if !strings.Contains(prompt, field) {
			t.Errorf("BuildTurnAnalysisPrompt missing expected field %q", field)
		}
	}
}

func TestBuildTurnAnalysisPrompt_IncludesHeuristicFindings(t *testing.T) {
	td := &TurnData{Model: "claude-3", TokensIn: 100, TokensOut: 50}
	tips := []Tip{
		{Severity: "warning", Category: "budget", RuleName: "BudgetPressure", Message: "Token budget is full."},
	}
	prompt := BuildTurnAnalysisPrompt(td, tips)
	if !strings.Contains(prompt, "BudgetPressure") {
		t.Error("prompt should contain heuristic rule name")
	}
	if !strings.Contains(prompt, "Heuristic findings:") {
		t.Error("prompt should contain heuristic findings header")
	}
}

func TestBuildTurnAnalysisPrompt_NoTips(t *testing.T) {
	td := &TurnData{Model: "claude-3", TokensIn: 100, TokensOut: 50}
	prompt := BuildTurnAnalysisPrompt(td, nil)
	if !strings.Contains(prompt, "No heuristic issues detected") {
		t.Error("prompt should state no issues when tips are empty")
	}
}

// ---------------------------------------------------------------------------
// BuildSessionAnalysisPrompt tests
// ---------------------------------------------------------------------------

func TestBuildSessionAnalysisPrompt_IncludesGradeAverages(t *testing.T) {
	turns := []TurnData{
		{Model: "gpt-4", TokensIn: 100, TokensOut: 50, Cost: 0.01},
		{Model: "gpt-4", TokensIn: 200, TokensOut: 100, Cost: 0.02},
	}
	grades := []SessionGrade{
		{TurnID: "t-1", Grade: 4},
		{TurnID: "t-2", Grade: 3},
	}
	prompt := BuildSessionAnalysisPrompt("sess-1", turns, nil, grades)
	if !strings.Contains(prompt, "3.5") {
		t.Error("prompt should contain grade average 3.5")
	}
	if !strings.Contains(prompt, "2 graded turns") {
		t.Error("prompt should indicate number of graded turns")
	}
}

func TestBuildSessionAnalysisPrompt_NoGrades(t *testing.T) {
	turns := []TurnData{{Model: "gpt-4", TokensIn: 100, TokensOut: 50, Cost: 0.01}}
	prompt := BuildSessionAnalysisPrompt("sess-1", turns, nil, nil)
	if strings.Contains(prompt, "quality grade") {
		t.Error("prompt should not mention quality grade when no grades exist")
	}
}

func TestBuildSessionAnalysisPrompt_IncludesSessionMetrics(t *testing.T) {
	turns := []TurnData{
		{Model: "gpt-4", TokensIn: 500, TokensOut: 200, Cost: 0.05},
		{Model: "claude-3", TokensIn: 300, TokensOut: 100, Cost: 0.03},
	}
	prompt := BuildSessionAnalysisPrompt("sess-2", turns, nil, nil)
	if !strings.Contains(prompt, "sess-2") {
		t.Error("prompt should contain session ID")
	}
	if !strings.Contains(prompt, "Turns: 2") {
		t.Error("prompt should contain turn count")
	}
	if !strings.Contains(prompt, "2 distinct") {
		t.Error("prompt should indicate distinct model count")
	}
}

// ---------------------------------------------------------------------------
// BuildHeuristicSummary tests
// ---------------------------------------------------------------------------

func TestBuildHeuristicSummary_AllSeverityLevels(t *testing.T) {
	tips := []Tip{
		{Severity: "critical", Category: "cost", RuleName: "CostQualityMismatch", Message: "Poor tradeoff.", Suggestion: "Reduce model tier."},
		{Severity: "warning", Category: "budget", RuleName: "BudgetPressure", Message: "Budget full.", Suggestion: "Trim system prompt."},
		{Severity: "info", Category: "memory", RuleName: "MemoryStarvation", Message: "Low memory.", Suggestion: "Increase budgets."},
	}
	summary := BuildHeuristicSummary(tips)

	if !strings.Contains(summary, "1 critical") {
		t.Error("summary should count critical tips")
	}
	if !strings.Contains(summary, "1 warnings") {
		t.Error("summary should count warnings")
	}
	if !strings.Contains(summary, "1 info") {
		t.Error("summary should count info tips")
	}
	for _, tip := range tips {
		if !strings.Contains(summary, tip.RuleName) {
			t.Errorf("summary missing rule name %q", tip.RuleName)
		}
		if !strings.Contains(summary, tip.Suggestion) {
			t.Errorf("summary missing suggestion for %q", tip.RuleName)
		}
	}
}

func TestBuildHeuristicSummary_NoTips(t *testing.T) {
	summary := BuildHeuristicSummary(nil)
	if !strings.Contains(summary, "No issues detected") {
		t.Error("empty tips should produce healthy message")
	}
}

// ---------------------------------------------------------------------------
// AnalyzeTurn tests — table-driven
// ---------------------------------------------------------------------------

func TestAnalyzeTurn_TableDriven(t *testing.T) {
	analyzer := NewContextAnalyzer()

	tests := []struct {
		name         string
		turn         TurnData
		expectRules  []string // rules that MUST fire
		rejectRules  []string // rules that must NOT fire
	}{
		{
			name: "BudgetPressure fires at 95%",
			turn: TurnData{
				TokenBudget:        10000,
				SystemPromptTokens: 5000,
				MemoryTokens:       2000,
				HistoryTokens:      2600,
			},
			expectRules: []string{"BudgetPressure"},
		},
		{
			name: "BudgetPressure does not fire at 50%",
			turn: TurnData{
				TokenBudget:        10000,
				SystemPromptTokens: 3000,
				MemoryTokens:       1000,
				HistoryTokens:      1000,
			},
			rejectRules: []string{"BudgetPressure"},
		},
		{
			name: "SystemPromptHeavy fires at 50%",
			turn: TurnData{
				TokenBudget:        10000,
				SystemPromptTokens: 5000,
			},
			expectRules: []string{"SystemPromptHeavy"},
		},
		{
			name: "MemoryStarvation fires when memory < 5% of large budget",
			turn: TurnData{
				TokenBudget: 10000,
				MemoryTokens: 200,
			},
			expectRules: []string{"MemoryStarvation"},
		},
		{
			name: "ShallowHistory fires with depth 1 and large input",
			turn: TurnData{
				TokenBudget:  8192,
				HistoryDepth: 1,
				TokensIn:     1000,
			},
			expectRules: []string{"ShallowHistory"},
		},
		{
			name: "HighToolDensity fires with 10 tool calls",
			turn: TurnData{
				TokenBudget:   8192,
				ToolCallCount: 10,
			},
			expectRules: []string{"HighToolDensity"},
		},
		{
			name: "ToolFailures fires at 25% failure rate",
			turn: TurnData{
				TokenBudget:      8192,
				ToolCallCount:    8,
				ToolFailureCount: 2,
			},
			expectRules: []string{"ToolFailures"},
		},
		{
			name: "ToolFailures does not fire at 10% failure rate",
			turn: TurnData{
				TokenBudget:      8192,
				ToolCallCount:    10,
				ToolFailureCount: 1,
			},
			rejectRules: []string{"ToolFailures"},
		},
		{
			name: "ExpensiveTurn fires above $0.05",
			turn: TurnData{
				TokenBudget: 8192,
				Cost:        0.08,
			},
			expectRules: []string{"ExpensiveTurn"},
		},
		{
			name: "ExpensiveTurn does not fire at $0.03",
			turn: TurnData{
				TokenBudget: 8192,
				Cost:        0.03,
			},
			rejectRules: []string{"ExpensiveTurn"},
		},
		{
			name: "EmptyReasoning fires when reasoning model has no thinking",
			turn: TurnData{
				TokenBudget:    8192,
				HasReasoning:   true,
				ThinkingLength: 0,
				TokensOut:      500,
			},
			expectRules: []string{"EmptyReasoning"},
		},
		{
			name: "HistoryCostDominant fires at 70% history",
			turn: TurnData{
				TokenBudget:   10000,
				HistoryTokens: 7000,
			},
			expectRules: []string{"HistoryCostDominant"},
		},
		{
			name: "LargeOutputRatio fires when output is 4x input",
			turn: TurnData{
				TokenBudget: 8192,
				TokensIn:    200,
				TokensOut:   1000,
			},
			expectRules: []string{"LargeOutputRatio"},
		},
		{
			name: "CachedTurnSavings fires on cached turn",
			turn: TurnData{
				TokenBudget: 8192,
				Cached:      true,
			},
			expectRules: []string{"CachedTurnSavings"},
		},
		{
			name: "SystemPromptTax fires with heavy system prompt and cost",
			turn: TurnData{
				TokenBudget:        8192,
				SystemPromptTokens: 3000,
				TokensIn:           5000,
				Cost:               0.03,
			},
			expectRules: []string{"SystemPromptTax"},
		},
		{
			name: "Clean turn produces no tips",
			turn: TurnData{
				TokenBudget:        10000,
				SystemPromptTokens: 1000,
				MemoryTokens:       1000,
				HistoryTokens:      1000,
				HistoryDepth:       5,
				TokensIn:           3000,
				TokensOut:          500,
				ToolCallCount:      2,
				ToolFailureCount:   0,
				Cost:               0.02,
			},
			expectRules: []string{},
			rejectRules: []string{"BudgetPressure", "ExpensiveTurn", "ToolFailures"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tips := analyzer.AnalyzeTurn(&tt.turn)
			ruleSet := make(map[string]bool)
			for _, tip := range tips {
				ruleSet[tip.RuleName] = true
			}

			for _, rule := range tt.expectRules {
				if !ruleSet[rule] {
					t.Errorf("expected rule %q to fire but it did not; got rules: %v", rule, ruleNames(tips))
				}
			}
			for _, rule := range tt.rejectRules {
				if ruleSet[rule] {
					t.Errorf("rule %q should not fire but it did", rule)
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// AnalyzeSession tests — table-driven
// ---------------------------------------------------------------------------

func TestAnalyzeSession_TableDriven(t *testing.T) {
	analyzer := NewContextAnalyzer()

	tests := []struct {
		name        string
		session     SessionData
		expectRules []string
		rejectRules []string
	}{
		{
			name: "QualityDeclining fires on declining grades",
			session: SessionData{
				SessionID: "s-1",
				Turns:     makeTurns(6, 0.01, "gpt-4"),
				Grades: []SessionGrade{
					{TurnID: "0", Grade: 5},
					{TurnID: "1", Grade: 5},
					{TurnID: "2", Grade: 4},
					{TurnID: "3", Grade: 3},
					{TurnID: "4", Grade: 2},
					{TurnID: "5", Grade: 2},
				},
			},
			expectRules: []string{"QualityDeclining"},
		},
		{
			name: "QualityDeclining does not fire on stable grades",
			session: SessionData{
				SessionID: "s-2",
				Turns:     makeTurns(4, 0.01, "gpt-4"),
				Grades: []SessionGrade{
					{TurnID: "0", Grade: 4},
					{TurnID: "1", Grade: 4},
					{TurnID: "2", Grade: 4},
					{TurnID: "3", Grade: 4},
				},
			},
			rejectRules: []string{"QualityDeclining"},
		},
		{
			name: "ModelChurn fires with 4 models",
			session: SessionData{
				SessionID: "s-3",
				Turns: []TurnData{
					{Model: "gpt-4", TokenBudget: 8192, TokensIn: 100, Cost: 0.01},
					{Model: "claude-3", TokenBudget: 8192, TokensIn: 100, Cost: 0.01},
					{Model: "llama-70b", TokenBudget: 8192, TokensIn: 100, Cost: 0.01},
					{Model: "gemini-pro", TokenBudget: 8192, TokensIn: 100, Cost: 0.01},
				},
			},
			expectRules: []string{"ModelChurn"},
		},
		{
			name: "ModelChurn does not fire with 2 models",
			session: SessionData{
				SessionID: "s-4",
				Turns: []TurnData{
					{Model: "gpt-4", TokenBudget: 8192, TokensIn: 100, Cost: 0.01},
					{Model: "claude-3", TokenBudget: 8192, TokensIn: 100, Cost: 0.01},
				},
			},
			rejectRules: []string{"ModelChurn"},
		},
		{
			name: "CostAcceleration fires when second half cost is much higher",
			session: SessionData{
				SessionID: "s-5",
				Turns: []TurnData{
					{Model: "gpt-4", TokenBudget: 8192, TokensIn: 100, Cost: 0.005},
					{Model: "gpt-4", TokenBudget: 8192, TokensIn: 100, Cost: 0.005},
					{Model: "gpt-4", TokenBudget: 8192, TokensIn: 100, Cost: 0.02},
					{Model: "gpt-4", TokenBudget: 8192, TokensIn: 100, Cost: 0.03},
				},
			},
			expectRules: []string{"CostAcceleration"},
		},
		{
			name: "CostAcceleration does not fire on stable costs",
			session: SessionData{
				SessionID: "s-6",
				Turns:     makeTurns(4, 0.01, "gpt-4"),
			},
			rejectRules: []string{"CostAcceleration"},
		},
		{
			name: "ToolSuccessRate fires on high aggregate failure rate",
			session: SessionData{
				SessionID: "s-7",
				Turns: []TurnData{
					{Model: "gpt-4", TokenBudget: 8192, ToolCallCount: 5, ToolFailureCount: 2},
					{Model: "gpt-4", TokenBudget: 8192, ToolCallCount: 5, ToolFailureCount: 2},
				},
			},
			expectRules: []string{"ToolSuccessRate"},
		},
		{
			name: "ContextDrift fires on high variance utilization",
			session: SessionData{
				SessionID: "s-8",
				Turns: []TurnData{
					{TokenBudget: 10000, SystemPromptTokens: 1000, MemoryTokens: 500, HistoryTokens: 500},
					{TokenBudget: 10000, SystemPromptTokens: 1000, MemoryTokens: 500, HistoryTokens: 8000},
					{TokenBudget: 10000, SystemPromptTokens: 1000, MemoryTokens: 500, HistoryTokens: 500},
					{TokenBudget: 10000, SystemPromptTokens: 1000, MemoryTokens: 500, HistoryTokens: 8000},
				},
			},
			expectRules: []string{"ContextDrift"},
		},
		{
			name: "CostQualityMismatch fires on expensive session with low grades",
			session: SessionData{
				SessionID: "s-9",
				Turns: []TurnData{
					{Model: "gpt-4", TokenBudget: 8192, TokensIn: 100, Cost: 0.06},
					{Model: "gpt-4", TokenBudget: 8192, TokensIn: 100, Cost: 0.06},
				},
				Grades: []SessionGrade{
					{TurnID: "0", Grade: 2},
					{TurnID: "1", Grade: 2},
				},
			},
			expectRules: []string{"CostQualityMismatch"},
		},
		{
			name: "LowCoverageWarning fires with sparse grades on large session",
			session: SessionData{
				SessionID: "s-10",
				Turns:     makeTurns(12, 0.01, "gpt-4"),
				Grades: []SessionGrade{
					{TurnID: "0", Grade: 4},
					{TurnID: "5", Grade: 3},
				},
			},
			expectRules: []string{"LowCoverageWarning"},
		},
		{
			name: "UnderutilizedMemory fires on long session with low memory",
			session: SessionData{
				SessionID: "s-11",
				Turns: func() []TurnData {
					turns := make([]TurnData, 8)
					for i := range turns {
						turns[i] = TurnData{TokenBudget: 10000, MemoryTokens: 100, TokensIn: 100, Cost: 0.01}
					}
					return turns
				}(),
			},
			expectRules: []string{"UnderutilizedMemory"},
		},
		{
			name: "Clean session produces no tips",
			session: SessionData{
				SessionID: "s-12",
				Turns: []TurnData{
					{Model: "gpt-4", TokenBudget: 10000, SystemPromptTokens: 1000, MemoryTokens: 1000, HistoryTokens: 1000, TokensIn: 100, Cost: 0.01},
					{Model: "gpt-4", TokenBudget: 10000, SystemPromptTokens: 1000, MemoryTokens: 1000, HistoryTokens: 1000, TokensIn: 100, Cost: 0.01},
				},
				Grades: []SessionGrade{
					{TurnID: "0", Grade: 4},
					{TurnID: "1", Grade: 4},
				},
			},
			rejectRules: []string{"ContextDrift", "ModelChurn", "CostAcceleration", "CostQualityMismatch"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tips := analyzer.AnalyzeSession(&tt.session)
			ruleSet := make(map[string]bool)
			for _, tip := range tips {
				ruleSet[tip.RuleName] = true
			}

			for _, rule := range tt.expectRules {
				if !ruleSet[rule] {
					t.Errorf("expected rule %q to fire but it did not; got rules: %v", rule, ruleNames(tips))
				}
			}
			for _, rule := range tt.rejectRules {
				if ruleSet[rule] {
					t.Errorf("rule %q should not fire but it did", rule)
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Tip field validation
// ---------------------------------------------------------------------------

func TestAnalyzeTurn_TipFieldsPopulated(t *testing.T) {
	analyzer := NewContextAnalyzer()
	td := &TurnData{
		TokenBudget:        10000,
		SystemPromptTokens: 5000,
		MemoryTokens:       200,
		HistoryTokens:      4500,
		HistoryDepth:       1,
		TokensIn:           1000,
		TokensOut:          4000,
		ToolCallCount:      8,
		ToolFailureCount:   3,
		Cost:               0.08,
		HasReasoning:       true,
		ThinkingLength:     0,
		Model:              "gpt-4",
	}
	tips := analyzer.AnalyzeTurn(td)
	if len(tips) == 0 {
		t.Fatal("expected at least one tip for known-bad data")
	}
	for _, tip := range tips {
		if tip.Severity == "" {
			t.Errorf("tip %q has empty severity", tip.RuleName)
		}
		if tip.Category == "" {
			t.Errorf("tip %q has empty category", tip.RuleName)
		}
		if tip.RuleName == "" {
			t.Error("tip has empty rule name")
		}
		if tip.Message == "" {
			t.Errorf("tip %q has empty message", tip.RuleName)
		}
		if tip.Suggestion == "" {
			t.Errorf("tip %q has empty suggestion", tip.RuleName)
		}
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func ruleNames(tips []Tip) []string {
	names := make([]string, len(tips))
	for i, tip := range tips {
		names[i] = tip.RuleName
	}
	return names
}

func makeTurns(n int, cost float64, model string) []TurnData {
	turns := make([]TurnData, n)
	for i := range turns {
		turns[i] = TurnData{
			Model:       model,
			TokenBudget: 8192,
			TokensIn:    100,
			TokensOut:   50,
			Cost:        cost,
		}
	}
	return turns
}
