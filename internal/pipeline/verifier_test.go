package pipeline

import (
	"strings"
	"testing"

	agenttools "roboticus/internal/agent/tools"
	"roboticus/internal/llm"
	"roboticus/internal/session"
)

func TestVerifyResponse_FailsUnsupportedCertaintyOnGaps(t *testing.T) {
	ctx := VerificationContext{
		UserPrompt:    "What caused the deployment failure?",
		HasGaps:       true,
		MemoryGapKind: session.MemoryGapNoEvidence,
	}

	result := VerifyResponse("The deployment failed because the canary rollout was misconfigured.", ctx)
	if result.Passed {
		t.Fatal("expected verification failure when response ignores explicit gaps")
	}
}

func TestVerifyResponse_AllowsUncertainLanguageOnGaps(t *testing.T) {
	ctx := VerificationContext{
		UserPrompt:    "What caused the deployment failure?",
		HasGaps:       true,
		MemoryGapKind: session.MemoryGapNoEvidence,
	}

	result := VerifyResponse("Based on the available evidence, I'm not certain yet. We need more data from the deployment logs.", ctx)
	if !result.Passed {
		t.Fatalf("expected uncertain response to pass verification, got %+v", result.Issues)
	}
}

func TestVerifyResponse_AllowsGeopoliticalUncertaintyLanguageOnGaps(t *testing.T) {
	ctx := VerificationContext{
		UserPrompt:    "What's the geopolitical situation?",
		HasGaps:       true,
		MemoryGapKind: session.MemoryGapNoEvidence,
	}

	result := VerifyResponse("I don't have up-to-date information on the current geopolitical situation. For accurate insights, it's best to refer to reliable current reporting.", ctx)
	if !result.Passed {
		t.Fatalf("expected uncertainty-forward geopolitical response to pass, got %+v", result.Issues)
	}
}

func TestVerifyResponse_FailsWhenContradictionsIgnored(t *testing.T) {
	ctx := VerificationContext{
		UserPrompt:        "Which refund rule applies?",
		HasContradictions: true,
	}

	result := VerifyResponse("The refund window is definitely 90 days.", ctx)
	if result.Passed {
		t.Fatal("expected contradiction-aware verification failure")
	}
}

func TestVerifyResponse_FailsWhenMultipartPromptUndercovered(t *testing.T) {
	ctx := VerificationContext{
		UserPrompt: "Explain the root cause and propose a remediation plan",
		Subgoals: []string{
			"Explain the root cause",
			"propose a remediation plan",
		},
	}

	result := VerifyResponse("The root cause was a stale cache entry in the billing service.", ctx)
	if result.Passed {
		t.Fatal("expected multipart verification failure")
	}
}

func TestVerifyResponse_PassesWhenMultipartPromptCovered(t *testing.T) {
	ctx := VerificationContext{
		UserPrompt: "Explain the root cause and propose a remediation plan",
		Subgoals: []string{
			"Explain the root cause",
			"propose a remediation plan",
		},
	}

	result := VerifyResponse("The root cause was a stale cache entry in billing. For remediation, invalidate the cache on deploy and add a consistency check.", ctx)
	if !result.Passed {
		t.Fatalf("expected multipart response to pass verification, got %+v", result.Issues)
	}
}

func TestBuildVerificationContext_PrefersPipelineTaskHints(t *testing.T) {
	sess := session.New("s1", "a1", "Bot")
	sess.AddUserMessage("Please handle this.")
	sess.SetTaskVerificationHints("analysis", "complex", "execute_directly", []string{
		"diagnose the root cause",
		"propose a remediation plan",
	})

	ctx := BuildVerificationContext(sess)
	if ctx.Intent != "analysis" {
		t.Fatalf("expected session intent hint, got %q", ctx.Intent)
	}
	if len(ctx.Subgoals) != 2 {
		t.Fatalf("expected session subgoals, got %+v", ctx.Subgoals)
	}
	if !ctx.RequiresActionPlan {
		t.Fatal("expected remediation-oriented subgoal to require action plan coverage")
	}
}

func TestVerifyResponse_FailsWhenActionPlanMissing(t *testing.T) {
	ctx := VerificationContext{
		UserPrompt:         "Explain the root cause and propose a remediation plan",
		RequiresActionPlan: true,
		Subgoals:           []string{"Explain the root cause", "propose a remediation plan"},
	}

	result := VerifyResponse("The root cause was a stale cache entry in billing.", ctx)
	if result.Passed {
		t.Fatal("expected verification failure when action plan is missing")
	}
}

func TestVerificationSubgoals_DoesNotDoubleCountWholePromptAndConjunctionParts(t *testing.T) {
	goals := verificationSubgoals("Explain the root cause and propose a remediation plan")
	if len(goals) != 2 {
		t.Fatalf("expected 2 canonical subgoals, got %+v", goals)
	}
	if goals[0] != "Explain the root cause" || goals[1] != "propose a remediation plan" {
		t.Fatalf("unexpected canonical subgoals: %+v", goals)
	}
}

func TestVerificationSubgoals_StripsOutputShapeDirective(t *testing.T) {
	goals := verificationSubgoals("Count markdown files recursively in the target docs dir and return only the number.")
	if len(goals) != 1 {
		t.Fatalf("expected 1 semantic subgoal, got %+v", goals)
	}
	if goals[0] != "Count markdown files recursively in the target docs dir" {
		t.Fatalf("unexpected semantic subgoal: %+v", goals)
	}
}

