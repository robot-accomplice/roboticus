// verification_evidence.go defines the typed evidence artifact the
// pipeline's retrieval stage hands to the verifier stage — replacing
// the pre-v1.0.6 pattern where the verifier `strings.Contains`'d its
// way through the rendered `[Retrieved Evidence] / [Gaps] / [Freshness
// Risks]` markers inside the memory context text.
//
// Why the string-parsing pattern was flagged: the text is produced by
// memory.AssembledContext.Format() in internal/agent/memory/context_assembly.go,
// and the verifier in internal/pipeline/verifier.go read those same
// markers from the rendered output. A stylistic change in the
// assembler — rewriting "[Retrieved Evidence]" to "## Evidence" for
// example — would silently break verifier detection with no
// compile-time error. That's a fragile clean-architecture seam.
//
// The typed artifact lives in the session package because session is
// the leaf that BOTH the pipeline (produces it) and the verifier
// (consumes it) can depend on without creating an import cycle:
//   memory imports session  (producer populates)
//   pipeline imports session (verifier reads)
// Putting the struct in memory would force session to import memory,
// which is the wrong direction; putting it in pipeline would force
// memory to import pipeline, same problem. session is the neutral
// ground.

package session

// VerificationEvidence is the typed view of memory-retrieval output
// that verifier.BuildVerificationContext consumes. Every field is a
// boolean or a slice of short strings — no nested structure, no
// format-sensitive markers. The pipeline is responsible for building
// this from the structured AssembledContext; the verifier reads it
// verbatim.
//
// Nil or zero-valued fields are legitimate: they mean "this section
// is not present in the retrieval output." The verifier tolerates
// nil slices and zero booleans gracefully.
type VerificationEvidence struct {
	// Section presence flags — set true when the corresponding section
	// was produced by the assembler with non-empty content.
	HasEvidence          bool
	HasGaps              bool
	HasFreshnessRisks    bool
	HasContradictions    bool
	HasCanonicalEvidence bool

	// EvidenceItems is the flat list of individual evidence bullet
	// points extracted from the [Retrieved Evidence] section. Bullets
	// preserve source/tier tags inline so the verifier can reason
	// about provenance without re-parsing.
	EvidenceItems []string

	// Executive state surfaced by the pipeline's task synthesis stage
	// into the assembled context. Each slice is the set of short
	// summary strings — NOT the rendered bullet text.
	UnresolvedQuestions []string
	VerifiedConclusions []string
	StoppingCriteria    []string
}

// SetVerificationEvidence attaches a typed evidence artifact to the
// session. The pipeline's Stage 8.5 calls this after retrieval
// completes so later stages (verifier) can read structured fields
// instead of text-parsing the rendered memory block.
//
// Passing nil is an explicit "no structured evidence available" signal
// — the verifier falls back to string parsing of MemoryContext() for
// backward compat with callers that don't go through the full pipeline
// (tests, smoke scripts, ad-hoc harness invocations).
func (s *Session) SetVerificationEvidence(ve *VerificationEvidence) {
	s.verificationEvidence = ve
}

// VerificationEvidence returns the typed evidence artifact set by the
// pipeline, or nil if none was attached. Callers MUST handle nil (see
// SetVerificationEvidence doc for why it's allowed).
func (s *Session) VerificationEvidence() *VerificationEvidence {
	return s.verificationEvidence
}
