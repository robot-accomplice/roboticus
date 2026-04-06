package policy

import (
	"context"
	"encoding/json"
	"testing"

	"roboticus/internal/agent/tools"
	"roboticus/internal/core"
)

func TestPolicy_AllowsNormalCall(t *testing.T) {
	pe := NewEngine(DefaultConfig())
	req := &ToolCallRequest{
		ToolName:  "echo",
		Arguments: `{"message":"hello"}`,
		Authority: core.AuthorityCreator,
	}
	result := pe.Evaluate(req)
	if result.Denied() {
		t.Errorf("normal call should be allowed, denied by %s: %s", result.Rule, result.Reason)
	}
}

func TestPolicy_BlocksPathTraversal(t *testing.T) {
	pe := NewEngine(DefaultConfig())
	req := &ToolCallRequest{
		ToolName:  "read_file",
		Arguments: `{"path":"../../etc/passwd"}`,
		Authority: core.AuthorityCreator,
	}
	result := pe.Evaluate(req)
	if !result.Denied() {
		t.Error("path traversal should be denied")
	}
	if result.Rule != "path_protection" {
		t.Errorf("expected path_protection rule, got %s", result.Rule)
	}
}

func TestPolicy_BlocksProtectedPaths(t *testing.T) {
	pe := NewEngine(DefaultConfig())
	req := &ToolCallRequest{
		ToolName:  "read_file",
		Arguments: `{"path":".env"}`,
		Authority: core.AuthorityCreator,
	}
	result := pe.Evaluate(req)
	if !result.Denied() {
		t.Error("access to .env should be denied")
	}
}

func TestPolicy_BlocksShellInjection(t *testing.T) {
	pe := NewEngine(DefaultConfig())
	req := &ToolCallRequest{
		ToolName:  "bash",
		Arguments: `{"command":"echo $(whoami)"}`,
		Authority: core.AuthorityCreator,
	}
	result := pe.Evaluate(req)
	if !result.Denied() {
		t.Error("shell injection should be denied")
	}
	// Could be caught by either path_protection or validation.
	if result.Rule != "validation" && result.Rule != "path_protection" {
		t.Errorf("expected validation or path_protection rule, got %s", result.Rule)
	}
}

func TestPolicy_BlocksLargeFinancialTransfer(t *testing.T) {
	pe := NewEngine(DefaultConfig())
	req := &ToolCallRequest{
		ToolName:  "transfer_funds",
		Arguments: `{"amount_dollars":500}`,
		Authority: core.AuthorityCreator,
	}
	result := pe.Evaluate(req)
	if !result.Denied() {
		t.Error("$500 transfer should exceed $100 limit")
	}
	if result.Rule != "financial" {
		t.Errorf("expected financial rule, got %s", result.Rule)
	}
}

func TestPolicy_AllowsSmallFinancialTransfer(t *testing.T) {
	pe := NewEngine(DefaultConfig())
	req := &ToolCallRequest{
		ToolName:  "transfer_funds",
		Arguments: `{"amount_dollars":50}`,
		Authority: core.AuthorityCreator,
	}
	result := pe.Evaluate(req)
	if result.Denied() {
		t.Errorf("$50 transfer should be allowed, denied by %s: %s", result.Rule, result.Reason)
	}
}

func TestPolicy_AuthorityGating(t *testing.T) {
	pe := NewEngine(DefaultConfig())
	reg := tools.NewRegistry()
	reg.Register(&mockToolAdapter{name: "dangerous_tool", risk: tools.RiskDangerous})

	// External authority should be denied for dangerous tools.
	req := &ToolCallRequest{
		ToolName:  "dangerous_tool",
		Arguments: `{}`,
		Authority: core.AuthorityExternal,
	}
	result := pe.EvaluateWithTools(req, reg)
	if !result.Denied() {
		t.Error("external authority should be denied for dangerous tools")
	}

	// Creator should be allowed.
	req.Authority = core.AuthorityCreator
	result = pe.EvaluateWithTools(req, reg)
	if result.Denied() {
		t.Errorf("creator should be allowed, denied by %s: %s", result.Rule, result.Reason)
	}
}

// mockToolAdapter implements tools.Tool for testing.
type mockToolAdapter struct {
	name string
	risk tools.RiskLevel
}

func (m *mockToolAdapter) Name() string                     { return m.name }
func (m *mockToolAdapter) Description() string              { return "mock" }
func (m *mockToolAdapter) Risk() tools.RiskLevel            { return m.risk }
func (m *mockToolAdapter) ParameterSchema() json.RawMessage { return nil }
func (m *mockToolAdapter) Execute(_ context.Context, _ string, _ *tools.Context) (*tools.Result, error) {
	return &tools.Result{Output: "ok"}, nil
}