func TestNormalizeSemanticSubgoals_DropsFormattingOnlyGoals(t *testing.T) {
	goals := normalizeSemanticSubgoals([]string{
		"Reply with only the number 1.",
		"Count markdown files recursively in the target docs dir and return only the number.",
	})
	if len(goals) != 1 {
		t.Fatalf("expected 1 semantic subgoal, got %+v", goals)
	}
	if goals[0] != "Count markdown files recursively in the target docs dir" {
		t.Fatalf("unexpected semantic subgoal: %+v", goals)
	}
}

func TestBuildVerificationContext_UsesSessionHistoryAsContinuityEvidence(t *testing.T) {
	sess := session.New("s-history", "a1", "Bot")
	sess.AddUserMessage("Remember this exact project codename for later: basalt-orchid. Also remember these output rules for later replies: answer in lowercase, use semicolons, and mention the codename. Reply only with noted.")
	sess.AddAssistantMessage("noted.", nil)
	sess.AddUserMessage("What codename and output rules did I tell you to remember? Reply on one line.")

	ctx := BuildVerificationContext(sess)
	if !ctx.HasCanonicalEvidence {
		t.Fatal("expected continuity question to promote session history to canonical evidence")
	}
	if len(ctx.EvidenceItems) == 0 {
		t.Fatal("expected continuity evidence items")
	}
}

func TestBuildVerificationContext_StripsFormattingOnlySessionSubgoals(t *testing.T) {
	sess := session.New("s-format", "a1", "Bot")
	sess.AddUserMessage("Reply with only the number 1.")
	sess.SetTaskVerificationHints("task", "simple", "execute_directly", []string{"Reply with only the number 1."})

	ctx := BuildVerificationContext(sess)
	if len(ctx.Subgoals) != 0 {
		t.Fatalf("subgoals = %+v, want none after formatting normalization", ctx.Subgoals)
	}
}

func TestBuildVerificationContext_ContinuityEvidenceOverridesGenericGaps(t *testing.T) {
	sess := session.New("s-history-gap", "a1", "Bot")
	sess.AddUserMessage("Remember this exact project codename for later: basalt-orchid. Reply only with noted.")
	sess.AddAssistantMessage("noted.", nil)
	sess.AddUserMessage("What codename did I tell you to remember? Reply on one line.")
	sess.SetMemoryContext("[Active Memory]\n\n[Retrieved Evidence]\n1. [semantic, 0.8] unrelated fact\n[Gaps]\n- No relevant procedures\n")

	ctx := BuildVerificationContext(sess)
	if !ctx.HasCanonicalEvidence {
		t.Fatal("expected canonical continuity evidence")
	}
	if ctx.HasGaps {
		t.Fatal("continuity evidence should override generic retrieval gaps")
	}
}

func TestVerifyResponse_PassesWhenActionPlanGoalUsesEquivalentRemediationLanguage(t *testing.T) {
	ctx := VerificationContext{
		UserPrompt:         "Explain the root cause and propose a remediation plan",
		RequiresActionPlan: true,
		Subgoals:           []string{"Explain the root cause", "propose a remediation plan"},
	}

	result := VerifyResponse("The root cause was a stale cache entry in billing. Recommended fix: invalidate the cache on deploy and add a consistency check before invoice generation.", ctx)
	if !result.Passed {
		t.Fatalf("expected equivalent remediation language to satisfy plan coverage, got %+v", result.Issues)
	}
}

func TestVerifyResponse_PassesWhenAffectedSystemsAnswerIsCautiousAndPartiallySupported(t *testing.T) {
	ctx := VerificationContext{
		UserPrompt: "Create a report that explains the root cause and identifies which systems were affected.",
		Subgoals: []string{
			"Create a report that explains the root cause",
			"identifies which systems were affected.",
		},
		EvidenceItems: []string{
			"Billing service cache invalidation failed after deploy",
		},
		HasGaps:       true,
		MemoryGapKind: session.MemoryGapNoEvidence,
	}

	result := VerifyResponse("The root cause was a stale billing cache. The available evidence confirms impact to billing, but ledger still needs verification.", ctx)
	if !result.Passed {
		t.Fatalf("expected cautious partial-entity answer to pass, got %+v", result.Issues)
	}
}

func TestVerifyResponse_PassesWhenOnlyMissingMemoryTiersRemain(t *testing.T) {
	ctx := VerificationContext{
		UserPrompt:    "In Go, what does len(slice) return when the slice is nil?",
		Intent:        "code",
		PlannedAction: "execute_directly",
		HasEvidence:   true,
		HasGaps:       true,
		MemoryGapKind: session.MemoryGapMissingTiers,
	}

	result := VerifyResponse("len on a nil slice returns 0 in Go.", ctx)
	if !result.Passed {
		t.Fatalf("expected missing-tier gaps to stay neutral, got %+v", result.Issues)
	}
}

func TestVerifyResponse_PassesDerivableQuestionWithoutMemoryEvidence(t *testing.T) {
	ctx := VerificationContext{
		UserPrompt:    "What is 2 + 2?",
		Intent:        "question",
		PlannedAction: "execute_directly",
		HasGaps:       true,
		MemoryGapKind: session.MemoryGapNoEvidence,
	}

	result := VerifyResponse("4", ctx)
	if !result.Passed {
		t.Fatalf("expected derivable no-evidence question to remain neutral, got %+v", result.Issues)
	}
}

