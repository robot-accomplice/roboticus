package pipeline

import (
	"strings"
	"testing"

	"roboticus/internal/session"
)

func TestExtractClaims_SplitsSentencesAndClassifiesCertainty(t *testing.T) {
	response := "The refund window is 30 days. Customers always get a full refund. Maybe we should verify with legal."

	claims := ExtractClaims(response)
	if len(claims) != 3 {
		t.Fatalf("expected 3 claims, got %d: %+v", len(claims), claims)
	}

	if claims[0].Certainty != CertaintyHigh {
		t.Fatalf("expected first claim certainty=high, got %s", claims[0].Certainty)
	}
	if claims[1].Certainty != CertaintyAbsolute {
		t.Fatalf("expected second claim certainty=absolute, got %s", claims[1].Certainty)
	}
	if claims[2].Certainty != CertaintyHedged {
		t.Fatalf("expected third claim certainty=hedged, got %s", claims[2].Certainty)
	}
}

func TestExtractClaims_SkipsQuestionsAndBullets(t *testing.T) {
	response := "What is the refund policy?\n- The window is always 30 days\n- According to the current policy, refunds are processed within 5 business days"

	claims := ExtractClaims(response)
	if len(claims) != 2 {
		t.Fatalf("expected 2 bullet claims, got %d: %+v", len(claims), claims)
	}
	if !claims[1].HasCanonicalAnchor {
		t.Fatalf("expected second bullet to detect canonical anchor: %+v", claims[1])
	}
}

func TestVerifyResponse_FailsWeakProvenanceCoverage(t *testing.T) {
	ctx := VerificationContext{
		UserPrompt:      "What is the current refund policy and has it been consistent?",
		PolicySensitive: true,
		EvidenceItems: []string{
			"Refund policy version 2 covers in-store purchases",
		},
	}

	// Three absolute claims, none cover the narrow single evidence item well.
	response := "Customers always receive a full refund. This has been true without exception since 2020. Refunds are guaranteed within 24 hours."
	result := VerifyResponse(response, ctx)

	if result.Passed {
		t.Fatalf("expected provenance-coverage failure, got pass with issues %+v", result.Issues)
	}
	if !hasIssue(result, "weak_provenance_coverage") {
		t.Fatalf("expected weak_provenance_coverage issue, got %+v", result.Issues)
	}
}

func TestVerifyResponse_PassesWhenAbsoluteClaimsAreCanonicallyAnchored(t *testing.T) {
	ctx := VerificationContext{
		UserPrompt:      "What is the current refund policy?",
		PolicySensitive: true,
		EvidenceItems: []string{
			"Refund policy version 2 allows refunds within 30 days for unused purchases",
		},
	}

	response := "According to the current policy, customers always receive a full refund within 30 days for unused purchases. According to the documentation, refunds are guaranteed for in-store items."
	result := VerifyResponse(response, ctx)

	if !result.Passed {
		t.Fatalf("expected anchored absolute claims to pass, got %+v", result.Issues)
	}
}

func TestVerifyResponse_FailsUnresolvedContradictedClaim(t *testing.T) {
	ctx := VerificationContext{
		UserPrompt:        "Which refund window applies?",
		HasContradictions: true,
		PolicySensitive:   true,
		Contradictions: []session.ContradictionEvidence{
			{
				Kind:           "value_conflict",
				Topic:          "refund window",
				Summary:        "refund window evidence disagrees",
				SharedKeywords: []string{"refund", "window"},
				EvidenceItems: []string{
					"Refund policy v1 specified a 30-day refund window",
					"Refund policy v2 specified a 60-day refund window",
				},
			},
		},
		EvidenceItems: []string{
			"Refund policy v1 specified a 30-day refund window",
			"Refund policy v2 specified a 60-day refund window",
		},
	}

	response := "The refund window is always 30 days."
	result := VerifyResponse(response, ctx)

	if result.Passed {
		t.Fatalf("expected contradiction reconciliation failure, got pass")
	}
	if !hasIssue(result, "unresolved_contradicted_claim") {
		// It is acceptable for the block-level ignored_contradictions check to
		// also fire, but the claim-level reason must be listed.
		t.Fatalf("expected unresolved_contradicted_claim issue, got %+v", result.Issues)
	}
}

func TestVerifyResponse_PassesWhenAbsoluteClaimAcknowledgesConflict(t *testing.T) {
	ctx := VerificationContext{
		UserPrompt:        "Which refund window applies?",
		HasContradictions: true,
		Contradictions: []session.ContradictionEvidence{
			{
				Kind:           "value_conflict",
				Topic:          "refund window",
				Summary:        "refund window evidence disagrees",
				SharedKeywords: []string{"refund", "window"},
				EvidenceItems: []string{
					"Refund policy v1 specified a 30-day refund window",
					"Refund policy v2 specified a 60-day refund window",
				},
			},
		},
		EvidenceItems: []string{
			"Refund policy v1 specified a 30-day refund window",
			"Refund policy v2 specified a 60-day refund window",
		},
	}

	response := "Sources disagree: v1 always required 30 days, but v2 always required 60 days. Depending on which revision applies, the right answer differs."
	result := VerifyResponse(response, ctx)

	if !result.Passed {
		t.Fatalf("expected conflict-acknowledging response to pass, got %+v", result.Issues)
	}
}

