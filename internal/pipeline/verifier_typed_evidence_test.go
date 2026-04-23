package pipeline

import (
	"strings"
	"testing"

	"roboticus/internal/session"
)

// TestBuildVerificationContext_PrefersTypedEvidence is the v1.0.6 P2-C
// regression. Pre-fix, BuildVerificationContext string-parsed session
// MemoryContext() for "[Retrieved Evidence]", "[Gaps]",
// "[Freshness Risks]", "[Contradictions]" — a format-sensitive coupling
// that would silently break if the assembler changed marker strings.
//
// This test attaches a typed VerificationEvidence artifact to the
// session (what the pipeline's Stage 8.5 does via the evidence sink)
// AND populates MemoryContext with a DELIBERATELY DIVERGENT text body
// (no "[Retrieved Evidence]" marker, no "canonical" substring). The
// verifier should prefer the typed artifact and report HasEvidence=true,
// HasCanonicalEvidence=true, not the false the string-parse would
// produce.
//
// If a future refactor regresses to string-parsing as primary (ignoring
// session.VerificationEvidence()), this test fails immediately.
func TestBuildVerificationContext_PrefersTypedEvidence(t *testing.T) {
	sess := session.New("s1", "a1", "Test")
	sess.AddUserMessage("some question")

	// Memory context: deliberately empty of the pre-fix marker strings,
	// so the string-parse fallback would return all-false.
	sess.SetMemoryContext("Totally unstructured memory prose with no section headers at all.")

	// Typed artifact: says yes on everything the string-parse would
	// have said no on.
	sess.SetVerificationEvidence(&session.VerificationEvidence{
		HasEvidence:          true,
		HasGaps:              true,
		HasFreshnessRisks:    true,
		HasContradictions:    true,
		HasCanonicalEvidence: true,
		Contradictions: []session.ContradictionEvidence{
			{Kind: "value_conflict", Topic: "refund window", Summary: "refund window evidence disagrees"},
		},
		EvidenceItems:       []string{"[semantic, 0.91] canonical rule X", "[procedural, 0.80] routine Y"},
		UnresolvedQuestions: []string{"Will X apply to Y?"},
		VerifiedConclusions: []string{"X is canonical per the spec."},
		StoppingCriteria:    []string{"All subgoals checked off."},
	})

	vctx := BuildVerificationContext(sess)

	if !vctx.HasEvidence || !vctx.HasGaps || !vctx.HasFreshnessRisk ||
		!vctx.HasContradictions || !vctx.HasCanonicalEvidence {
		t.Fatalf("verifier should have picked up all flags from typed artifact; got %+v", vctx)
	}
	if len(vctx.EvidenceItems) != 2 {
		t.Fatalf("EvidenceItems len = %d; want 2", len(vctx.EvidenceItems))
	}
	if len(vctx.Contradictions) != 1 || vctx.Contradictions[0].Topic != "refund window" {
		t.Fatalf("typed contradiction artifact not preserved: %+v", vctx.Contradictions)
	}
	if len(vctx.UnresolvedQuestions) != 1 || len(vctx.VerifiedConclusions) != 1 || len(vctx.StoppingCriteria) != 1 {
		t.Fatalf("exec-state slices wrong lengths: %+v", vctx)
	}
}

// TestBuildVerificationContext_CompatibilityDerivesTypedEvidence confirms
// compatibility callers still work: SetMemoryContext derives a typed
// VerificationEvidence artifact at the session boundary, so the verifier
// itself stays on typed data only.
func TestBuildVerificationContext_CompatibilityDerivesTypedEvidence(t *testing.T) {
	sess := session.New("s1", "a1", "Test")
	sess.AddUserMessage("q")

	// No SetVerificationEvidence call → nil artifact.
	sess.SetMemoryContext(strings.Join([]string{
		"[Active Memory]",
		"",
		"[Working State]",
		"- some state",
		"",
		"[Retrieved Evidence]",
		"1. [semantic, 0.92, canonical] some snippet",
		"",
		"[Gaps]",
		"- missing tier X",
	}, "\n"))

	vctx := BuildVerificationContext(sess)
	if !vctx.HasEvidence {
		t.Fatalf("expected session-boundary compatibility derivation to set HasEvidence")
	}
	if !vctx.HasGaps {
		t.Fatalf("expected session-boundary compatibility derivation to set HasGaps")
	}
	if !vctx.HasCanonicalEvidence {
		t.Fatalf("expected compatibility derivation to detect canonical qualifier inside bracketed evidence row")
	}
}

// TestBuildVerificationContext_CompatibilityCanonicalNoFalsePositive covers
// the same canonical-detection hazard on the session-boundary
// compatibility path.
func TestBuildVerificationContext_CompatibilityCanonicalNoFalsePositive(t *testing.T) {
	sess := session.New("s1", "a1", "Test")
	sess.AddUserMessage("q")

	// Memory context mentions "canonical" in narrative prose but has
	// NO bracketed evidence row with the canonical qualifier. Pre-fix
	// the naked strings.Contains would return true here; post-fix it
	// must return false.
	sess.SetMemoryContext(strings.Join([]string{
		"[Active Memory]",
		"",
		"[Working State]",
		"- user is researching whether the RFC is the canonical source",
		"",
		"[Retrieved Evidence]",
		"1. [semantic, 0.77, via=fts] the rfc describes a protocol",
	}, "\n"))

	vctx := BuildVerificationContext(sess)
	if vctx.HasCanonicalEvidence {
		t.Fatalf("compatibility derivation should NOT false-positive on prose mentions of 'canonical'")
	}
}
