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
		EvidenceItems:        []string{"[semantic, 0.91] canonical rule X", "[procedural, 0.80] routine Y"},
		UnresolvedQuestions:  []string{"Will X apply to Y?"},
		VerifiedConclusions:  []string{"X is canonical per the spec."},
		StoppingCriteria:     []string{"All subgoals checked off."},
	})

	vctx := BuildVerificationContext(sess)

	if !vctx.HasEvidence || !vctx.HasGaps || !vctx.HasFreshnessRisk ||
		!vctx.HasContradictions || !vctx.HasCanonicalEvidence {
		t.Fatalf("verifier should have picked up all flags from typed artifact; got %+v", vctx)
	}
	if len(vctx.EvidenceItems) != 2 {
		t.Fatalf("EvidenceItems len = %d; want 2", len(vctx.EvidenceItems))
	}
	if len(vctx.UnresolvedQuestions) != 1 || len(vctx.VerifiedConclusions) != 1 || len(vctx.StoppingCriteria) != 1 {
		t.Fatalf("exec-state slices wrong lengths: %+v", vctx)
	}
}

// TestBuildVerificationContext_StringParseFallback confirms the
// fallback path still works for callers that don't flow through the
// pipeline (tests, smoke scripts, ad-hoc CLIs). If VerificationEvidence
// is nil the verifier must reach for the rendered markers instead.
func TestBuildVerificationContext_StringParseFallback(t *testing.T) {
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
		t.Fatalf("fallback string-parse should set HasEvidence from [Retrieved Evidence] marker")
	}
	if !vctx.HasGaps {
		t.Fatalf("fallback string-parse should set HasGaps from [Gaps] marker")
	}
	if !vctx.HasCanonicalEvidence {
		t.Fatalf("fallback should detect 'canonical' substring")
	}
}
