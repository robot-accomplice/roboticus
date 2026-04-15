package pipeline

import (
	"context"
	"strings"
	"testing"

	"roboticus/internal/llm"
)

// deterministicCertaintyClassifier builds a SemanticClassifier whose
// centroids and query embedder are fully under test control. The corpus
// uses orthogonal one-hot vectors so the centroids are clean per category,
// and the queryEmbedFn maps each test sentence to a known vector. This
// lets tests prove the verifier wiring works without depending on a real
// embedding provider.
//
// vectors:
//   absolute → [1, 0, 0]
//   high     → [0, 1, 0]
//   hedged   → [0, 0, 1]
func deterministicCertaintyClassifier(queryMap map[string]string) *llm.SemanticClassifier {
	c := llm.NewSemanticClassifier(nil, nil)
	c.WithAbstainPolicy(llm.AbstainPolicy{MinScore: 0.30, MinGap: 0.10})
	c.SetCorpusVectors([]llm.ClassifierExample{
		{Intent: "absolute", Vector: []float32{1, 0, 0}},
		{Intent: "high", Vector: []float32{0, 1, 0}},
		{Intent: "hedged", Vector: []float32{0, 0, 1}},
	})
	c.SetQueryEmbedFn(func(text string) []float32 {
		switch queryMap[text] {
		case "absolute":
			return []float32{1, 0, 0}
		case "high":
			return []float32{0, 1, 0}
		case "hedged":
			return []float32{0, 0, 1}
		}
		// Unknown sentence → midpoint vector that abstains cleanly.
		return []float32{0.4, 0.4, 0.4}
	})
	return c
}

func TestNewClaimCertaintyClassifier_PreEmbedsCorpus(t *testing.T) {
	c := NewClaimCertaintyClassifier(nil)
	if c == nil {
		t.Fatal("expected classifier instance")
	}
	// A bare classify must succeed because PrepareCorpus ran on construction.
	intent, score, err := c.Classify(context.Background(), "we are sceptical that this works")
	if err != nil {
		t.Fatalf("classify: %v", err)
	}
	// We only assert the call shape — score depends on n-gram embeddings,
	// but it must not be zero (which would mean centroids were empty).
	if score == 0 && intent == "unknown" {
		t.Fatal("expected non-zero score after PrepareCorpus")
	}
}

func TestClassifyCertaintySemantic_RecognisesParaphrasedHedge(t *testing.T) {
	c := deterministicCertaintyClassifier(map[string]string{
		"we hold significant doubt about whether this approach is right": "hedged",
	})
	got, ok := classifyCertaintySemantic(context.Background(), c,
		"we hold significant doubt about whether this approach is right")
	if !ok {
		t.Fatalf("expected classifier to assign a non-default certainty, got default")
	}
	if got != CertaintyHedged {
		t.Fatalf("expected CertaintyHedged for the paraphrased hedge, got %s", got)
	}
}

func TestClassifyCertaintySemantic_NilClassifierReturnsModerate(t *testing.T) {
	got, ok := classifyCertaintySemantic(context.Background(), nil, "anything")
	if ok {
		t.Fatal("expected nil classifier to report no upgrade")
	}
	if got != CertaintyModerate {
		t.Fatalf("expected CertaintyModerate fallback, got %s", got)
	}
}

func TestUpgradeClaimCertaintyWithClassifier_LeavesLexicalTagsAlone(t *testing.T) {
	// Even if the classifier WOULD say "hedged", an already-absolute claim
	// must stay absolute — the upgrade only fires on Moderate.
	c := deterministicCertaintyClassifier(map[string]string{
		"Customers always receive a refund.": "hedged",
	})
	claims := []VerifiedClaim{{
		Sentence:  "Customers always receive a refund.",
		Lower:     "customers always receive a refund.",
		Certainty: CertaintyAbsolute,
	}}
	upgradeClaimCertaintyWithClassifier(claims, c)
	if claims[0].Certainty != CertaintyAbsolute {
		t.Fatalf("classifier must not overwrite a lexical tag, got %s", claims[0].Certainty)
	}
}

