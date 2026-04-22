package agent

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"

	"roboticus/internal/core"
)

func TestBuildSystemPrompt_ContainsAgentName(t *testing.T) {
	cfg := PromptConfig{AgentName: "Roboticus"}
	prompt := BuildSystemPrompt(cfg)
	if !strings.Contains(prompt, "Roboticus") {
		t.Error("should contain agent name")
	}
}

func TestBuildSystemPrompt_ContainsVersion(t *testing.T) {
	cfg := PromptConfig{AgentName: "Bot", Version: "1.0.0"}
	prompt := BuildSystemPrompt(cfg)
	if !strings.Contains(prompt, "1.0.0") {
		t.Error("should contain version")
	}
}

func TestBuildSystemPrompt_ContainsFirmware(t *testing.T) {
	cfg := PromptConfig{AgentName: "Bot", Firmware: "Custom firmware text."}
	prompt := BuildSystemPrompt(cfg)
	if !strings.Contains(prompt, "Custom firmware text") {
		t.Error("should contain firmware")
	}
}

func TestBuildSystemPrompt_ContainsPersonality(t *testing.T) {
	cfg := PromptConfig{AgentName: "Bot", Personality: "Friendly and helpful."}
	prompt := BuildSystemPrompt(cfg)
	if !strings.Contains(prompt, "Friendly and helpful") {
		t.Error("should contain personality")
	}
}

func TestBuildSystemPrompt_SubagentOmitsPersonalityOperatorAndDirectives(t *testing.T) {
	cfg := PromptConfig{
		AgentName:   "Bot",
		Personality: "Friendly and helpful.",
		Operator:    "Operator prefers dry humor.",
		Directives:  "Optimize for operator rapport.",
		IsSubagent:  true,
	}
	prompt := BuildSystemPrompt(cfg)
	if strings.Contains(prompt, "## Identity") {
		t.Fatal("subagent prompt should not contain identity block")
	}
	if strings.Contains(prompt, "## Operator Context") {
		t.Fatal("subagent prompt should not contain operator context block")
	}
	if strings.Contains(prompt, "## Active Directives") {
		t.Fatal("subagent prompt should not contain active directives block")
	}
	if !strings.Contains(prompt, "specialist subagent") {
		t.Fatal("subagent prompt should contain orchestration block")
	}
	if !strings.Contains(prompt, "Report upward to the orchestrator layer, never directly to the operator") {
		t.Fatal("subagent prompt should enforce orchestrator-only reporting")
	}
	if !strings.Contains(prompt, "only claim completion when backed by concrete tool output") {
		t.Fatal("subagent prompt should require proof-backed completion claims")
	}
	if !strings.Contains(prompt, "Separate completed work, evidence, and remaining gaps or uncertainty") {
		t.Fatal("subagent prompt should require explicit evidence and gap reporting")
	}
}

func TestBuildSystemPrompt_Empty(t *testing.T) {
	cfg := PromptConfig{}
	prompt := BuildSystemPrompt(cfg)
	if prompt == "" {
		t.Error("should produce non-empty prompt even with empty config")
	}
}

func TestBuildSystemPrompt_SignedContainsBoundaryMarkers(t *testing.T) {
	cfg := PromptConfig{
		AgentName:   "TestBot",
		Firmware:    "Platform rules.",
		BoundaryKey: []byte("test-secret-key"),
	}
	prompt := BuildSystemPrompt(cfg)
	if !strings.Contains(prompt, "[BOUNDARY:") {
		t.Error("signed prompt should contain [BOUNDARY:...] markers")
	}
	// Should have one boundary per section. With AgentName + Firmware + Runtime +
	// ToolUse + Safety = 5 sections.
	count := strings.Count(prompt, "[BOUNDARY:")
	if count < 3 {
		t.Errorf("expected at least 3 boundary markers, got %d", count)
	}
}

func TestBuildSystemPrompt_UnsignedHasNoMarkers(t *testing.T) {
	cfg := PromptConfig{
		AgentName: "TestBot",
		Firmware:  "Rules.",
	}
	prompt := BuildSystemPrompt(cfg)
	if strings.Contains(prompt, "[BOUNDARY:") {
		t.Error("unsigned prompt (nil key) should not contain boundary markers")
	}
}

func TestBuildSystemPrompt_EmptyKeyNoMarkers(t *testing.T) {
	cfg := PromptConfig{
		AgentName:   "TestBot",
		BoundaryKey: []byte{},
	}
	prompt := BuildSystemPrompt(cfg)
	if strings.Contains(prompt, "[BOUNDARY:") {
		t.Error("empty key should not produce boundary markers")
	}
}

