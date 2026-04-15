package pipeline

import (
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