func TestVerifyResponse_PassesDerivableQuestionWithIrrelevantEvidencePresent(t *testing.T) {
	ctx := VerificationContext{
		UserPrompt:    "What is 2 + 2?",
		Intent:        "question",
		PlannedAction: "execute_directly",
		Subgoals:      []string{"what is 2 + 2"},
		EvidenceItems: []string{
			"Working memory count: 1",
			"Episodic memory count: 4178",
		},
	}

	result := VerifyResponse("4", ctx)
	if !result.Passed {
		t.Fatalf("expected derivable direct-fact answer to stay evidence-neutral, got %+v", result.Issues)
	}
}

func TestVerifyResponse_PassesDerivableLanguageFactWithIrrelevantEvidencePresent(t *testing.T) {
	ctx := VerificationContext{
		UserPrompt:    "In Go, what does `len(slice)` return when the slice is nil?",
		PlannedAction: "execute_directly",
		Subgoals:      []string{"in go, what does len(slice) return when the slice is nil"},
		EvidenceItems: []string{
			"Working memory count: 1",
			"Semantic memory count: 2266",
		},
	}

	result := VerifyResponse("`len` returns 0 for a nil slice.", ctx)
	if !result.Passed {
		t.Fatalf("expected derivable language/runtime fact to stay evidence-neutral, got %+v", result.Issues)
	}
}

func TestVerifyResponse_PassesWhenScheduledOutputSatisfiesScheduleGoal(t *testing.T) {
	ctx := VerificationContext{
		UserPrompt: "schedule a cron job that runs every 5 minutes and tell me exactly what was scheduled",
		Subgoals: []string{
			"schedule a cron job that runs every 5 minutes",
			"tell me exactly what was scheduled",
		},
	}

	result := VerifyResponse("Created cron job 'periodic-check' with schedule */5 * * * * — runs every 5 minutes. ID: cron-abc123.", ctx)
	if !result.Passed {
		t.Fatalf("expected scheduled output to satisfy cron coverage, got %+v", result.Issues)
	}
}

func TestVerifyResponse_FailsPolicySensitiveAbsoluteAnswerWithoutCanonicalAnchor(t *testing.T) {
	ctx := VerificationContext{
		UserPrompt:           "What is the refund policy?",
		PolicySensitive:      true,
		HasCanonicalEvidence: true,
	}

	result := VerifyResponse("Customers definitely always get a full refund within 90 days.", ctx)
	if result.Passed {
		t.Fatal("expected canonical-source verification failure")
	}
}

func TestVerifyResponse_PassesPolicySensitiveAnswerWithCanonicalAnchor(t *testing.T) {
	ctx := VerificationContext{
		UserPrompt:           "What is the refund policy?",
		PolicySensitive:      true,
		HasCanonicalEvidence: true,
	}

	result := VerifyResponse("According to the current policy, customers can request a refund within 30 days for unused purchases.", ctx)
	if !result.Passed {
		t.Fatalf("expected canonically anchored policy answer to pass, got %+v", result.Issues)
	}
}

func TestVerifyResponse_FailsFreshnessOverclaim(t *testing.T) {
	ctx := VerificationContext{
		UserPrompt:        "What is the latest refund policy?",
		HasFreshnessRisk:  true,
		RequiresFreshness: true,
	}

	result := VerifyResponse("The refund policy is definitely 90 days.", ctx)
	if result.Passed {
		t.Fatal("expected freshness-aware verification failure")
	}
}

func TestVerifyResponse_PassesWhenFreshnessRiskAcknowledged(t *testing.T) {
	ctx := VerificationContext{
		UserPrompt:        "What is the latest refund policy?",
		HasFreshnessRisk:  true,
		RequiresFreshness: true,
	}

	result := VerifyResponse("Based on the available evidence, this may be outdated. Please verify against the current policy before acting.", ctx)
	if !result.Passed {
		t.Fatalf("expected freshness-aware caution to pass, got %+v", result.Issues)
	}
}

func TestBuildVerificationContext_PromotesArtifactProofIntoEvidence(t *testing.T) {
	sess := session.New("s-artifact", "a1", "Bot")
	sess.AddUserMessage("Create tmp/out.txt containing exactly hello")
	proof := agenttools.NewArtifactProof("workspace_file", "tmp/out.txt", "hello", false)
	sess.AddToolResultWithMetadata("call-1", "write_file", proof.Output(), proof.Metadata(), false)

	ctx := BuildVerificationContext(sess)
	if len(ctx.ArtifactProofs) != 1 {
		t.Fatalf("artifact proofs = %d, want 1", len(ctx.ArtifactProofs))
	}
	if !ctx.HasCanonicalEvidence {
		t.Fatal("artifact proof should count as canonical evidence")
	}
	if len(ctx.EvidenceItems) == 0 {
		t.Fatal("expected artifact proof evidence item")
	}
}

func TestBuildVerificationContext_PromotesSourceArtifactReadProofIntoEvidence(t *testing.T) {
	sess := session.New("s-read", "a1", "Bot")
	sess.AddUserMessage("Read tmp/in.txt and then create tmp/out.txt containing exactly hello")
	proof := agenttools.NewArtifactReadProof("workspace_file", "tmp/in.txt", "source-data")
	sess.AddToolResultWithMetadata("call-1", "read_file", "source-data", proof.Metadata(), false)

	ctx := BuildVerificationContext(sess)
	if len(ctx.SourceArtifactProofs) != 1 {
		t.Fatalf("source artifact proofs = %d, want 1", len(ctx.SourceArtifactProofs))
	}
	if !ctx.HasCanonicalEvidence {
		t.Fatal("source read proof should count as canonical evidence")
	}
	if len(ctx.EvidenceItems) == 0 {
		t.Fatal("expected source read proof evidence item")
	}
}

