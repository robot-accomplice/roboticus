package session

import "testing"

func TestSetMemoryContext_DerivesVerificationEvidence(t *testing.T) {
	sess := New("s1", "a1", "bot")
	sess.SetMemoryContext("[Active Memory]\n\n[Working State]\nExecutive State:\nUnresolved questions:\n- is rollout blocked by legal?\nStopping criteria:\n- ship PR with tests (all tests green)\n\n[Retrieved Evidence]\n1. [semantic, 0.92, canonical] deploy doc\n\n[Gaps]\n- missing procedural evidence\n")

	ve := sess.VerificationEvidence()
	if ve == nil {
		t.Fatal("expected derived verification evidence")
	}
	if !ve.HasEvidence || !ve.HasGaps || !ve.HasCanonicalEvidence {
		t.Fatalf("expected derived section flags, got %+v", ve)
	}
	if len(ve.EvidenceItems) != 1 || ve.EvidenceItems[0] != "deploy doc" {
		t.Fatalf("unexpected derived evidence items: %+v", ve.EvidenceItems)
	}
	if len(ve.UnresolvedQuestions) != 1 || ve.UnresolvedQuestions[0] != "is rollout blocked by legal?" {
		t.Fatalf("unexpected unresolved questions: %+v", ve.UnresolvedQuestions)
	}
	if len(ve.StoppingCriteria) != 1 || ve.StoppingCriteria[0] != "ship PR with tests" {
		t.Fatalf("unexpected stopping criteria: %+v", ve.StoppingCriteria)
	}
}

func TestSetMemoryContext_DoesNotOverrideExplicitVerificationEvidence(t *testing.T) {
	sess := New("s1", "a1", "bot")
	explicit := &VerificationEvidence{
		HasEvidence:          true,
		HasCanonicalEvidence: true,
		EvidenceItems:        []string{"typed evidence"},
	}
	sess.SetVerificationEvidence(explicit)
	sess.SetMemoryContext("[Retrieved Evidence]\n1. [semantic, 0.92] text evidence\n")

	if sess.VerificationEvidence() != explicit {
		t.Fatal("expected explicit verification evidence to win over derived compatibility artifact")
	}
}
