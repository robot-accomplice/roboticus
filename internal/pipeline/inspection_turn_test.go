package pipeline

import (
	"os"
	"strings"
	"testing"
)

func TestResolveInspectionTarget_DesktopVaultAlias(t *testing.T) {
	resolution := ResolveInspectionTarget(
		"Please give me the briefest summary you can of the contents of the vault on my Desktop",
		"/Users/jmachen/.roboticus/workspace",
		[]string{
			"/Users/jmachen/Desktop/My Vault",
			"/Users/jmachen/.roboticus/workspace/Vault",
		},
	)

	if resolution.ClarificationRequired {
		t.Fatal("desktop vault alias should resolve without clarification")
	}
	if len(resolution.ResolvedPaths) != 1 || resolution.ResolvedPaths[0] != "/Users/jmachen/Desktop/My Vault" {
		t.Fatalf("resolved paths = %v, want Desktop vault", resolution.ResolvedPaths)
	}
	if resolution.PromptSummary == "" {
		t.Fatal("resolved inspection target should produce a prompt summary")
	}
}

func TestResolveInspectionTarget_ExplicitAllowedPath(t *testing.T) {
	resolution := ResolveInspectionTarget(
		"what about a list of the projects in /Users/jmachen/code ?",
		"/Users/jmachen/.roboticus/workspace",
		[]string{
			"/Users/jmachen/Desktop/My Vault",
			"/Users/jmachen/code",
		},
	)

	if resolution.ClarificationRequired {
		t.Fatal("explicit allowed path should resolve without clarification")
	}
	if len(resolution.ResolvedPaths) != 1 || resolution.ResolvedPaths[0] != "/Users/jmachen/code" {
		t.Fatalf("resolved paths = %v, want /Users/jmachen/code", resolution.ResolvedPaths)
	}
}

func TestResolveInspectionTarget_PathClarificationFollowup(t *testing.T) {
	content := "are you sure about that? the vault in question is at /Users/jmachen/Desktop My Vault"
	if !looksLikeFocusedInspectionTurn(content) {
		t.Fatal("path clarification follow-up should stay on focused inspection path")
	}
	resolution := ResolveInspectionTarget(
		content,
		"/Users/jmachen/.roboticus/workspace",
		[]string{
			"/Users/jmachen/Desktop/My Vault",
			"/Users/jmachen/.roboticus/workspace/Vault",
		},
	)
	if resolution.ClarificationRequired {
		t.Fatal("path clarification follow-up should resolve without clarification")
	}
	if len(resolution.ResolvedPaths) != 1 || resolution.ResolvedPaths[0] != "/Users/jmachen/Desktop/My Vault" {
		t.Fatalf("resolved paths = %v, want /Users/jmachen/Desktop/My Vault", resolution.ResolvedPaths)
	}
}

func TestResolveInspectionTarget_AmbiguousVaultRequestsClarification(t *testing.T) {
	resolution := ResolveInspectionTarget(
		"what's in the vault?",
		"/Users/jmachen/.roboticus/workspace",
		[]string{
			"/Users/jmachen/Desktop/My Vault",
			"/Users/jmachen/.roboticus/workspace/Vault",
		},
	)

	if !resolution.ClarificationRequired {
		t.Fatal("generic vault question should require clarification")
	}
	if resolution.PromptSummary == "" {
		t.Fatal("ambiguous inspection target should still produce a clarification summary")
	}
}

func TestResolveInspectionTarget_CodeFolderAlias(t *testing.T) {
	content := "Duncan, what are the ten most recently update projects in my code folder?"
	if !looksLikeFocusedInspectionTurn(content) {
		t.Fatal("code folder inventory question should stay on focused inspection path")
	}
	resolution := ResolveInspectionTarget(
		content,
		"/Users/jmachen/.roboticus/workspace",
		[]string{
			"/Users/jmachen/Desktop/My Vault",
			"/Users/jmachen/code",
		},
	)
	if resolution.ClarificationRequired {
		t.Fatal("code folder alias should resolve without clarification")
	}
	if len(resolution.ResolvedPaths) != 1 || resolution.ResolvedPaths[0] != "/Users/jmachen/code" {
		t.Fatalf("resolved paths = %v, want /Users/jmachen/code", resolution.ResolvedPaths)
	}
}