func TestBuildVerifierRetryPlan_SourceArtifactUnreadPrefersSourceReadTools(t *testing.T) {
	result := VerificationResult{
		Passed: false,
		Issues: []VerificationIssue{{
			Code:   "source_artifact_unread",
			Detail: "the response depends on source artifacts that were referenced in the prompt but not read through authoritative tool-backed evidence: tmp/in.txt",
		}},
	}
	ctx := VerificationContext{
		SourceArtifacts: []string{"tmp/in.txt"},
	}
	selected := []llm.ToolDef{
		{Type: "function", Function: llm.ToolFuncDef{Name: "write_file"}},
		{Type: "function", Function: llm.ToolFuncDef{Name: "recall_memory"}},
		{Type: "function", Function: llm.ToolFuncDef{Name: "read_file"}},
		{Type: "function", Function: llm.ToolFuncDef{Name: "get_runtime_context"}},
	}

	plan := buildVerifierRetryPlan(result, ctx, selected)
	if !strings.Contains(plan.Message, "reading the prompt-named source artifact") {
		t.Fatalf("retry message = %q, want source-read corrective guidance", plan.Message)
	}
	if plan.CorrectionReason == "" {
		t.Fatal("expected correction reason")
	}
	if len(plan.ToolDefs) != 3 {
		t.Fatalf("tool defs = %d, want 3", len(plan.ToolDefs))
	}
	for _, def := range plan.ToolDefs {
		if def.Function.Name == "recall_memory" {
			t.Fatal("recall_memory should be removed from source-read corrective retry surface")
		}
	}
}

func TestVerifyResponse_AllowsDirectArtifactClaimWhenExactProofOverridesStaleGaps(t *testing.T) {
	ctx := VerificationContext{
		UserPrompt:        "Create tmp/out.txt containing exactly hello",
		HasGaps:           true,
		HasContradictions: true,
		ToolResults: []ToolResultEntry{{
			ToolName: "write_file",
			Output:   `{"proof_type":"artifact_write"}`,
			ArtifactProof: func() *agenttools.ArtifactProof {
				p := agenttools.NewArtifactProof("workspace_file", "tmp/out.txt", "hello", false)
				return &p
			}(),
		}},
		ArtifactProofs: []agenttools.ArtifactProof{
			agenttools.NewArtifactProof("workspace_file", "tmp/out.txt", "hello", false),
		},
		ExpectedArtifacts: []ExpectedArtifactSpec{{
			ArtifactKind: "workspace_file",
			Path:         "tmp/out.txt",
			ExactContent: "hello",
		}},
		ArtifactConformance: ArtifactConformance{
			Expected:    []ExpectedArtifactSpec{{ArtifactKind: "workspace_file", Path: "tmp/out.txt", ExactContent: "hello"}},
			MatchedPath: []string{"tmp/out.txt"},
		},
	}

	result := VerifyResponse("I created tmp/out.txt containing exactly hello.", ctx)
	if !result.Passed {
		t.Fatalf("expected exact artifact proof to override stale gaps/contradictions, got %+v", result.Issues)
	}
}

func TestVerifyResponse_AllowsDirectArtifactClaimWhenWithContentPromptExactProofOverridesStaleGaps(t *testing.T) {
	prompt := "Create two files in tmp/procedural-canary-8/ exactly as follows. File 1: rollout-config.json with content:\n{\n  \"service\": \"billing\",\n  \"strategy\": \"canary\",\n  \"steps\": 3\n}\nFile 2: rollout-runbook.md with content:\n# Billing Canary Runbook\n1. Deploy canary to 10% of traffic.\n2. Check error rate and latency for 15 minutes.\n3. If metrics are healthy, advance to 50%.\n4. If metrics regress, roll back immediately."
	configProof := agenttools.NewArtifactProof("workspace_file", "tmp/procedural-canary-8/rollout-config.json", "{\n  \"service\": \"billing\",\n  \"strategy\": \"canary\",\n  \"steps\": 3\n}", false)
	runbookProof := agenttools.NewArtifactProof("workspace_file", "tmp/procedural-canary-8/rollout-runbook.md", "# Billing Canary Runbook\n1. Deploy canary to 10% of traffic.\n2. Check error rate and latency for 15 minutes.\n3. If metrics are healthy, advance to 50%.\n4. If metrics regress, roll back immediately.", false)
	ctx := VerificationContext{
		UserPrompt: prompt,
		HasGaps:    true,
		EvidenceItems: []string{
			"[procedural, 0.70] stale prior note-writing pattern",
		},
		ToolResults: []ToolResultEntry{
			{ToolName: "write_file", Output: configProof.Output(), ArtifactProof: &configProof},
			{ToolName: "write_file", Output: runbookProof.Output(), ArtifactProof: &runbookProof},
		},
		ArtifactProofs: []agenttools.ArtifactProof{configProof, runbookProof},
	}
	contract := ParseArtifactPromptContract(prompt)
	ctx.ExpectedArtifacts = contract.ExpectedOutputs
	ctx.SourceArtifacts = contract.SourceInputs
	ctx.ArtifactConformance = CompareArtifactConformance(ctx.ExpectedArtifacts, ctx.ArtifactProofs)
	if len(ctx.ExpectedArtifacts) != 2 || !ctx.ArtifactConformance.AllExactSatisfied() {
		t.Fatalf("expected exact artifact specs and conformance, got specs=%+v conformance=%+v", ctx.ExpectedArtifacts, ctx.ArtifactConformance)
	}

	result := VerifyResponse("I created rollout-config.json and rollout-runbook.md with the requested content.", ctx)
	if !result.Passed {
		t.Fatalf("expected exact artifact proof to override stale gaps for with-content prompt, got %+v", result.Issues)
	}
}