func TestBuildSystemPrompt_OperationalIntrospectionBlock(t *testing.T) {
	// L1 (default) uses compact introspection.
	cfg := PromptConfig{
		AgentName: "Bot",
		ToolNames: []string{"search", "read_file", "shell"},
	}
	prompt := BuildSystemPrompt(cfg)
	if !strings.Contains(prompt, "Introspection") {
		t.Error("should contain introspection block")
	}

	// L2+ uses full introspection with tool listing.
	cfg.BudgetTier = 2
	prompt = BuildSystemPrompt(cfg)
	if !strings.Contains(prompt, "Operational Introspection") {
		t.Error("L2 should contain full operational introspection block")
	}
	if !strings.Contains(prompt, "3 tools") {
		t.Error("L2 should list tool count")
	}
	if !strings.Contains(prompt, "use that first") {
		t.Error("L2 should treat injected current-turn evidence as the first memory authority")
	}
	if !strings.Contains(prompt, "effective path constraints") {
		t.Error("introspection should reference effective path constraints instead of vague policy")
	}
	if strings.Contains(prompt, "ALWAYS call `recall_memory` to search — even if injected memories are present") {
		t.Error("L2 should not instruct unconditional memory re-search when injected evidence exists")
	}
}

func TestBuildSystemPrompt_RuntimeMetadataBlock(t *testing.T) {
	cfg := PromptConfig{
		AgentName: "Bot",
		Model:     "gpt-4",
		Workspace: "/home/user/project",
		Version:   "0.11.4",
	}
	prompt := BuildSystemPrompt(cfg)
	if !strings.Contains(prompt, "Runtime Context") {
		t.Error("should contain runtime context block")
	}
	if !strings.Contains(prompt, "gpt-4") {
		t.Error("should contain model name in runtime context")
	}
	if !strings.Contains(prompt, "/home/user/project") {
		t.Error("should contain workspace in runtime context")
	}
}

func TestBuildSystemPrompt_InspectionTargetSummary(t *testing.T) {
	cfg := PromptConfig{
		AgentName:        "Bot",
		InspectionTarget: "This turn is a filesystem inspection request. Resolved target path: /Users/jmachen/Desktop/My Vault. Inspect this target directly with list_directory before answering.",
	}
	prompt := BuildSystemPrompt(cfg)
	if !strings.Contains(prompt, "## Inspection Target") {
		t.Fatal("prompt should include inspection target block")
	}
	if !strings.Contains(prompt, "/Users/jmachen/Desktop/My Vault") {
		t.Fatal("prompt should include resolved inspection target path")
	}
}

func TestBuildSystemPrompt_DestinationTargetSummary(t *testing.T) {
	cfg := PromptConfig{
		AgentName:         "Bot",
		ToolNames:         []string{"write_file", "get_runtime_context"},
		DestinationTarget: "This turn requests authoring into the allowlisted destination root /Users/jmachen/Desktop/My Vault. This path is writable. Because it is not the configured default Obsidian vault, use write_file with an absolute path under this root.",
	}
	prompt := BuildSystemPrompt(cfg)
	if !strings.Contains(prompt, "## Destination Target") {
		t.Fatal("prompt should include destination target block")
	}
	if !strings.Contains(prompt, "/Users/jmachen/Desktop/My Vault") {
		t.Fatal("prompt should include resolved destination path")
	}
	if !strings.Contains(prompt, "`write_file` and `edit_file` may target") {
		t.Fatal("prompt should advertise absolute-allowed write surface")
	}
}

func TestBuildSystemPrompt_FocusedAnalysisAuthoringContract(t *testing.T) {
	cfg := PromptConfig{
		AgentName:         "Bot",
		ToolProfile:       "focused_analysis_authoring",
		ToolNames:         []string{"inventory_projects", "bash", "write_file", "list_directory"},
		InspectionTarget:  "Resolved target path: /Users/jmachen/code",
		DestinationTarget: "Resolved destination root: /Users/jmachen/Desktop/My Vault",
	}
	prompt := BuildSystemPrompt(cfg)
	if !strings.Contains(prompt, "## Analysis + Authoring Contract") {
		t.Fatal("prompt should include focused analysis+authoring contract")
	}
	if !strings.Contains(prompt, "Do not fill required columns with `--`, `Unknown`, or placeholders") {
		t.Fatal("prompt should forbid placeholder-heavy incomplete reports")
	}
	if !strings.Contains(prompt, "Prefer `inventory_projects` for project-root reports") {
		t.Fatal("prompt should prefer the first-class project inventory tool")
	}
	if !strings.Contains(prompt, "Use `bash` when filesystem or git metadata is most naturally derived from shell commands.") {
		t.Fatal("prompt should guide bash usage for metadata-heavy analysis turns")
	}
	if !strings.Contains(prompt, "prefer one structured `bash` inventory pass over repeated extension-specific globbing") {
		t.Fatal("prompt should discourage extension-globbing heuristics on project reports")
	}
	if !strings.Contains(prompt, "treat the immediate child directories of the inspection target as candidate project roots") {
		t.Fatal("prompt should steer project-directory reports toward immediate child project roots")
	}
}

