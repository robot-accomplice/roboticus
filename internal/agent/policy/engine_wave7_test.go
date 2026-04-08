package policy

import (
	"testing"

	"roboticus/internal/agent/tools"
	"roboticus/internal/core"
)

// --- #65: SecurityClaim in ToolCallRequest ---

func TestToolCallRequest_SecurityClaimField(t *testing.T) {
	claim := &core.SecurityClaim{
		Authority:        core.AuthorityPeer,
		Sources:          []core.ClaimSource{core.ClaimSourceChannelAllowList},
		Ceiling:          core.AuthorityCreator,
		ThreatDowngraded: false,
		SenderID:         "user123",
		Channel:          "telegram",
	}
	req := &ToolCallRequest{
		ToolName:      "echo",
		Arguments:     `{"msg":"hi"}`,
		Authority:     core.AuthorityPeer,
		SecurityClaim: claim,
	}
	if req.SecurityClaim == nil {
		t.Fatal("SecurityClaim should be set")
	}
	if req.SecurityClaim.SenderID != "user123" {
		t.Errorf("expected SenderID user123, got %s", req.SecurityClaim.SenderID)
	}
	if req.SecurityClaim.Authority != core.AuthorityPeer {
		t.Errorf("expected AuthorityPeer, got %d", req.SecurityClaim.Authority)
	}
}

func TestToolCallRequest_SecurityClaimNil(t *testing.T) {
	req := &ToolCallRequest{
		ToolName:  "echo",
		Arguments: `{}`,
		Authority: core.AuthorityCreator,
	}
	// nil SecurityClaim should not cause panics in evaluation.
	pe := NewEngine(DefaultConfig())
	result := pe.Evaluate(req)
	if result.Denied() {
		t.Errorf("normal call with nil SecurityClaim should be allowed, denied by %s", result.Rule)
	}
}

// --- #66: Financial drain detection ---

func TestCheckFinancialDrain_DrainInToolName(t *testing.T) {
	req := &ToolCallRequest{
		ToolName:  "drain_wallet",
		Arguments: `{}`,
		Authority: core.AuthorityCreator,
	}
	result := CheckFinancialDrain(req)
	if !result.Denied() {
		t.Error("drain in tool name should be denied")
	}
	if result.Rule != "financial_drain" {
		t.Errorf("expected financial_drain rule, got %s", result.Rule)
	}
}

func TestCheckFinancialDrain_WithdrawAllInArgs(t *testing.T) {
	req := &ToolCallRequest{
		ToolName:  "transfer",
		Arguments: `{"action":"withdraw_all","wallet":"abc"}`,
		Authority: core.AuthorityCreator,
	}
	result := CheckFinancialDrain(req)
	if !result.Denied() {
		t.Error("withdraw_all in args should be denied")
	}
}

func TestCheckFinancialDrain_SweepInArgs(t *testing.T) {
	req := &ToolCallRequest{
		ToolName:  "wallet_action",
		Arguments: `{"action":"sweep","target":"attacker"}`,
		Authority: core.AuthorityCreator,
	}
	result := CheckFinancialDrain(req)
	if !result.Denied() {
		t.Error("sweep pattern should be denied")
	}
}

func TestCheckFinancialDrain_PercentageDrain(t *testing.T) {
	req := &ToolCallRequest{
		ToolName:  "transfer",
		Arguments: `{"percentage":100,"target":"attacker"}`,
		Authority: core.AuthorityCreator,
	}
	result := CheckFinancialDrain(req)
	if !result.Denied() {
		t.Error("100% transfer should be denied as drain")
	}
}

func TestCheckFinancialDrain_AmountAll(t *testing.T) {
	req := &ToolCallRequest{
		ToolName:  "transfer",
		Arguments: `{"amount":"all","target":"attacker"}`,
		Authority: core.AuthorityCreator,
	}
	result := CheckFinancialDrain(req)
	if !result.Denied() {
		t.Error("amount=all should be denied as drain")
	}
}