func TestVerifyResponse_FailsWhenExactArtifactContentMismatches(t *testing.T) {
	ctx := VerificationContext{
		UserPrompt: "Create tmp/out.txt containing exactly hello",
		ExpectedArtifacts: []ExpectedArtifactSpec{{
			ArtifactKind: "workspace_file",
			Path:         "tmp/out.txt",
			ExactContent: "hello",
		}},
		ArtifactProofs: []agenttools.ArtifactProof{
			agenttools.NewArtifactProof("workspace_file", "tmp/out.txt", "goodbye", false),
		},
		ArtifactConformance: ArtifactConformance{
			Expected: []ExpectedArtifactSpec{{ArtifactKind: "workspace_file", Path: "tmp/out.txt", ExactContent: "hello"}},
			Mismatched: []ArtifactContentMismatch{{
				Path:     "tmp/out.txt",
				Expected: "hello",
				Actual:   "goodbye",
				Reason:   "content_mismatch",
			}},
		},
	}

	result := VerifyResponse("I wrote tmp/out.txt.", ctx)
	if result.Passed {
		t.Fatal("expected verification failure on exact-content mismatch")
	}
	if !result.HasIssue("artifact_content_mismatch") {
		t.Fatalf("issues = %+v, want artifact_content_mismatch", result.Issues)
	}
}

func TestVerifyResponse_FailsWhenResponseInventsExtraArtifact(t *testing.T) {
	prompt := "Create two files in tmp/check/ exactly as follows. File 1: alpha.txt with content:\nALPHA\nFile 2: beta.txt with content:\nBETA"
	alpha := agenttools.NewArtifactProof("workspace_file", "tmp/check/alpha.txt", "ALPHA", false)
	beta := agenttools.NewArtifactProof("workspace_file", "tmp/check/beta.txt", "BETA", false)
	ctx := VerificationContext{
		UserPrompt:     prompt,
		ToolResults:    []ToolResultEntry{{ToolName: "write_file", Output: alpha.Output(), ArtifactProof: &alpha}, {ToolName: "write_file", Output: beta.Output(), ArtifactProof: &beta}},
		ArtifactProofs: []agenttools.ArtifactProof{alpha, beta},
	}
	contract := ParseArtifactPromptContract(prompt)
	ctx.ExpectedArtifacts = contract.ExpectedOutputs
	ctx.SourceArtifacts = contract.SourceInputs
	ctx.ArtifactConformance = CompareArtifactConformance(ctx.ExpectedArtifacts, ctx.ArtifactProofs)
	if !ctx.ArtifactConformance.AllExactSatisfied() {
		t.Fatalf("expected exact artifact conformance, got %+v", ctx.ArtifactConformance)
	}

	result := VerifyResponse("I created alpha.txt, beta.txt, and gamma.txt.", ctx)
	if result.Passed {
		t.Fatal("expected verification failure on invented extra artifact")
	}
	if !result.HasIssue("artifact_set_overclaim") {
		t.Fatalf("issues = %+v, want artifact_set_overclaim", result.Issues)
	}
}

func TestVerifyResponse_FailsWhenUnexpectedArtifactWasWritten(t *testing.T) {
	prompt := "Create two files in tmp/check/ exactly as follows. File 1: alpha.txt with content:\nALPHA\nFile 2: beta.txt with content:\nBETA"
	alpha := agenttools.NewArtifactProof("workspace_file", "tmp/check/alpha.txt", "ALPHA", false)
	beta := agenttools.NewArtifactProof("workspace_file", "tmp/check/beta.txt", "BETA", false)
	gamma := agenttools.NewArtifactProof("workspace_file", "tmp/check/gamma.txt", "GAMMA", false)
	ctx := VerificationContext{
		UserPrompt:     prompt,
		ToolResults:    []ToolResultEntry{{ToolName: "write_file", Output: alpha.Output(), ArtifactProof: &alpha}, {ToolName: "write_file", Output: beta.Output(), ArtifactProof: &beta}, {ToolName: "write_file", Output: gamma.Output(), ArtifactProof: &gamma}},
		ArtifactProofs: []agenttools.ArtifactProof{alpha, beta, gamma},
	}
	contract := ParseArtifactPromptContract(prompt)
	ctx.ExpectedArtifacts = contract.ExpectedOutputs
	ctx.SourceArtifacts = contract.SourceInputs
	ctx.ArtifactConformance = CompareArtifactConformance(ctx.ExpectedArtifacts, ctx.ArtifactProofs)
	if len(ctx.ArtifactConformance.Unexpected) != 1 {
		t.Fatalf("expected one unexpected proof, got %+v", ctx.ArtifactConformance)
	}

	result := VerifyResponse("I created alpha.txt, beta.txt, and gamma.txt.", ctx)
	if result.Passed {
		t.Fatal("expected verification failure on unexpected artifact write")
	}
	if !result.HasIssue("artifact_unexpected_write") {
		t.Fatalf("issues = %+v, want artifact_unexpected_write", result.Issues)
	}
}

func TestVerifyResponse_AllowsInspectionListingWithoutArtifactContract(t *testing.T) {
	ctx := VerificationContext{
		UserPrompt:    "What's in your vault right now?",
		Intent:        "task",
		PlannedAction: "execute_directly",
		ToolResults: []ToolResultEntry{
			{
				ToolName: "list_directory",
				Output:   "alpha.txt\nbeta.txt\ngamma.txt",
			},
		},
	}
	result := VerifyResponse("The vault currently contains alpha.txt, beta.txt, and gamma.txt.", ctx)
	if result.HasIssue("artifact_set_overclaim") {
		t.Fatalf("issues = %+v, did not expect artifact_set_overclaim", result.Issues)
	}
}