func TestVerifyResponse_FailsUnsupportedAbsoluteClaimOnHighRiskQuery(t *testing.T) {
	ctx := VerificationContext{
		UserPrompt:      "Is this refund flow compliant with our policy?",
		PolicySensitive: true,
		EvidenceItems: []string{
			"Shipping carrier list updated last quarter",
		},
	}

	response := "The refund flow is definitely compliant with every policy. Refunds are guaranteed to complete within 60 seconds."
	result := VerifyResponse(response, ctx)

	if result.Passed {
		t.Fatalf("expected unsupported absolute claim failure, got pass")
	}
	if !hasAnyIssue(result, "weak_provenance_coverage", "unsupported_absolute_claim") {
		t.Fatalf("expected a claim-level failure, got %+v", result.Issues)
	}
}

func TestVerifyResponse_ClaimLevelChecksSilentWithoutEvidence(t *testing.T) {
	ctx := VerificationContext{
		UserPrompt:      "What's the refund policy?",
		PolicySensitive: true,
	}

	// Absolute-sounding but evidenceless prompts should not trip the claim-level
	// checks (they trip the earlier canonical_source_omitted if relevant).
	response := "Refund timing depends on the payment method. Customers typically see credit within 5 business days."
	result := VerifyResponse(response, ctx)

	// Must not add claim-level issues when EvidenceItems is empty.
	for _, issue := range result.Issues {
		if strings.HasPrefix(issue.Code, "weak_provenance") || strings.HasPrefix(issue.Code, "unsupported_absolute") {
			t.Fatalf("unexpected claim-level issue when no evidence present: %+v", issue)
		}
	}
}

func TestVerifyResponse_FailsProofObligationOnFinancialIntent(t *testing.T) {
	ctx := VerificationContext{
		UserPrompt:    "Is this refund financially compliant with our accounting controls?",
		Intents:       []string{"financial_action"},
		EvidenceItems: []string{"Ledger reconciliation batch 42 completed"},
	}
	// Absolute claim, no canonical anchor, evidence does not carry a
	// canonical marker — fails the per-intent proof obligation.
	response := "Yes, the refund is always compliant with the accounting controls. It is definitely within every audit bucket."
	result := VerifyResponse(response, ctx)
	if result.Passed {
		t.Fatalf("expected proof-obligation failure on financial intent, got pass with %+v", result.Issues)
	}
	if !hasIssue(result, "proof_obligation_unmet") {
		t.Fatalf("expected proof_obligation_unmet issue, got %+v", result.Issues)
	}
}

func TestVerifyResponse_ClaimAuditCarriesMissingProofRequirements(t *testing.T) {
	ctx := VerificationContext{
		UserPrompt:      "Is the refund flow compliant with the current policy?",
		PolicySensitive: true,
		EvidenceItems: []string{
			"Shipping carrier list updated last quarter",
		},
	}

	response := "The refund flow definitely complies with every policy requirement."
	result := VerifyResponse(response, ctx)

	if result.Passed {
		t.Fatalf("expected proof-style verifier failure, got pass")
	}
	if !hasIssue(result, "proof_obligation_unmet") {
		t.Fatalf("expected proof_obligation_unmet issue, got %+v", result.Issues)
	}
	if len(result.ClaimAudits) == 0 {
		t.Fatal("expected claim audits")
	}
	audit := result.ClaimAudits[0]
	if len(audit.ProofRequired) == 0 {
		t.Fatalf("expected proof requirements on claim audit, got %+v", audit)
	}
	if len(audit.MissingProof) == 0 {
		t.Fatalf("expected missing proof details on claim audit, got %+v", audit)
	}
}

func TestVerifyResponse_PassesProofObligationWithCanonicalEvidence(t *testing.T) {
	ctx := VerificationContext{
		UserPrompt: "Does the refund flow satisfy our compliance policy?",
		Intents:    []string{"compliance"},
		EvidenceItems: []string{
			"Refund compliance policy v3 authoritative documentation covers refund timing",
		},
	}
	// Absolute claim with evidence that carries the canonical marker — passes
	// the per-intent proof obligation without needing explicit anchor text.
	response := "The refund flow always satisfies the compliance policy for the documented refund timing."
	result := VerifyResponse(response, ctx)
	if !result.Passed {
		t.Fatalf("expected canonical-evidence-supported response to pass, got %+v", result.Issues)
	}
}

func TestVerifyResponse_PassesProofObligationWithExplicitAnchor(t *testing.T) {
	ctx := VerificationContext{
		UserPrompt:    "Is this payout compliant with KYC rules?",
		Intents:       []string{"financial_action"},
		EvidenceItems: []string{"Payout batch 17 recorded in ledger"},
	}
	// Absolute claim with explicit "according to ..." anchor — passes.
	response := "According to the current policy, the payout is always compliant with KYC rules for verified counterparties."
	result := VerifyResponse(response, ctx)
	if !result.Passed {
		t.Fatalf("expected explicitly anchored response to pass, got %+v", result.Issues)
	}
}

func TestVerifyResponse_ProofObligationSilentWithoutHighRiskIntent(t *testing.T) {
	ctx := VerificationContext{
		UserPrompt:    "What's the weather like?",
		Intents:       []string{"conversation"},
		EvidenceItems: []string{"Weather report for Berlin"},
	}
	response := "It is always sunny in the afternoon. Weather is definitely warm."
	result := VerifyResponse(response, ctx)
	for _, issue := range result.Issues {
		if issue.Code == "proof_obligation_unmet" {
			t.Fatalf("should not fire proof obligation on low-risk intent, got %+v", result.Issues)
		}
	}
}

func hasIssue(result VerificationResult, code string) bool {
	for _, issue := range result.Issues {
		if issue.Code == code {
			return true
		}
	}
	return false
}

func hasAnyIssue(result VerificationResult, codes ...string) bool {
	set := make(map[string]struct{}, len(codes))
	for _, c := range codes {
		set[c] = struct{}{}
	}
	for _, issue := range result.Issues {
		if _, ok := set[issue.Code]; ok {
			return true
		}
	}
	return false
}
