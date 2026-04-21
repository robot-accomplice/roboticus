package pipeline

import (
	"strings"
	"testing"

	"roboticus/internal/session"
)

func TestVerifyResponse_FailsUnsupportedCertaintyOnGaps(t *testing.T) {
	ctx := VerificationContext{
		UserPrompt: "What caused the deployment failure?",
		HasGaps:    true,
	}

	result := VerifyResponse("The deployment failed because the canary rollout was misconfigured.", ctx)
	if result.Passed {
		t.Fatal("expected verification failure when response ignores explicit gaps")
	}
}

func TestVerifyResponse_AllowsUncertainLanguageOnGaps(t *testing.T) {
	ctx := VerificationContext{
		UserPrompt: "What caused the deployment failure?",
		HasGaps:    true,
	}

	result := VerifyResponse("Based on the available evidence, I'm not certain yet. We need more data from the deployment logs.", ctx)
	if !result.Passed {
		t.Fatalf("expected uncertain response to pass verification, got %+v", result.Issues)
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