func TestVerifyResponse_AllowsCodeFolderInspectionListingWithInspectionEvidence(t *testing.T) {
	ctx := VerificationContext{
		UserPrompt:       "What are the ten most recently updated projects in my code folder?",
		Intent:           "task",
		PlannedAction:    "execute_directly",
		InspectionProofs: []agenttools.InspectionProof{agenttools.NewInspectionProof("directory_listing", "list_directory", "/Users/jmachen/code", 10)},
		ToolResults: []ToolResultEntry{{ToolName: "list_directory", Output: "claude/\nroboticus/\nroboticus-site/\n", Inspection: func() *agenttools.InspectionProof {
			p := agenttools.NewInspectionProof("directory_listing", "list_directory", "/Users/jmachen/code", 3)
			return &p
		}()}},
	}
	result := VerifyResponse("The ten most recently updated projects include `claude/`, `roboticus/`, and `roboticus-site/`.", ctx)
	if result.HasIssue("artifact_set_overclaim") {
		t.Fatalf("issues = %+v, did not expect artifact_set_overclaim", result.Issues)
	}
}

func TestVerifyResponse_AllowsTruthfulSourceArtifactReference(t *testing.T) {
	prompt := "Read tmp/procedural-workflow-1/requirements.txt, then create exactly two files in tmp/procedural-workflow-1/: deploy-config.json with content:\n{}\nFile 2: rollout-runbook.md with content:\n# Runbook"
	config := agenttools.NewArtifactProof("workspace_file", "tmp/procedural-workflow-1/deploy-config.json", "{}", false)
	runbook := agenttools.NewArtifactProof("workspace_file", "tmp/procedural-workflow-1/rollout-runbook.md", "# Runbook", false)
	contract := ParseArtifactPromptContract(prompt)
	ctx := VerificationContext{
		UserPrompt:        prompt,
		ToolResults:       []ToolResultEntry{{ToolName: "write_file", Output: config.Output(), ArtifactProof: &config}, {ToolName: "write_file", Output: runbook.Output(), ArtifactProof: &runbook}},
		ArtifactProofs:    []agenttools.ArtifactProof{config, runbook},
		ExpectedArtifacts: contract.ExpectedOutputs,
		SourceArtifacts:   contract.SourceInputs,
	}
	ctx.ArtifactConformance = CompareArtifactConformance(ctx.ExpectedArtifacts, ctx.ArtifactProofs)
	result := VerifyResponse("I read requirements.txt and created deploy-config.json and rollout-runbook.md.", ctx)
	if result.HasIssue("artifact_set_overclaim") {
		t.Fatalf("issues = %+v, did not expect artifact_set_overclaim", result.Issues)
	}
}

func TestVerifyResponse_FailsWhenSourceArtifactWasNotRead(t *testing.T) {
	prompt := "Read tmp/procedural-workflow-1/requirements.txt, then create exactly two files in tmp/procedural-workflow-1/: deploy-config.json with content:\n{}\nFile 2: rollout-runbook.md with content:\n# Runbook"
	config := agenttools.NewArtifactProof("workspace_file", "tmp/procedural-workflow-1/deploy-config.json", "{}", false)
	runbook := agenttools.NewArtifactProof("workspace_file", "tmp/procedural-workflow-1/rollout-runbook.md", "# Runbook", false)
	contract := ParseArtifactPromptContract(prompt)
	ctx := VerificationContext{
		UserPrompt:        prompt,
		ToolResults:       []ToolResultEntry{{ToolName: "write_file", Output: config.Output(), ArtifactProof: &config}, {ToolName: "write_file", Output: runbook.Output(), ArtifactProof: &runbook}},
		ArtifactProofs:    []agenttools.ArtifactProof{config, runbook},
		ExpectedArtifacts: contract.ExpectedOutputs,
		SourceArtifacts:   contract.SourceInputs,
	}
	ctx.ArtifactConformance = CompareArtifactConformance(ctx.ExpectedArtifacts, ctx.ArtifactProofs)
	ctx.SourceConformance = CompareSourceArtifactConformance(ctx.SourceArtifacts, nil)

	result := VerifyResponse("I created deploy-config.json and rollout-runbook.md based on requirements.txt.", ctx)
	if result.Passed {
		t.Fatal("expected verification failure when source artifact was not read")
	}
	if !result.HasIssue("source_artifact_unread") {
		t.Fatalf("issues = %+v, want source_artifact_unread", result.Issues)
	}
}

func TestBuildVerificationContext_ExtractsEvidenceItems(t *testing.T) {
	sess := session.New("s1", "a1", "Bot")
	sess.AddUserMessage("Find the root cause and identify the affected systems.")
	sess.SetMemoryContext("[Active Memory]\n\n[Retrieved Evidence]\n1. [semantic, 0.90] Billing service cache invalidation failed\n2. [relationship, 0.80] Billing Service depends_on Ledger Service\n\n[Gaps]\n- No relevant procedures or workflows found")

	ctx := BuildVerificationContext(sess)
	if len(ctx.EvidenceItems) != 2 {
		t.Fatalf("expected 2 evidence items, got %+v", ctx.EvidenceItems)
	}
	if ctx.EvidenceItems[0] == "" || ctx.EvidenceItems[1] == "" {
		t.Fatalf("expected non-empty evidence items, got %+v", ctx.EvidenceItems)
	}
}

