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

	// Check if any financial tool was actually called.
	hasFinancialTool := false
	for _, tr := range ctx.ToolResults {
		lower := strings.ToLower(tr.ToolName)
		for _, prefix := range financialToolPrefixes {
			if strings.Contains(lower, prefix) {
				hasFinancialTool = true
				break
			}
		}
		if hasFinancialTool {
			break
		}
	}

	// If financial tools were called, the claims may be legitimate.
	if hasFinancialTool {
		return GuardResult{Passed: true}
	}

	// Check for fabricated financial claims.
	lower := strings.ToLower(content)
	for _, claim := range financialActionClaims {
		if strings.Contains(lower, claim) {
			return GuardResult{
				Passed: false,
				Retry:  true,
				Reason: "claimed financial action without calling a financial tool",
			}
		}
	}

	return GuardResult{Passed: true}
}
