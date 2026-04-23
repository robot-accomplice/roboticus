package pipeline

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSummarizeVerification_CountsAbsoluteSupport(t *testing.T) {
	ctx := VerificationContext{
		UserPrompt:      "Is the refund flow compliant?",
		PolicySensitive: true,
		EvidenceItems: []string{
			"Refund policy version 2 allows refunds within 30 days",
		},
	}
	// Two absolute claims: one supported, one orphaned. One hedged claim.
	response := "According to the current policy, refunds are always allowed within 30 days. Customers definitely receive refunds within 24 hours. Maybe the auth step is optional."
	result := VerifyResponse(response, ctx)
	summary := SummarizeVerification(result)

	if summary.ClaimCount == 0 {
		t.Fatalf("expected claim audits populated, got %+v", result.ClaimAudits)
	}
	if summary.AbsoluteCount < 2 {
		t.Fatalf("expected at least 2 absolute claims, got %+v", summary)
	}
	if summary.AnchoredCount < 1 {
		t.Fatalf("expected anchored count >= 1, got %+v", summary)
	}
	if summary.CoverageRatio <= 0 || summary.CoverageRatio > 1 {
		t.Fatalf("coverage ratio out of range: %+v", summary)
	}
}

func TestSummarizeVerification_CountsContestedAndProofGapClaims(t *testing.T) {
	result := VerificationResult{
		Passed: false,
		ClaimAudits: []ClaimAudit{
			{
				Sentence:       "Sources disagree on the refund window.",
				Certainty:      CertaintyAbsolute.String(),
				Supported:      true,
				Contested:      true,
				Reconciled:     true,
				ProofSatisfied: true,
			},
			{
				Sentence:     "The refund flow definitely complies.",
				Certainty:    CertaintyAbsolute.String(),
				Supported:    false,
				Contested:    false,
				MissingProof: []string{"evidence_support", "canonical_anchor"},
				IssueCode:    "proof_obligation_unmet",
			},
		},
	}

	summary := SummarizeVerification(result)
	if summary.ContestedCount != 1 {
		t.Fatalf("expected contested count 1, got %+v", summary)
	}
	if summary.ProofGapCount != 1 {
		t.Fatalf("expected proof gap count 1, got %+v", summary)
	}
	if summary.ReconciledCount != 1 {
		t.Fatalf("expected reconciled count 1, got %+v", summary)
	}
}

func TestClaimAudit_MarksUnsupportedAbsoluteClaim(t *testing.T) {
	ctx := VerificationContext{
		UserPrompt:      "Is the refund flow compliant?",
		PolicySensitive: true,
		EvidenceItems: []string{
			"Shipping carrier list updated last quarter",
		},
	}
	response := "The refund flow is definitely compliant with every policy. Refunds are guaranteed within 60 seconds."
	result := VerifyResponse(response, ctx)

	flagged := 0
	for _, audit := range result.ClaimAudits {
		if audit.IssueCode == "unsupported_absolute_claim" {
			flagged++
		}
	}
	if flagged == 0 {
		t.Fatalf("expected at least one claim audit marked unsupported, got %+v", result.ClaimAudits)
	}
}

func TestAnnotateVerifierTrace_EmitsClaimMap(t *testing.T) {
	tr := NewTraceRecorder()
	tr.BeginSpan("inference")

	result := VerifyResponse("Customers definitely always receive refunds.", VerificationContext{
		UserPrompt:      "What's the refund policy?",
		PolicySensitive: true,
		EvidenceItems:   []string{"Refund policy version 2 allows refunds"},
	})
	AnnotateVerifierTrace(tr, result)
	tr.EndSpan("ok")

	trace := tr.Finish("turn-1", "test")
	if len(trace.Stages) == 0 {
		t.Fatal("expected at least one stage")
	}
	meta := trace.Stages[0].Metadata
	if meta == nil {
		t.Fatal("expected metadata on span")
	}
	if _, ok := meta["verifier.passed"]; !ok {
		t.Fatalf("expected verifier.passed annotation, got %+v", meta)
	}
	if _, ok := meta["verifier.claim_count"]; !ok {
		t.Fatalf("expected verifier.claim_count annotation")
	}
	if _, ok := meta["verifier.contested_count"]; !ok {
		t.Fatalf("expected verifier.contested_count annotation")
	}
	if _, ok := meta["verifier.proof_gap_count"]; !ok {
		t.Fatalf("expected verifier.proof_gap_count annotation")
	}

	raw, ok := meta["verifier.claim_map_json"].(string)
	if !ok || raw == "" {
		t.Fatalf("expected verifier.claim_map_json to be a non-empty JSON string, got %+v", meta["verifier.claim_map_json"])
	}
	var audits []ClaimAudit
	if err := json.Unmarshal([]byte(raw), &audits); err != nil {
		t.Fatalf("claim map is not valid JSON: %v", err)
	}
	if len(audits) == 0 {
		t.Fatal("expected at least one claim audit in the trace")
	}
	if !strings.Contains(strings.ToLower(audits[0].Sentence), "refund") {
		t.Fatalf("expected the audit to carry the original sentence, got %q", audits[0].Sentence)
	}
}

func TestSummarizeVerification_EmptyResult(t *testing.T) {
	summary := SummarizeVerification(VerificationResult{Passed: true})
	if !summary.Passed {
		t.Fatal("empty result should be passed")
	}
	if summary.ClaimCount != 0 {
		t.Fatalf("empty result should have no claims, got %d", summary.ClaimCount)
	}
	if summary.CoverageRatio != 0 {
		t.Fatalf("empty result should have zero coverage ratio, got %f", summary.CoverageRatio)
	}
}