func TestCheckFinancialDrain_NormalTransferAllowed(t *testing.T) {
	req := &ToolCallRequest{
		ToolName:  "transfer",
		Arguments: `{"amount_dollars":10,"target":"friend"}`,
		Authority: core.AuthorityCreator,
	}
	result := CheckFinancialDrain(req)
	if result.Denied() {
		t.Errorf("normal transfer should be allowed, denied: %s", result.Reason)
	}
}

func TestCheckFinancialDrain_CaseInsensitive(t *testing.T) {
	req := &ToolCallRequest{
		ToolName:  "DRAIN_WALLET",
		Arguments: `{}`,
		Authority: core.AuthorityCreator,
	}
	result := CheckFinancialDrain(req)
	if !result.Denied() {
		t.Error("DRAIN_WALLET (uppercase) should be denied")
	}
}

// --- #67: Dynamic rule registration ---

// testRule is a simple configurable rule for testing dynamic registration.
type testRule struct {
	name     string
	priority int
	deny     bool
}

func (r *testRule) Name() string  { return r.name }
func (r *testRule) Priority() int { return r.priority }
func (r *testRule) Evaluate(_ *ToolCallRequest, _ *tools.Registry) DecisionResult {
	if r.deny {
		return Deny(r.name, "test denial")
	}
	return Allow()
}

func TestRegisterDynamic_InsertsInPriorityOrder(t *testing.T) {
	pe := NewEngine(DefaultConfig())
	originalCount := len(pe.Rules())

	pe.RegisterDynamic(&testRule{name: "early", priority: 0}, 0)
	pe.RegisterDynamic(&testRule{name: "late", priority: 100}, 100)

	rules := pe.Rules()
	if len(rules) != originalCount+2 {
		t.Fatalf("expected %d rules, got %d", originalCount+2, len(rules))
	}

	// "early" should be first (priority 0).
	if rules[0].Name() != "early" {
		t.Errorf("expected first rule to be 'early', got %q", rules[0].Name())
	}
	// "late" should be last (priority 100).
	if rules[len(rules)-1].Name() != "late" {
		t.Errorf("expected last rule to be 'late', got %q", rules[len(rules)-1].Name())
	}
}

func TestRegisterDynamic_DenyingRuleBlocksEvaluation(t *testing.T) {
	pe := NewEngine(DefaultConfig())

	// Register a denying rule at highest priority.
	pe.RegisterDynamic(&testRule{name: "blocker", priority: 0, deny: true}, 0)

	req := &ToolCallRequest{
		ToolName:  "echo",
		Arguments: `{"msg":"hi"}`,
		Authority: core.AuthorityCreator,
	}
	result := pe.Evaluate(req)
	if !result.Denied() {
		t.Error("dynamic denying rule should block evaluation")
	}
	if result.Rule != "blocker" {
		t.Errorf("expected 'blocker' rule, got %q", result.Rule)
	}
}

func TestRegisterDynamic_PriorityOverride(t *testing.T) {
	pe := NewEngine(DefaultConfig())

	// The testRule has priority 50 internally, but we register with priority 0.
	pe.RegisterDynamic(&testRule{name: "overridden", priority: 50}, 0)

	rules := pe.Rules()
	if rules[0].Name() != "overridden" {
		t.Errorf("expected overridden rule at index 0, got %q", rules[0].Name())
	}
	if rules[0].Priority() != 0 {
		t.Errorf("expected priority 0, got %d", rules[0].Priority())
	}
}

func TestRegisterDynamic_RulesSnapshot(t *testing.T) {
	pe := NewEngine(DefaultConfig())
	snap := pe.Rules()
	original := len(snap)

	// Mutating the snapshot should not affect the engine.
	snap = append(snap, &testRule{name: "rogue"})
	if len(pe.Rules()) != original {
		t.Error("mutating Rules() snapshot should not affect engine")
	}
}