func TestUpgradeClaimCertaintyWithClassifier_OnlyTouchesModerate(t *testing.T) {
	c := deterministicCertaintyClassifier(map[string]string{
		"a paraphrased hedge sentence": "hedged",
		"a paraphrased high sentence":  "high",
	})
	claims := []VerifiedClaim{
		{Sentence: "a paraphrased hedge sentence", Certainty: CertaintyModerate},
		{Sentence: "a paraphrased high sentence", Certainty: CertaintyHigh}, // already tagged
	}
	upgradeClaimCertaintyWithClassifier(claims, c)
	if claims[0].Certainty != CertaintyHedged {
		t.Fatalf("expected moderate paraphrase to upgrade to hedged, got %s", claims[0].Certainty)
	}
	if claims[1].Certainty != CertaintyHigh {
		t.Fatalf("classifier must not touch already-tagged claims, got %s", claims[1].Certainty)
	}
}

func TestVerifyResponse_ClassifierEnabledFlagsParaphrasedAbsolute(t *testing.T) {
	// The whole point of the M6 follow-on: a paraphrased absolute that the
	// lexical markers miss should still trigger the per-claim proof
	// obligation on a high-risk query.
	//
	// Test sentence is intentionally lexical-marker-free: no "always",
	// "never", "is/was/are", "must", etc. The classifier is the only
	// thing that can label it absolute.
	sentence := "this represents the singular operating posture forever."
	c := deterministicCertaintyClassifier(map[string]string{sentence: "absolute"})

	ctx := VerificationContext{
		UserPrompt: "Is this refund flow compliant?",
		Intents:    []string{"financial_action"},
		EvidenceItems: []string{
			"Some unrelated evidence item with no useful keywords",
		},
		CertaintyClassifier: c,
	}
	result := VerifyResponse(sentence, ctx)
	if result.Passed {
		t.Fatalf("expected proof obligation failure on paraphrased absolute, got pass with %+v", result.Issues)
	}
	if !hasAnyIssue(result, "proof_obligation_unmet", "unsupported_absolute_claim") {
		t.Fatalf("expected an absolute-claim issue, got %+v", result.Issues)
	}
}

func TestVerifyResponse_ClassifierDisabledKeepsLexicalOnlyBehaviour(t *testing.T) {
	// Same lexical-marker-free sentence, but no classifier. The lexical
	// pass alone leaves it at CertaintyModerate, so the proof obligation
	// does NOT fire — proving the upgrade is doing the work in the
	// previous test rather than something else.
	ctx := VerificationContext{
		UserPrompt: "Is this refund flow compliant?",
		Intents:    []string{"financial_action"},
		EvidenceItems: []string{
			"Some unrelated evidence item with no useful keywords",
		},
		// CertaintyClassifier deliberately nil.
	}
	response := "this represents the singular operating posture forever."
	result := VerifyResponse(response, ctx)
	for _, issue := range result.Issues {
		if strings.HasPrefix(issue.Code, "proof_obligation") || strings.HasPrefix(issue.Code, "unsupported_absolute") {
			t.Fatalf("expected no absolute-claim issue without classifier, got %+v", result.Issues)
		}
	}
}

func TestNewClaimCertaintyClassifier_CorpusCoversFailureModes(t *testing.T) {
	// A small smoke check that the corpus actually covers the design's
	// stated failure modes: every category is represented and unique.
	seen := map[string]int{}
	for _, ex := range claimCertaintyExamples() {
		seen[ex.Intent]++
	}
	for _, want := range []string{"absolute", "high", "hedged"} {
		if seen[want] < 5 {
			t.Fatalf("corpus too sparse for category %q (got %d, want >= 5)", want, seen[want])
		}
	}
}