func TestBuildSystemPrompt_OmitsBashGuidanceWhenBashIsNotSelected(t *testing.T) {
	cfg := PromptConfig{
		AgentName: "Bot",
		ToolNames: []string{"read_file", "write_file", "get_runtime_context"},
	}
	prompt := BuildSystemPrompt(cfg)
	if strings.Contains(prompt, "`bash` executes in the workspace root") {
		t.Error("should not mention bash runtime guidance when bash is not selected")
	}
	if strings.Contains(prompt, "real shell access") {
		t.Error("should not claim shell access when bash is not selected")
	}
	if !strings.Contains(prompt, "attribute it to the tool that produced it") {
		t.Error("should include generic tool attribution guidance when bash is not selected")
	}
}

func TestBuildSystemPrompt_BashGuidanceUsesEffectivePathConstraints(t *testing.T) {
	cfg := PromptConfig{
		AgentName: "Bot",
		ToolNames: []string{"bash", "get_runtime_context"},
	}
	prompt := BuildSystemPrompt(cfg)
	if !strings.Contains(prompt, "effective path constraints") {
		t.Fatalf("prompt should mention effective path constraints, got %q", prompt)
	}
	if strings.Contains(prompt, "allowed paths and security policy") {
		t.Fatalf("prompt should not overclaim security policy visibility, got %q", prompt)
	}
}

func TestBuildSystemPrompt_ObsidianDirective_Enabled(t *testing.T) {
	cfg := PromptConfig{
		AgentName: "Bot",
		Obsidian: &core.ObsidianConfig{
			Enabled:   true,
			VaultPath: "/home/user/vault",
		},
	}
	prompt := BuildSystemPrompt(cfg)
	if !strings.Contains(prompt, "Obsidian Integration") {
		t.Error("should contain Obsidian directive when enabled")
	}
	if !strings.Contains(prompt, "/home/user/vault") {
		t.Error("should contain vault path")
	}
	if !strings.Contains(prompt, "obsidian_write") {
		t.Error("should mention the explicit obsidian_write tool")
	}
}

func TestBuildSystemPrompt_ObsidianDirective_Disabled(t *testing.T) {
	cfg := PromptConfig{
		AgentName: "Bot",
		Obsidian: &core.ObsidianConfig{
			Enabled:   false,
			VaultPath: "/home/user/vault",
		},
	}
	prompt := BuildSystemPrompt(cfg)
	if strings.Contains(prompt, "Obsidian Integration") {
		t.Error("should not contain Obsidian directive when disabled")
	}
}

func TestBuildSystemPrompt_ObsidianDirective_Nil(t *testing.T) {
	cfg := PromptConfig{AgentName: "Bot"}
	prompt := BuildSystemPrompt(cfg)
	if strings.Contains(prompt, "Obsidian Integration") {
		t.Error("should not contain Obsidian directive when nil")
	}
}

func TestSignBoundary_Deterministic(t *testing.T) {
	key := []byte("determinism-key")
	content := "Hello, world!"
	a := signBoundary(key, content)
	b := signBoundary(key, content)
	if a != b {
		t.Errorf("signBoundary should be deterministic: %q != %q", a, b)
	}
	// Verify format.
	if !strings.HasPrefix(a, "[BOUNDARY:") || !strings.HasSuffix(a, "]") {
		t.Errorf("unexpected format: %q", a)
	}
	// Verify the hex inside is a valid HMAC-SHA256 (64 hex chars).
	inner := a[len("[BOUNDARY:") : len(a)-1]
	if len(inner) != 64 {
		t.Errorf("expected 64 hex chars, got %d", len(inner))
	}
	decoded, err := hex.DecodeString(inner)
	if err != nil {
		t.Fatalf("hex decode failed: %v", err)
	}
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(content))
	expected := mac.Sum(nil)
	if !hmac.Equal(decoded, expected) {
		t.Error("HMAC mismatch in signBoundary output")
	}
}
