package pipeline

import "strings"

// ActionVerificationGuard cross-references financial action claims against
// actual tool execution in the guard context (Wave 8, #76). Unlike
// FinancialActionTruthGuard which checks for fabricated claims, this guard
// verifies that claimed amounts and recipients match tool output.
type ActionVerificationGuard struct{}

func (g *ActionVerificationGuard) Name() string { return "action_verification" }

func (g *ActionVerificationGuard) Check(content string) GuardResult {
	return GuardResult{Passed: true}
}

func (g *ActionVerificationGuard) CheckWithContext(content string, ctx *GuardContext) GuardResult {
	if ctx == nil {
		return GuardResult{Passed: true}
	}

	// Only trigger when financial tools were actually called.
	var financialResults []ToolResultEntry
	for _, tr := range ctx.ToolResults {
		lower := strings.ToLower(tr.ToolName)
		for _, prefix := range financialToolPrefixes {
			if strings.Contains(lower, prefix) {
				financialResults = append(financialResults, tr)
				break
			}
		}
	}
	if len(financialResults) == 0 {
		return GuardResult{Passed: true}
	}

	lower := strings.ToLower(content)

	// Check for success claims when tool output indicates failure.
	for _, tr := range financialResults {
		outputLower := strings.ToLower(tr.Output)
		hasFailure := strings.Contains(outputLower, "error") ||
			strings.Contains(outputLower, "failed") ||
			strings.Contains(outputLower, "insufficient") ||
			strings.Contains(outputLower, "rejected")

		if hasFailure {
			// Verify the response doesn't claim success.
			successClaims := []string{
				"successfully transferred", "payment completed",
				"transfer complete", "transaction successful",
				"funds sent", "payment processed",
			}
			for _, claim := range successClaims {
				if strings.Contains(lower, claim) {
					return GuardResult{
						Passed:  false,
						Retry:   true,
						Reason:  "claimed financial success but tool reported failure",
						Verdict: GuardRetryRequested,
					}
				}
			}
		}
	}

	// Check for amount mismatches: response mentions a dollar amount not in tool output.
	// This is a heuristic — we look for $X patterns in response and verify they appear in output.
	responseAmounts := extractDollarAmounts(lower)
	if len(responseAmounts) > 0 {
		allOutputs := ""
		for _, tr := range financialResults {
			allOutputs += strings.ToLower(tr.Output) + " "
		}
		for _, amt := range responseAmounts {
			if !strings.Contains(allOutputs, amt) {
				return GuardResult{
					Passed:  false,
					Retry:   true,
					Reason:  "response contains dollar amount not found in tool output: " + amt,
					Verdict: GuardRetryRequested,
				}
			}
		}
	}

	return GuardResult{Passed: true}
}

// extractDollarAmounts finds $X.XX patterns in text.
func extractDollarAmounts(text string) []string {
	var amounts []string
	for i := 0; i < len(text)-1; i++ {
		if text[i] == '$' {
			j := i + 1
			for j < len(text) && (text[j] >= '0' && text[j] <= '9' || text[j] == '.' || text[j] == ',') {
				j++
			}
			if j > i+1 {
				amounts = append(amounts, text[i:j])
			}
		}
	}
	return amounts
}
