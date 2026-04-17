package pipeline

import "testing"

func TestActionVerificationGuard_BlocksSuccessClaimAfterDeniedFinancialTool(t *testing.T) {
	g := &ActionVerificationGuard{}
	ctx := &GuardContext{
		ToolResults: []ToolResultEntry{
			{ToolName: "wallet_transfer", Output: "Policy denied: amount 50000 cents exceeds limit of 10000 cents"},
		},
	}

	result := g.CheckWithContext("I transferred $500 to your account.", ctx)
	if result.Passed {
		t.Fatal("expected denied financial tool result to block success claim")
	}
	if !result.Retry {
		t.Fatal("expected retry when response claims success after denied financial tool")
	}
}
