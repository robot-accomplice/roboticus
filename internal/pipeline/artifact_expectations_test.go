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

func TestCompareArtifactClaims_FlagsInventedExtraFile(t *testing.T) {
	expected := []ExpectedArtifactSpec{
		{ArtifactKind: "workspace_file", Path: "tmp/check/alpha.txt", ExactContent: "ALPHA"},
		{ArtifactKind: "workspace_file", Path: "tmp/check/beta.txt", ExactContent: "BETA"},
	}
	proofs := []agenttools.ArtifactProof{
		agenttools.NewArtifactProof("workspace_file", "tmp/check/alpha.txt", "ALPHA", false),
		agenttools.NewArtifactProof("workspace_file", "tmp/check/beta.txt", "BETA", false),
	}

	conformance := CompareArtifactClaims("I created alpha.txt, beta.txt, and gamma.txt.", expected, nil, proofs, nil, "Create exactly two files")
	if !conformance.HasUnsupported() {
		t.Fatalf("expected unsupported claimed artifact, got %+v", conformance)
	}
	if len(conformance.UnsupportedClaim) != 1 || conformance.UnsupportedClaim[0] != "gamma.txt" {
		t.Fatalf("unsupported claims = %+v", conformance.UnsupportedClaim)
	}
}

func TestCompareArtifactClaims_IgnoresInspectionListingWithoutArtifactContract(t *testing.T) {
	conformance := CompareArtifactClaims("The vault contains alpha.txt, beta.txt, and gamma.txt.", nil, nil, nil, nil, "What's in the vault right now?")
	if conformance.HasUnsupported() {
		t.Fatalf("unexpected unsupported claims = %+v", conformance.UnsupportedClaim)
	}
	if len(conformance.Claimed) != 0 {
		t.Fatalf("claimed paths = %+v, want none without authoring contract", conformance.Claimed)
	}
}

func TestParseArtifactPromptContract_ClassifiesSourceInputsSeparately(t *testing.T) {
	prompt := "Read tmp/procedural-workflow-1/requirements.txt, then create exactly two files in tmp/procedural-workflow-1/: deploy-config.json with content:\n{}\nFile 2: rollout-runbook.md with content:\n# Runbook"
	contract := ParseArtifactPromptContract(prompt)
	if len(contract.ExpectedOutputs) != 2 {
		t.Fatalf("expected outputs = %d, want 2", len(contract.ExpectedOutputs))
	}
	if len(contract.SourceInputs) != 1 || contract.SourceInputs[0] != "tmp/procedural-workflow-1/requirements.txt" {
		t.Fatalf("source inputs = %+v", contract.SourceInputs)
	}
}

func TestParseArtifactPromptContract_ClassifiesSourceInputsWithNoColonContentDirectives(t *testing.T) {
	prompt := "Read tmp/procedural-workflow-4/requirements.txt and then create tmp/procedural-workflow-4/deploy-config.json with content {\"service\":\"payments-api\",\"environment\":\"staging\",\"strategy\":\"rolling\"} and create tmp/procedural-workflow-4/rollout-runbook.md with content # Rollout Runbook\n\n1. Deploy payments-api to staging.\n2. Use a rolling strategy.\n3. Verify health checks before promotion.\n"
	contract := ParseArtifactPromptContract(prompt)
	if len(contract.ExpectedOutputs) != 2 {
		t.Fatalf("expected outputs = %d, want 2", len(contract.ExpectedOutputs))
	}
	if contract.ExpectedOutputs[0].Path != "tmp/procedural-workflow-4/deploy-config.json" || contract.ExpectedOutputs[1].Path != "tmp/procedural-workflow-4/rollout-runbook.md" {
		t.Fatalf("paths = %#v", contract.ExpectedOutputs)
	}
	if contract.ExpectedOutputs[0].ExactContent != "{\"service\":\"payments-api\",\"environment\":\"staging\",\"strategy\":\"rolling\"}" {
		t.Fatalf("first content = %q", contract.ExpectedOutputs[0].ExactContent)
	}
	if contract.ExpectedOutputs[1].ExactContent != "# Rollout Runbook\n\n1. Deploy payments-api to staging.\n2. Use a rolling strategy.\n3. Verify health checks before promotion." {
		t.Fatalf("second content = %q", contract.ExpectedOutputs[1].ExactContent)
	}
	if len(contract.SourceInputs) != 1 || contract.SourceInputs[0] != "tmp/procedural-workflow-4/requirements.txt" {
		t.Fatalf("source inputs = %+v", contract.SourceInputs)
	}
}

func TestCompareArtifactClaims_AllowsSourceArtifactReference(t *testing.T) {
	expected := []ExpectedArtifactSpec{
		{ArtifactKind: "workspace_file", Path: "tmp/procedural-workflow-1/deploy-config.json", ExactContent: "{}"},
		{ArtifactKind: "workspace_file", Path: "tmp/procedural-workflow-1/rollout-runbook.md", ExactContent: "# Runbook"},
	}
	proofs := []agenttools.ArtifactProof{
		agenttools.NewArtifactProof("workspace_file", "tmp/procedural-workflow-1/deploy-config.json", "{}", false),
		agenttools.NewArtifactProof("workspace_file", "tmp/procedural-workflow-1/rollout-runbook.md", "# Runbook", false),
	}

	conformance := CompareArtifactClaims("I read requirements.txt and created deploy-config.json and rollout-runbook.md.", expected, []string{"tmp/procedural-workflow-1/requirements.txt"}, proofs, nil, "Read tmp/procedural-workflow-1/requirements.txt, then create files")
	if conformance.HasUnsupported() {
		t.Fatalf("unexpected unsupported claims: %+v", conformance.UnsupportedClaim)
	}
}

func TestCompareArtifactClaims_IgnoresInspectionListingWithInspectionEvidence(t *testing.T) {
	inspection := []agenttools.InspectionProof{
		agenttools.NewInspectionProof("directory_listing", "list_directory", "/Users/jmachen/code", 10),
	}
	conformance := CompareArtifactClaims("The most recently updated projects include claude/settings.local.json and code/aegis-blockchain/EXPERTGUIDE.md.", nil, nil, nil, inspection, "What are the ten most recently updated projects in my code folder?")
	if conformance.HasUnsupported() {
		t.Fatalf("unexpected unsupported claims = %+v", conformance.UnsupportedClaim)
	}
}

func TestCompareArtifactConformance_FlagsUnexpectedExtraWrite(t *testing.T) {
	expected := []ExpectedArtifactSpec{
		{ArtifactKind: "workspace_file", Path: "tmp/check/alpha.txt", ExactContent: "ALPHA"},
		{ArtifactKind: "workspace_file", Path: "tmp/check/beta.txt", ExactContent: "BETA"},
	}
	proofs := []agenttools.ArtifactProof{
		agenttools.NewArtifactProof("workspace_file", "tmp/check/alpha.txt", "ALPHA", false),
		agenttools.NewArtifactProof("workspace_file", "tmp/check/beta.txt", "BETA", false),
		agenttools.NewArtifactProof("workspace_file", "tmp/check/gamma.txt", "GAMMA", false),
	}

	conformance := CompareArtifactConformance(expected, proofs)
	if !conformance.HasUnsatisfied() {
		t.Fatalf("expected unsatisfied conformance, got %+v", conformance)
	}
	if len(conformance.Unexpected) != 1 || conformance.Unexpected[0] != "tmp/check/gamma.txt" {
		t.Fatalf("unexpected = %+v", conformance.Unexpected)
	}
}
