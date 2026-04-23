package pipeline

import "testing"

func TestFinancialActionTruthGuard_BlocksFabricatedTransfer(t *testing.T) {
	g := &FinancialActionTruthGuard{}
	ctx := &GuardContext{} // no tool results

	result := g.CheckWithContext("I transferred $500 to your account.", ctx)
	if result.Passed {
		t.Fatal("expected guard to block fabricated transfer claim")
	}
	if !result.Retry {
		t.Fatal("expected retry flag for fabricated financial claim")
	}
}

func TestFinancialActionTruthGuard_BlocksFabricatedPayment(t *testing.T) {
	g := &FinancialActionTruthGuard{}
	ctx := &GuardContext{
		ToolResults: []ToolResultEntry{
			{ToolName: "read_file", Output: "some file content"},
		},
	}

	result := g.CheckWithContext("Payment completed successfully.", ctx)
	if result.Passed {
		t.Fatal("expected guard to block fabricated payment claim")
	}
}

func TestFinancialActionTruthGuard_AllowsWithWalletTool(t *testing.T) {
	g := &FinancialActionTruthGuard{}
	ctx := &GuardContext{
		ToolResults: []ToolResultEntry{
			{ToolName: "wallet_transfer", Output: "tx_hash: 0xabc123"},
		},
	}

	result := g.CheckWithContext("I transferred $500 to your account.", ctx)
	if !result.Passed {
		t.Fatalf("expected pass when wallet tool was used, got reason: %s", result.Reason)
	}
}

func TestFinancialActionTruthGuard_AllowsWithPaymentTool(t *testing.T) {
	g := &FinancialActionTruthGuard{}
	ctx := &GuardContext{
		ToolResults: []ToolResultEntry{
			{ToolName: "x402_payment", Output: "payment_id: pay_123"},
		},
	}

	result := g.CheckWithContext("Payment completed successfully.", ctx)
	if !result.Passed {
		t.Fatal("expected pass when payment tool was used")
	}
}

func TestFinancialActionTruthGuard_BlocksClaimAfterDeniedFinancialTool(t *testing.T) {
	g := &FinancialActionTruthGuard{}
	ctx := &GuardContext{
		ToolResults: []ToolResultEntry{
			{ToolName: "wallet_transfer", Output: "Policy denied: amount 50000 cents exceeds limit of 10000 cents"},
		},
	}

	result := g.CheckWithContext("I transferred $500 to your account.", ctx)
	if result.Passed {
		t.Fatal("expected denied financial tool result to block fabricated success claim")
	}
	if !result.Retry {
		t.Fatal("expected retry when success was claimed after denied financial tool")
	}
}

func TestFinancialActionTruthGuard_PassesNonFinancialContent(t *testing.T) {
	g := &FinancialActionTruthGuard{}
	ctx := &GuardContext{}

	result := g.CheckWithContext("The weather today is sunny.", ctx)
	if !result.Passed {
		t.Fatal("expected pass for non-financial content")
	}
}

func TestFinancialActionTruthGuard_PassesNilContext(t *testing.T) {
	g := &FinancialActionTruthGuard{}

	result := g.CheckWithContext("I transferred $100.", nil)
	if !result.Passed {
		t.Fatal("expected pass with nil context")
	}
}