func TestVerifyResponse_FailsWhenAnsweredSubgoalLacksEvidenceSupport(t *testing.T) {
	ctx := VerificationContext{
		UserPrompt: "Find the root cause and identify affected systems",
		Subgoals: []string{
			"find the root cause",
			"identify affected systems",
		},
		EvidenceItems: []string{
			"Billing service cache invalidation failed after deploy",
		},
	}

	result := VerifyResponse("The root cause was a stale billing cache, and the affected systems were billing and ledger.", ctx)
	if result.Passed {
		t.Fatal("expected unsupported evidence failure")
	}
	if len(result.Issues) == 0 || result.Issues[len(result.Issues)-1].Code != "unsupported_subgoal" {
		t.Fatalf("expected unsupported_subgoal issue, got %+v", result.Issues)
	}
}

func TestVerifyResponse_FailsWhenReversedAffectedSystemsGoalLacksEvidenceSupport(t *testing.T) {
	ctx := VerificationContext{
		UserPrompt: "What was the root cause, and which systems were affected?",
		Subgoals: []string{
			"what was the root cause",
			"which systems were affected",
		},
		EvidenceItems: []string{
			"Billing service cache invalidation failed after deploy",
		},
	}

	result := VerifyResponse("The root cause was a stale billing cache, and the affected systems were billing and ledger.", ctx)
	if result.Passed {
		t.Fatal("expected unsupported evidence failure for reversed affected-systems phrasing")
	}
	if len(result.Issues) == 0 || result.Issues[len(result.Issues)-1].Code != "unsupported_subgoal" {
		t.Fatalf("expected unsupported_subgoal issue, got %+v", result.Issues)
	}
}

func TestBuildVerificationContext_ExtractsExecutiveSections(t *testing.T) {
	sess := session.New("s1", "a1", "Bot")
	sess.AddUserMessage("Is the rollout blocked by legal review?")
	sess.SetMemoryContext("[Active Memory]\n\n[Working State]\nExecutive State:\nTask: t-1\n" +
		"Plan:\n- Investigate auth outage\n" +
		"Unresolved questions:\n- is rollout blocked by legal?\n" +
		"Stopping criteria:\n- ship PR with tests (all tests green)\n" +
		"\n[Retrieved Evidence]\n1. [semantic, 0.9] deploy doc\n")

	ctx := BuildVerificationContext(sess)
	if len(ctx.UnresolvedQuestions) != 1 {
		t.Fatalf("expected 1 unresolved question, got %+v", ctx.UnresolvedQuestions)
	}
	if len(ctx.StoppingCriteria) != 1 {
		t.Fatalf("expected 1 stopping criterion, got %+v", ctx.StoppingCriteria)
	}
	if ctx.StoppingCriteria[0] != "ship PR with tests" {
		t.Fatalf("expected stopping criterion to strip payload parenthetical, got %q", ctx.StoppingCriteria[0])
	}
}

func TestVerifyResponse_FailsWhenUnresolvedQuestionAbandoned(t *testing.T) {
	ctx := VerificationContext{
		UserPrompt:          "Is the rollout blocked by legal review?",
		UnresolvedQuestions: []string{"is rollout blocked by legal"},
	}
	// Response ignores the legal-rollout question entirely.
	result := VerifyResponse("The deploy pipeline is green and the build artifact is ready.", ctx)
	if result.Passed {
		t.Fatalf("expected abandoned-question failure, got pass")
	}
	if !hasIssue(result, "abandoned_unresolved_question") {
		t.Fatalf("expected abandoned_unresolved_question issue, got %+v", result.Issues)
	}
}

func TestVerifyResponse_FailsWhenStoppingCriteriaUnmet(t *testing.T) {
	ctx := VerificationContext{
		UserPrompt:       "Are we ready to ship?",
		StoppingCriteria: []string{"ship a PR with tests"},
	}
	result := VerifyResponse("Task complete. We are done.", ctx)
	if result.Passed {
		t.Fatalf("expected stopping-criteria failure, got pass")
	}
	if !hasIssue(result, "stopping_criteria_unmet") {
		t.Fatalf("expected stopping_criteria_unmet issue, got %+v", result.Issues)
	}
}

func TestVerificationRetryMessage_IncludesProofAndContradictionGuidance(t *testing.T) {
	result := VerificationResult{
		Passed: false,
		Issues: []VerificationIssue{
			{Code: "unresolved_contradicted_claim", Detail: "conflicting evidence was not reconciled"},
			{Code: "proof_obligation_unmet", Detail: "high-risk claims were not anchored"},
		},
		ClaimAudits: []ClaimAudit{
			{
				Sentence:     "The refund window is always 30 days.",
				Certainty:    CertaintyAbsolute.String(),
				Contested:    true,
				MissingProof: []string{"canonical_anchor", "contradiction_resolution"},
				IssueCode:    "proof_obligation_unmet",
			},
		},
	}

	msg := result.RetryMessage()
	if !strings.Contains(msg, "reconcile contested evidence") {
		t.Fatalf("expected contradiction guidance in retry message, got %q", msg)
	}
	if !strings.Contains(msg, "anchor each high-risk claim") {
		t.Fatalf("expected proof guidance in retry message, got %q", msg)
	}
}

func TestVerifyResponse_PassesWhenStoppingCriteriaAddressed(t *testing.T) {
	ctx := VerificationContext{
		UserPrompt:       "Are we ready to ship?",
		StoppingCriteria: []string{"ship a PR with tests"},
	}
	result := VerifyResponse("Task complete. The PR is ready and the tests all pass.", ctx)
	if !result.Passed {
		t.Fatalf("expected criteria-addressed response to pass, got %+v", result.Issues)
	}
}

