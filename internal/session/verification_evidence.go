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

import (
	"regexp"
	"strings"
)

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
// completes so later stages (verifier) can read structured fields.
//
// Passing nil is an explicit "no structured evidence available" signal.
// Compatibility callers that only set rendered memory text are normalized
// by Session.SetMemoryContext(), not by downstream consumers.
func (s *Session) SetVerificationEvidence(ve *VerificationEvidence) {
	s.verificationEvidence = ve
	s.verificationEvidenceDerived = false
}

// VerificationEvidence returns the typed evidence artifact set by the
// pipeline or derived from rendered memory text at the session boundary,
// or nil if neither exists.
func (s *Session) VerificationEvidence() *VerificationEvidence {
	return s.verificationEvidence
}

// canonicalQualifierRegex matches the assembler's "canonical"
// qualifier only when it appears inside a bracketed evidence-row meta
// block, e.g. `[semantic, 0.91, canonical]`.
var canonicalQualifierRegex = regexp.MustCompile(`\[[^\]]*\bcanonical\b[^\]]*\]`)

// deriveVerificationEvidenceFromMemoryContext is the compatibility bridge for
// callers that still only set rendered memory text on the session. The session
// owns this format-sensitive normalization so downstream consumers can stay on
// typed artifacts only.
func deriveVerificationEvidenceFromMemoryContext(memoryContext string) *VerificationEvidence {
	if strings.TrimSpace(memoryContext) == "" {
		return nil
	}
	ve := &VerificationEvidence{
		HasEvidence:          strings.Contains(memoryContext, "[Retrieved Evidence]"),
		HasGaps:              strings.Contains(memoryContext, "[Gaps]"),
		HasFreshnessRisks:    strings.Contains(memoryContext, "[Freshness Risks]"),
		HasContradictions:    strings.Contains(memoryContext, "[Contradictions]"),
		HasCanonicalEvidence: canonicalQualifierRegex.MatchString(memoryContext),
		EvidenceItems:        verificationSectionItems(memoryContext, "[Retrieved Evidence]"),
		UnresolvedQuestions:  verificationExecutiveSection(memoryContext, "Unresolved questions"),
		VerifiedConclusions:  verificationExecutiveSection(memoryContext, "Verified conclusions"),
		StoppingCriteria:     verificationExecutiveSection(memoryContext, "Stopping criteria"),
	}
	return ve
}

func verificationExecutiveSection(memoryContext, label string) []string {
	lines := strings.Split(memoryContext, "\n")
	var items []string
	inSection := false
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" {
			if inSection {
				break
			}
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			if inSection {
				break
			}
			continue
		}
		if strings.HasSuffix(line, ":") {
			if inSection {
				break
			}
			if strings.EqualFold(strings.TrimSuffix(line, ":"), label) {
				inSection = true
			}
			continue
		}
		if !inSection {
			continue
		}
		if strings.HasPrefix(line, "Task:") ||
			strings.HasPrefix(line, "Plan:") ||
			strings.HasPrefix(line, "Assumptions:") ||
			strings.HasPrefix(line, "Decision checkpoints:") ||
			strings.HasPrefix(line, "Verified conclusions:") ||
			strings.HasPrefix(line, "Unresolved questions:") ||
			strings.HasPrefix(line, "Stopping criteria:") {
			break
		}
		if strings.HasPrefix(line, "- ") {
			item := strings.TrimSpace(strings.TrimPrefix(line, "- "))
			item = trimTrailingParenthetical(item)
			if item != "" {
				items = append(items, item)
			}
		}
	}
	return items
}

func verificationSectionItems(memoryContext, header string) []string {
	lines := strings.Split(memoryContext, "\n")
	var items []string
	inSection := false
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" {
			if inSection {
				break
			}
			continue
		}
		if strings.HasPrefix(line, header) {
			inSection = true
			continue
		}
		if !inSection {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			break
		}
		if strings.HasPrefix(line, "Executive State:") {
			break
		}
		if idx := strings.Index(line, "] "); idx != -1 {
			items = append(items, strings.TrimSpace(line[idx+2:]))
		} else {
			items = append(items, line)
		}
	}
	return items
}

func trimTrailingParenthetical(s string) string {
	s = strings.TrimSpace(s)
	if !strings.HasSuffix(s, ")") {
		return s
	}
	open := strings.LastIndex(s, " (")
	if open == -1 {
		return s
	}
	return strings.TrimSpace(s[:open])
}
