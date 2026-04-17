package pipeline

import (
	"strings"
	"testing"
)

func TestModelIdentityTruthGuard_Rewrite(t *testing.T) {
	g := &ModelIdentityTruthGuard{}
	ctx := &GuardContext{
		Intents:       []string{"model_identity"},
		AgentName:     "Roboticus",
		ResolvedModel: "gpt-4",
	}
	result := g.CheckWithContext("I am a large language model.", ctx)
	if result.Passed {
		t.Error("should rewrite identity response")
	}
	if !strings.Contains(result.Content, "Roboticus") {
		t.Errorf("rewritten content should contain agent name, got: %s", result.Content)
	}
	if !strings.Contains(result.Content, "gpt-4") {
		t.Errorf("rewritten content should contain model name, got: %s", result.Content)
	}
}

func TestModelIdentityTruthGuard_NoIntent(t *testing.T) {
	g := &ModelIdentityTruthGuard{}
	ctx := &GuardContext{Intents: []string{"conversation"}}
	result := g.CheckWithContext("Hello there!", ctx)
	if !result.Passed {
		t.Error("non-identity intent should pass")
	}
}

func TestCurrentEventsTruthGuard_StaleDisclaimer(t *testing.T) {
	g := &CurrentEventsTruthGuard{}
	ctx := &GuardContext{Intents: []string{"current_events"}}
	result := g.CheckWithContext("As of my last training data, I cannot provide real-time updates.", ctx)
	if result.Passed {
		t.Error("should reject stale-knowledge disclaimer")
	}
	if !result.Retry {
		t.Error("should request retry")
	}
}

func TestCurrentEventsTruthGuard_NoDisclaimer(t *testing.T) {
	g := &CurrentEventsTruthGuard{}
	ctx := &GuardContext{Intents: []string{"current_events"}}
	result := g.CheckWithContext("Today's temperature in New York is 72F.", ctx)
	if !result.Passed {
		t.Error("response without disclaimer should pass")
	}
}

func TestExecutionTruthGuard_FalseExecution(t *testing.T) {
	g := &ExecutionTruthGuard{}
	ctx := &GuardContext{ToolResults: nil}
	result := g.CheckWithContext("I ran the command and the output was successful.", ctx)
	if result.Passed {
		t.Error("should reject false execution claim without tool results")
	}
}

func TestExecutionTruthGuard_DeniedCapability(t *testing.T) {
	g := &ExecutionTruthGuard{}
	ctx := &GuardContext{
		ToolResults: []ToolResultEntry{{ToolName: "bash", Output: "hello world"}},
	}
	result := g.CheckWithContext("I'm unable to execute commands on your system.", ctx)
	if result.Passed {
		t.Error("should rewrite denial when tools actually ran")
	}
	if !strings.Contains(result.Content, "bash") {
		t.Errorf("rewritten content should include tool results, got: %s", result.Content)
	}
}

func TestFilesystemDenialGuard_AllowsActualSandboxDenial(t *testing.T) {
	g := &FilesystemDenialGuard{}
	ctx := &GuardContext{
		ToolResults: []ToolResultEntry{
			{ToolName: "list_directory", Output: "error: absolute paths must be in allowed_paths list"},
		},
	}
	result := g.CheckWithContext("I can't access your files directly because that path is outside the allowed workspace.", ctx)
	if !result.Passed {
		t.Fatalf("expected pass for real sandbox denial, got reason: %s", result.Reason)
	}
}

func TestFilesystemDenialGuard_RewritesFalseDenialWhenToolsRan(t *testing.T) {
	g := &FilesystemDenialGuard{}
	ctx := &GuardContext{
		ToolResults: []ToolResultEntry{
			{ToolName: "list_directory", Output: "file-a\nfile-b"},
		},
	}
	result := g.CheckWithContext("I can't access your files directly, but I can still help conceptually.", ctx)
	if result.Passed {
		t.Fatal("expected false filesystem denial to be blocked")
	}
	if result.Retry && result.Content != "" {
		t.Fatalf("expected retry-only or rewrite-only result, got both retry=%v content=%q", result.Retry, result.Content)
	}
}

func TestExecutionTruthGuard_HonestExecution(t *testing.T) {
	g := &ExecutionTruthGuard{}
	ctx := &GuardContext{
		ToolResults: []ToolResultEntry{{ToolName: "bash", Output: "hello world"}},
	}
	result := g.CheckWithContext("The command completed. Here's the output: hello world", ctx)
	if !result.Passed {
		t.Error("honest execution claim should pass")
	}
}

func TestPersonalityIntegrityGuard_ForeignIdentity(t *testing.T) {
	g := &PersonalityIntegrityGuard{}
	result := g.Check("As an AI developed by OpenAI, I'm here to help. The answer is 42.")
	if result.Passed {
		t.Error("should strip foreign identity boilerplate")
	}
	if strings.Contains(strings.ToLower(result.Content), "openai") {
		t.Errorf("stripped content should not contain OpenAI, got: %s", result.Content)
	}
}

func TestPersonalityIntegrityGuard_Clean(t *testing.T) {
	g := &PersonalityIntegrityGuard{}
	result := g.Check("The answer to your question is 42.")
	if !result.Passed {
		t.Error("clean response should pass")
	}
}

func TestPersonalityIntegrityGuard_OnlyBoilerplate(t *testing.T) {
	g := &PersonalityIntegrityGuard{}
	result := g.Check("I am Claude, an AI assistant made by Anthropic.")
	if result.Passed {
		t.Error("pure boilerplate should trigger retry")
	}
	if !result.Retry {
		t.Error("should request retry when only boilerplate remains")
	}
}