func TestResolveInspectionTarget_TildeHomeAlias(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		t.Fatalf("UserHomeDir() failed: %v", err)
	}
	content := "give me the file distribution in the folder ~"
	if !looksLikeFocusedInspectionTurn(content) {
		t.Fatal("tilde home inspection should stay on focused inspection path")
	}
	resolution := ResolveInspectionTarget(
		content,
		"/Users/jmachen/.roboticus/workspace",
		[]string{
			"/Users/jmachen/Desktop/My Vault",
			"/Users/jmachen/code",
			home,
		},
	)
	if resolution.ClarificationRequired {
		t.Fatal("tilde home alias should resolve without clarification")
	}
	if len(resolution.ResolvedPaths) != 1 || resolution.ResolvedPaths[0] != home {
		t.Fatalf("resolved paths = %v, want %q", resolution.ResolvedPaths, home)
	}
}

func TestResolveInspectionTarget_TildeProjectDoesNotSwallowTrailingProse(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		t.Fatalf("UserHomeDir() failed: %v", err)
	}
	want := home + "/code/roboticus"
	content := "Please review all of the subdirectories associated with the project at ~/code/roboticus and try to locate the architecture documentation."
	if !looksLikeFocusedInspectionTurn(content) {
		t.Fatal("repo architecture review should stay on focused inspection path")
	}
	resolution := ResolveInspectionTarget(
		content,
		"/Users/jmachen/.roboticus/workspace",
		[]string{home + "/code"},
	)
	if resolution.ClarificationRequired {
		t.Fatal("tilde project path should resolve without clarification")
	}
	if len(resolution.ResolvedPaths) != 1 || resolution.ResolvedPaths[0] != want {
		t.Fatalf("resolved paths = %v, want %q", resolution.ResolvedPaths, want)
	}
	if strings.Contains(resolution.ResolvedPaths[0], " and ") {
		t.Fatalf("resolved path swallowed prose: %q", resolution.ResolvedPaths[0])
	}
}

func TestResolveFilesystemDestination_DesktopVaultAlias(t *testing.T) {
	resolution := ResolveFilesystemDestination(
		"write the report as a new document to my obsidian vault on my desktop",
		"/Users/jmachen/.roboticus/workspace",
		[]string{
			"/Users/jmachen/Desktop/My Vault",
			"/Users/jmachen/code",
			"/Users/jmachen/.roboticus/workspace/Vault",
		},
		"/Users/jmachen/.roboticus/workspace/Vault",
	)
	if resolution.ClarificationRequired {
		t.Fatal("desktop vault destination should resolve without clarification")
	}
	if resolution.ResolvedRoot != "/Users/jmachen/Desktop/My Vault" {
		t.Fatalf("resolved root = %q, want Desktop vault", resolution.ResolvedRoot)
	}
	if resolution.UseConfiguredVault {
		t.Fatal("desktop vault should not be treated as configured default vault")
	}
}

func TestResolveSourceCodeTarget_CurrentRepoRoot(t *testing.T) {
	content := "Refactor the configuration parser to support hot-reload with validation, rollback on failure, and emit structured change events."
	if !looksLikeSourceBackedCodeTask(content) {
		t.Fatal("source-backed refactor prompt should be detected")
	}
	resolution := ResolveSourceCodeTarget(
		content,
		"/Users/jmachen/code/roboticus-v1.0.8-verifier",
		[]string{
			"/Users/jmachen/Desktop/My Vault",
			"/Users/jmachen/code",
		},
	)
	if resolution.ResolvedRoot != "/Users/jmachen/code/roboticus-v1.0.8-verifier" {
		t.Fatalf("resolved root = %q", resolution.ResolvedRoot)
	}
	if resolution.PromptSummary == "" {
		t.Fatal("source-backed code target should produce a prompt summary")
	}
}
