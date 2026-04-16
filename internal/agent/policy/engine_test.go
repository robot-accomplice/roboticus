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

func TestPolicy_BlocksHomeShortcutInWorkspaceOnlyMode(t *testing.T) {
	cfg := DefaultConfig()
	cfg.WorkspaceOnly = true
	pe := NewEngine(cfg)
	req := &ToolCallRequest{
		ToolName:  "bash",
		Arguments: `{"command":"find ~/Downloads -maxdepth 1 -type f"}`,
		Authority: core.AuthorityCreator,
	}
	result := pe.Evaluate(req)
	if !result.Denied() {
		t.Fatal("expected home-directory shortcut to be denied")
	}
	if result.Rule != "path_protection" {
		t.Fatalf("rule = %s, want path_protection", result.Rule)
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

// --- Config Protection Rule tests ---

// TestConfigProtectionRule_Direct tests the configProtectionRule in isolation
// (not through the full engine, since path_protection fires first on roboticus.toml).
func TestConfigProtectionRule_Direct(t *testing.T) {
	rule := &configProtectionRule{}
	tests := []struct {
		name   string
		tool   string
		args   string
		denied bool
	}{
		{"write_file with api_key in config", "write_file", `{"path":"roboticus.toml","content":"api_key = \"secret\""}`, true},
		{"write_file with admin_token in overrides", "write_file", `{"path":"config-overrides.toml","content":"admin_token = \"abc\""}`, true},
		{"write_file with scope_mode", "write_file", `{"path":"roboticus.toml","content":"scope_mode = \"open\""}`, true},
		{"write_file with keystore", "write_file", `{"path":"roboticus.toml","content":"keystore = \"/tmp\""}`, true},
		{"write_file with trusted_proxy", "write_file", `{"path":"config-overrides.toml","content":"trusted_proxy = \"*\""}`, true},
		{"write_file with private_key", "write_file", `{"path":"roboticus.toml","content":"private_key = \"0x...\""}`, true},
		{"write_file with _secret suffix", "write_file", `{"path":"roboticus.toml","content":"db_secret = \"pass\""}`, true},
		{"write_file with _token suffix", "write_file", `{"path":"roboticus.toml","content":"refresh_token = \"tok\""}`, true},
		{"bash with config and key", "bash", `{"command":"echo api_key > roboticus.toml"}`, true},
		{"run_script with config and key", "run_script", `{"script":"sed -i s/old/api_key/ roboticus.toml"}`, true},
		{"write_file to non-config", "write_file", `{"path":"data/notes.txt","content":"api_key = x"}`, false},
		{"write_file config without protected field", "write_file", `{"path":"roboticus.toml","content":"agent_name = \"bot\""}`, false},
		{"read_file config (not a write tool)", "read_file", `{"path":"roboticus.toml"}`, false},
		{"echo (not a write tool)", "echo", `{"message":"roboticus.toml api_key"}`, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := &ToolCallRequest{
				ToolName:  tc.tool,
				Arguments: tc.args,
				Authority: core.AuthorityCreator,
			}
			result := rule.Evaluate(req, nil)
			if tc.denied && !result.Denied() {
				t.Error("expected denial")
			}
			if !tc.denied && result.Denied() {
				t.Errorf("expected allow, denied by %s: %s", result.Rule, result.Reason)
			}
			if tc.denied && result.Rule != "config_protection" {
				t.Errorf("expected config_protection rule, got %s", result.Rule)
			}
		})
	}
}

func TestPolicy_ConfigProtection_AllowsNonConfigWrites(t *testing.T) {
	pe := NewEngine(DefaultConfig())
	req := &ToolCallRequest{
		ToolName:  "write_file",
		Arguments: `{"path":"data/notes.txt","content":"hello world"}`,
		Authority: core.AuthorityCreator,
	}
	result := pe.Evaluate(req)
	if result.Denied() {
		t.Errorf("non-config write should be allowed, denied by %s: %s", result.Rule, result.Reason)
	}
}

func TestPolicy_ConfigProtection_DeniedByEngineForConfigWrites(t *testing.T) {
	// Through the full engine, writes to config files with protected fields are
	// always denied (may be by path_protection or config_protection depending on overlap).
	pe := NewEngine(DefaultConfig())
	req := &ToolCallRequest{
		ToolName:  "write_file",
		Arguments: `{"path":"config-overrides.toml","content":"admin_token = \"abc\""}`,
		Authority: core.AuthorityCreator,
	}
	result := pe.Evaluate(req)
	if !result.Denied() {
		t.Error("write to config-overrides.toml with admin_token should be denied")
	}
}

func TestPolicy_ConfigProtection_IgnoresNonWriteTools(t *testing.T) {
	rule := &configProtectionRule{}
	req := &ToolCallRequest{
		ToolName:  "read_file",
		Arguments: `{"path":"roboticus.toml","content":"api_key"}`,
		Authority: core.AuthorityCreator,
	}
	result := rule.Evaluate(req, nil)
	if result.Denied() {
		t.Error("config_protection should not fire for non-write tools")
	}
}

func TestPolicy_ConfigProtectionRule_Priority(t *testing.T) {
	rule := &configProtectionRule{}
	if rule.Priority() != 7 {
		t.Errorf("priority = %d, want 7", rule.Priority())
	}
	if rule.Name() != "config_protection" {
		t.Errorf("name = %q", rule.Name())
	}
}

func TestPolicy_ConfigProtectionRule_RegisteredInEngine(t *testing.T) {
	pe := NewEngine(DefaultConfig())
	rules := pe.Rules()
	found := false
	for _, r := range rules {
		if r.Name() == "config_protection" {
			found = true
			break
		}
	}
	if !found {
		t.Error("config_protection rule should be registered in default engine")
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
