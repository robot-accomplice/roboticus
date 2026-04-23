package pipeline

import (
	"strings"
	"testing"

	agenttools "roboticus/internal/agent/tools"
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
		t.Error("should reject false denial when tools actually ran")
	}
	if !result.Retry {
		t.Fatal("expected retry instead of canned rewrite for false denial")
	}
	if result.Content != "" {
		t.Fatalf("expected no canned content rewrite, got: %q", result.Content)
	}
}

func TestExecutionTruthGuard_AllowsRealPolicyDenial(t *testing.T) {
	g := &ExecutionTruthGuard{}
	ctx := &GuardContext{
		ToolResults: []ToolResultEntry{{ToolName: "bash", Output: "Policy denied: dangerous tools require self-generated or higher authority"}},
	}
	result := g.CheckWithContext("I can't execute that command because it requires higher authority.", ctx)
	if !result.Passed {
		t.Fatalf("expected pass for real policy denial, got reason: %s", result.Reason)
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

func TestExecutionTruthGuard_RejectsArtifactClaimWithoutArtifactWriteEvidence(t *testing.T) {
	g := &ExecutionTruthGuard{}
	ctx := &GuardContext{
		UserPrompt: "Create a new Obsidian note named codex-live-test.md in the vault containing exactly: # Codex Live Test.",
		Intents:    []string{"task"},
		ToolResults: []ToolResultEntry{
			{ToolName: "get_runtime_context", Output: "Workspace: /tmp/workspace"},
			{ToolName: "ingest_policy", Output: `{"ok":true,"summary":"ingested obsidian-note/codex-live-test.md v0"}`},
		},
	}
	result := g.CheckWithContext("I've successfully created the Obsidian note codex-live-test.md and stored it in the vault.", ctx)
	if result.Passed {
		t.Fatal("expected artifact claim without artifact-writing evidence to be rejected")
	}
	if !result.Retry {
		t.Fatal("expected retry for false artifact-creation claim")
	}
}

func TestExecutionTruthGuard_AllowsArtifactClaimWithArtifactWriteEvidence(t *testing.T) {
	g := &ExecutionTruthGuard{}
	proof := agenttools.NewArtifactProof("obsidian_note", "codex-live-test.md", "# Codex Live Test.", false)
	ctx := &GuardContext{
		UserPrompt: "Create a new Obsidian note named codex-live-test.md in the vault containing exactly: # Codex Live Test.",
		Intents:    []string{"task"},
		ToolResults: []ToolResultEntry{
			{ToolName: "obsidian_write", Output: proof.Output(), Metadata: proof.Metadata(), ArtifactProof: &proof},
		},
	}
	result := g.CheckWithContext("I've successfully created the Obsidian note codex-live-test.md.", ctx)
	if !result.Passed {
		t.Fatalf("artifact write evidence should pass, got reason: %s", result.Reason)
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
