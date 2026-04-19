package pipeline

import "strings"

// FinancialActionTruthGuard detects fabricated financial claims in responses.
// If the model claims to have performed financial actions (transfers, payments,
// deposits) but no wallet, payment, or transfer tool was actually called, the
// response is rejected and retried.
type FinancialActionTruthGuard struct{}

var financialActionClaims = []string{
	"i transferred",
	"i sent $",
	"i've sent $",
	"payment completed",
	"payment successful",
	"transaction completed",
	"i deposited",
	"i withdrew",
	"funds transferred",
	"transfer complete",
	"i paid",
	"i've paid",
	"payment has been processed",
	"i initiated a transfer",
	"i processed the payment",
}

var financialToolPrefixes = []string{
	"wallet",
	"payment",
	"transfer",
	"treasury",
	"eip3009",
	"x402",
	"send_token",
	"send_payment",
}

func (g *FinancialActionTruthGuard) Name() string { return "financial_action_truth" }

func (g *FinancialActionTruthGuard) Check(content string) GuardResult {
	return GuardResult{Passed: true}
}

func (g *FinancialActionTruthGuard) CheckWithContext(content string, ctx *GuardContext) GuardResult {
	if ctx == nil {
		return GuardResult{Passed: true}
	}

	// Check if any financial tool was actually called, and whether those calls
	// ended in real denial/failure signals. A tool name alone is not proof that
	// the action completed.
	hasFinancialTool := false
	hasFinancialFailure := false
	for _, tr := range ctx.ToolResults {
		lower := strings.ToLower(tr.ToolName)
		for _, prefix := range financialToolPrefixes {
			if strings.Contains(lower, prefix) {
				hasFinancialTool = true
				if toolResultSignalsFailure(tr) {
					hasFinancialFailure = true
				}
				break
			}
		}
	}

	lower := strings.ToLower(content)
	for _, claim := range financialActionClaims {
		if !strings.Contains(lower, claim) {
			continue
		}
		if !hasFinancialTool {
			return GuardResult{
				Passed: false,
				Retry:  true,
				Reason: "claimed financial action without calling a financial tool",
			}
		}
		if hasFinancialFailure {
			return GuardResult{
				Passed: false,
				Retry:  true,
				Reason: "claimed financial action despite denied or failed financial tool result",
			}
		}
	}

	return GuardResult{Passed: true}
}
