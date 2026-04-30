package pipeline

import "testing"

func TestConfigProtectionGuard_BlocksSensitiveKey(t *testing.T) {
	g := &ConfigProtectionGuard{}
	ctx := &GuardContext{
		ToolResults: []ToolResultEntry{
			{ToolName: "update_config", Output: "updated api_key to sk-test-123"},
		},
	}

	result := g.CheckWithContext("I've updated the API key as requested.", ctx)
	if result.Passed {
		t.Fatal("expected guard to block config mutation of api_key")
	}
	if !result.Blocked {
		t.Fatal("expected guard to structurally block config mutation")
	}
	if result.Content != "" {
		t.Fatalf("expected no canned replacement content, got %q", result.Content)
	}
	if result.ContractEvent.ContractID != "config_protection" {
		t.Fatalf("contract id = %q, want config_protection", result.ContractEvent.ContractID)
	}
	if result.ContractEvent.ContractGroup != "security" {
		t.Fatalf("contract group = %q, want security", result.ContractEvent.ContractGroup)
	}
}

func TestConfigProtectionGuard_BlocksKeystoreMutation(t *testing.T) {
	g := &ConfigProtectionGuard{}
	ctx := &GuardContext{
		ToolResults: []ToolResultEntry{
			{ToolName: "write_setting", Output: "keystore.path changed to /tmp/keys"},
		},
	}

	result := g.CheckWithContext("Done, keystore path updated.", ctx)
	if result.Passed {
		t.Fatal("expected guard to block keystore mutation")
	}
}

func TestConfigProtectionGuard_BlocksTrustedProxyMutation(t *testing.T) {
	g := &ConfigProtectionGuard{}
	ctx := &GuardContext{
		ToolResults: []ToolResultEntry{
			{ToolName: "config_set", Output: "trusted_proxy=*"},
		},
	}

	result := g.CheckWithContext("Trusted proxy updated.", ctx)
	if result.Passed {
		t.Fatal("expected guard to block trusted_proxy mutation")
	}
}

func TestConfigProtectionGuard_AllowsNonSensitiveConfig(t *testing.T) {
	g := &ConfigProtectionGuard{}
	ctx := &GuardContext{
		ToolResults: []ToolResultEntry{
			{ToolName: "update_config", Output: "updated agent.name to MyBot"},
		},
	}

	result := g.CheckWithContext("Agent name updated.", ctx)
	if !result.Passed {
		t.Fatalf("expected non-sensitive config change to pass, got reason: %s", result.Reason)
	}
}

func TestConfigProtectionGuard_PassesWithoutToolResults(t *testing.T) {
	g := &ConfigProtectionGuard{}
	ctx := &GuardContext{}

	result := g.CheckWithContext("Some response.", ctx)
	if !result.Passed {
		t.Fatal("expected pass with no tool results")
	}
}

func TestConfigProtectionGuard_PassesWithNilContext(t *testing.T) {
	g := &ConfigProtectionGuard{}

	result := g.CheckWithContext("Some response.", nil)
	if !result.Passed {
		t.Fatal("expected pass with nil context")
	}
}

func TestConfigProtectionGuard_IgnoresNonConfigTools(t *testing.T) {
	g := &ConfigProtectionGuard{}
	ctx := &GuardContext{
		ToolResults: []ToolResultEntry{
			{ToolName: "read_file", Output: "api_key = sk-old-value"},
		},
	}

	result := g.CheckWithContext("The file contains an API key.", ctx)
	if !result.Passed {
		t.Fatal("expected pass for non-config tool even with sensitive content")
	}
}