func TestVerifyResponse_PassesWhenAnsweredSubgoalsAreEvidenceSupported(t *testing.T) {
	ctx := VerificationContext{
		UserPrompt: "Find the root cause and identify affected systems",
		Subgoals: []string{
			"find the root cause",
			"identify affected systems",
		},
		EvidenceItems: []string{
			"Billing service cache invalidation failed after deploy",
			"Billing Service depends_on Ledger Service",
		},
	}

	result := VerifyResponse("The root cause was a stale billing cache, and the affected systems were billing and ledger.", ctx)
	if !result.Passed {
		t.Fatalf("expected evidence-supported response to pass, got %+v", result.Issues)
	}
}

func TestVerifyResponse_PassesConciseArtifactBackedReportCompletion(t *testing.T) {
	ctx := VerificationContext{
		UserPrompt:    "Generate a report on all development projects in my code directory including project path, project name, project languages, project first edit date, project last edit date, and whether the project is out of date with the remote origin repo and in which direction. Order it by last edit date descending and write the report as a new document to my obsidian vault on my desktop.",
		PlannedAction: "execute_directly",
		Subgoals: []string{
			"project path",
			"project name",
			"project language(s)",
			"project first edit date",
			"project last edit date",
			"whether the project is out of date with the remote origin repo and in which direction",
		},
		ArtifactProofs: []agenttools.ArtifactProof{{
			ProofType:            "artifact_write",
			ArtifactKind:         "file",
			Path:                 "/Users/jmachen/Desktop/My Vault/development_projects_report.md",
			Bytes:                2048,
			ContentSHA256:        "abc123",
			ExactContentIncluded: false,
		}},
		EvidenceItems: []string{
			"project inventory gathered from /Users/jmachen/code",
			"written report saved to /Users/jmachen/Desktop/My Vault/development_projects_report.md",
		},
	}

	result := VerifyResponse("The report has been generated and saved to your Desktop vault as development_projects_report.md.", ctx)
	if !result.Passed {
		t.Fatalf("expected concise artifact-backed completion to pass, got %+v", result.Issues)
	}
	if result.HasIssue("subgoal_coverage") {
		t.Fatalf("did not expect subgoal_coverage, got %+v", result.Issues)
	}
}

func TestVerifyResponse_MixedOutputTurnStillRequiresChatCoverage(t *testing.T) {
	ctx := VerificationContext{
		UserPrompt:    "Generate a report on all development projects in my code directory, write it to my desktop vault, and also summarize the top three most recently updated projects here in chat.",
		PlannedAction: "execute_directly",
		Subgoals: []string{
			"project path",
			"project name",
			"project language(s)",
			"project first edit date",
			"project last edit date",
			"whether the project is out of date with the remote origin repo and in which direction",
			"summarize the top three most recently updated projects here in chat",
		},
		ArtifactProofs: []agenttools.ArtifactProof{{
			ProofType:            "artifact_write",
			ArtifactKind:         "file",
			Path:                 "/Users/jmachen/Desktop/My Vault/development_projects_report.md",
			Bytes:                2048,
			ContentSHA256:        "abc123",
			ExactContentIncluded: false,
		}},
		EvidenceItems: []string{
			"project inventory gathered from /Users/jmachen/code",
			"written report saved to /Users/jmachen/Desktop/My Vault/development_projects_report.md",
		},
	}

	result := VerifyResponse("The report has been generated and saved to your Desktop vault as development_projects_report.md.", ctx)
	if result.Passed {
		t.Fatalf("expected mixed-output turn to fail coverage, got pass")
	}
	if !result.HasIssue("subgoal_coverage") {
		t.Fatalf("expected subgoal_coverage, got %+v", result.Issues)
	}
}

func TestVerifyResponse_FailsOperationalStatusLeakageOnSocialTurn(t *testing.T) {
	ctx := VerificationContext{
		UserPrompt: "What's the good word?",
		Intent:     "conversational",
	}
	result := VerifyResponse("The good word is that the vault is still locked and the workspace remains sandboxed.", ctx)
	if result.Passed {
		t.Fatal("expected off-topic social-turn verification failure")
	}
	if !hasIssue(result, "off_topic_social_turn") {
		t.Fatalf("expected off_topic_social_turn issue, got %+v", result.Issues)
	}
}

func TestVerifyResponse_AllowsNormalGreetingOnSocialTurn(t *testing.T) {
	ctx := VerificationContext{
		UserPrompt: "What's the good word?",
		Intent:     "conversational",
	}
	result := VerifyResponse("Not much, just saying hello back. What can I help you with?", ctx)
	if !result.Passed {
		t.Fatalf("expected normal social reply to pass, got %+v", result.Issues)
	}
}

func TestVerificationResult_RetryMessageForOffTopicSocialTurnIsSpecific(t *testing.T) {
	result := VerificationResult{
		Passed: false,
		Issues: []VerificationIssue{{
			Code:   "off_topic_social_turn",
			Detail: "the user made a lightweight social or colloquial greeting, but the response pivoted into operational status instead of acknowledging the greeting",
		}},
	}
	msg := result.RetryMessage()
	if !strings.Contains(msg, "brief, natural way") {
		t.Fatalf("retry message missing social-turn guidance: %q", msg)
	}
	if !strings.Contains(msg, "Do not mention sandbox state") {
		t.Fatalf("retry message missing operational-status prohibition: %q", msg)
	}
}
