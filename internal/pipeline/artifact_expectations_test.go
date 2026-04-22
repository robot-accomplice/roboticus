package pipeline

import (
	"testing"

	agenttools "roboticus/internal/agent/tools"
)

func TestParseExpectedArtifactSpecs_InlineMultiArtifact(t *testing.T) {
	prompt := "Create the following files:\n- tmp/procedural-canary/rollout-config.json containing exactly:\n{\n  \"service\": \"auth\"\n}\n- tmp/procedural-canary/rollout-runbook.md containing exactly:\n# Rollout Runbook\n\n1. Read [[rollout-config.json]].\n"

	specs := ParseExpectedArtifactSpecs(prompt)
	if len(specs) != 2 {
		t.Fatalf("specs = %d, want 2", len(specs))
	}
	if specs[0].Path != "tmp/procedural-canary/rollout-config.json" {
		t.Fatalf("first path = %q", specs[0].Path)
	}
	if specs[1].Path != "tmp/procedural-canary/rollout-runbook.md" {
		t.Fatalf("second path = %q", specs[1].Path)
	}
	if specs[0].ExactContent != "{\n  \"service\": \"auth\"\n}" {
		t.Fatalf("first content = %q", specs[0].ExactContent)
	}
}

func TestParseExpectedArtifactSpecs_OrdinalMultiArtifact(t *testing.T) {
	prompt := "In the Obsidian vault, create two notes: project-bootstrap-check.md and project-bootstrap-actions.md. The first note should contain exactly: # Project Bootstrap Check. The second note should contain exactly: # Project Bootstrap Actions."

	specs := ParseExpectedArtifactSpecs(prompt)
	if len(specs) != 2 {
		t.Fatalf("specs = %d, want 2", len(specs))
	}
	if specs[0].Path != "project-bootstrap-check.md" || specs[1].Path != "project-bootstrap-actions.md" {
		t.Fatalf("paths = %#v", specs)
	}
	if specs[0].ExactContent != "# Project Bootstrap Check." {
		t.Fatalf("first content = %q", specs[0].ExactContent)
	}
}

func TestParseExpectedArtifactSpecs_WithContentMultiArtifact(t *testing.T) {
	prompt := "Create two files in tmp/procedural-canary-8/ exactly as follows. File 1: rollout-config.json with content:\n{\n  \"service\": \"billing\",\n  \"strategy\": \"canary\",\n  \"steps\": 3\n}\nFile 2: rollout-runbook.md with content:\n# Billing Canary Runbook\n1. Deploy canary to 10% of traffic.\n2. Check error rate and latency for 15 minutes.\n3. If metrics are healthy, advance to 50%.\n4. If metrics regress, roll back immediately.\nAfter writing both files, tell me exactly what happened. Do not claim success if the file contents do not match the specification."

	specs := ParseExpectedArtifactSpecs(prompt)
	if len(specs) != 2 {
		t.Fatalf("specs = %d, want 2", len(specs))
	}
	if specs[0].Path != "tmp/procedural-canary-8/rollout-config.json" || specs[1].Path != "tmp/procedural-canary-8/rollout-runbook.md" {
		t.Fatalf("paths = %#v", specs)
	}
	if specs[0].ExactContent != "{\n  \"service\": \"billing\",\n  \"strategy\": \"canary\",\n  \"steps\": 3\n}" {
		t.Fatalf("first content = %q", specs[0].ExactContent)
	}
	if specs[1].ExactContent != "# Billing Canary Runbook\n1. Deploy canary to 10% of traffic.\n2. Check error rate and latency for 15 minutes.\n3. If metrics are healthy, advance to 50%.\n4. If metrics regress, roll back immediately." {
		t.Fatalf("second content = %q", specs[1].ExactContent)
	}
}

func TestCompareArtifactConformance_FindsMismatch(t *testing.T) {
	expected := []ExpectedArtifactSpec{{
		Path:         "tmp/out.txt",
		ArtifactKind: "workspace_file",
		ExactContent: "hello",
	}}
	proofs := []agenttools.ArtifactProof{
		agenttools.NewArtifactProof("workspace_file", "tmp/out.txt", "goodbye", false),
	}

	conformance := CompareArtifactConformance(expected, proofs)
	if !conformance.HasUnsatisfied() {
		t.Fatal("expected unsatisfied conformance")
	}
	if len(conformance.Mismatched) != 1 {
		t.Fatalf("mismatched = %d, want 1", len(conformance.Mismatched))
	}
}

func TestCompareArtifactConformance_LiveProceduralCanaryMatches(t *testing.T) {
	prompt := "Create the following files exactly as specified in the workspace.\n- tmp/procedural-canary-3/rollout-config.json containing exactly:\n{\n  \"service\": \"auth\",\n  \"strategy\": \"canary\",\n  \"max_percent\": 25\n}\n- tmp/procedural-canary-3/rollout-runbook.md containing exactly:\n# Rollout Runbook\n\n1. Read [[rollout-config.json]].\n2. Validate the canary percent.\n3. Confirm the deployment target.\n\nConfig: `tmp/procedural-canary-3/rollout-config.json`"
	specs := ParseExpectedArtifactSpecs(prompt)
	if len(specs) != 2 {
		t.Fatalf("specs = %d, want 2", len(specs))
	}

	proofs := []agenttools.ArtifactProof{
		agenttools.NewArtifactProof("workspace_file", "tmp/procedural-canary-3/rollout-config.json", "{\n  \"service\": \"auth\",\n  \"strategy\": \"canary\",\n  \"max_percent\": 25\n}", false),
		agenttools.NewArtifactProof("workspace_file", "tmp/procedural-canary-3/rollout-runbook.md", "# Rollout Runbook\n\n1. Read [[rollout-config.json]].\n2. Validate the canary percent.\n3. Confirm the deployment target.\n\nConfig: `tmp/procedural-canary-3/rollout-config.json`", false),
	}

	conformance := CompareArtifactConformance(specs, proofs)
	if !conformance.AllExactSatisfied() {
		t.Fatalf("conformance = %+v", conformance)
	}
}
