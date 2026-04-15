// verifier_claims.go adds claim-level verification on top of the block-level
// checks in verifier.go. Where the base verifier asks broad questions such as
// "does the response acknowledge gaps or contradictions?", this file parses the
// response into individual claims, classifies their certainty, and checks each
// one against retrieved evidence and contradicted-evidence signals.
//
// Scope of the new checks:
//   - unresolved_contradicted_claim: an absolute claim echoes evidence that is
//     known to be contested, but the response does not reconcile the conflict.
//   - weak_provenance_coverage: on high-risk queries (policy, freshness,
//     contradictions, action-plan), fewer than half of the absolute claims
//     trace back to retrieved evidence or a canonical anchor.
//   - unsupported_absolute_claim: an absolute claim on a high-risk query has
//     no evidence support and no canonical anchor at all.
//
// These checks complement, not replace, the block-level checks: a response can
// pass the old checks and still fail here because it asserts specifics that
// the evidence cannot back up.

package pipeline

import (
	"fmt"
	"strings"
)

// ClaimCertainty classifies how strongly a claim is asserted.
type ClaimCertainty int

const (
	// CertaintyHedged includes explicit hedges ("maybe", "might", "could").
	CertaintyHedged ClaimCertainty = iota
	// CertaintyModerate is the default for assertive sentences without hedges.
	CertaintyModerate
	// CertaintyHigh marks sentences with direct "is/was/will" assertions.
	CertaintyHigh
	// CertaintyAbsolute flags universal quantifiers or unconditional language.
	CertaintyAbsolute
)

// String returns a human-readable label for logs and issue details.
func (c ClaimCertainty) String() string {
	switch c {
	case CertaintyHedged:
		return "hedged"
	case CertaintyModerate:
		return "moderate"
	case CertaintyHigh:
		return "high"
	case CertaintyAbsolute:
		return "absolute"
	default:
		return "unknown"
	}
}

// VerifiedClaim is a single sentence-level assertion extracted from a response.
type VerifiedClaim struct {
	Sentence           string
	Lower              string
	Certainty          ClaimCertainty
	HasCanonicalAnchor bool
	Keywords           []string
}

// absoluteMarkers trigger CertaintyAbsolute regardless of other signals.
var absoluteMarkers = []string{
	"definitely", "always", "never", "guaranteed", "certainly", "without exception",
	"in every case", "in all cases", "undoubtedly", "unquestionably", "invariably",
	"absolutely", "under no circumstances",
}

// hedgeMarkers pull a sentence down to CertaintyHedged.
var hedgeMarkers = []string{
	"maybe", "might", "may ", "perhaps", "possibly", "could be",
	"not sure", "i'm not certain", "i am not certain", "it seems",
	"appears to", "likely", "unlikely", "probably", "presumably",
	"based on the available", "from the available",
}

// highCertaintyMarkers push a sentence to CertaintyHigh when no absolute or
// hedge signal is present. These are simple indicative-mood cues.
var highCertaintyMarkers = []string{
	" is ", " was ", " are ", " were ", " will ", " has been ", " have been ",
	" does ", " did ", " must ",
}

// canonicalAnchorMarkers identify explicit source attribution within a claim.
var canonicalAnchorMarkers = []string{
	"according to", "per the canonical", "per the documented", "per the current",
	"the current policy", "the current rule", "the canonical source",
	"the documentation says", "the spec says", "official documentation",
	"per the policy", "as documented in", "cited in",
}

// ExtractClaims parses a response into individual claims with certainty and
// canonical anchor metadata. Empty/short sentences and questions are skipped.
func ExtractClaims(response string) []VerifiedClaim {
	if strings.TrimSpace(response) == "" {
		return nil
	}

	var claims []VerifiedClaim
	for _, raw := range splitIntoSentences(response) {
		sentence := strings.TrimSpace(raw)
		if sentence == "" {
			continue
		}
		if len(sentence) < 8 {
			continue
		}
		lower := strings.ToLower(sentence)

		// Skip pure questions — they make claims about the world only tangentially.
		if strings.HasSuffix(sentence, "?") {
			continue
		}

		claim := VerifiedClaim{
			Sentence:           sentence,
			Lower:              lower,
			Certainty:          classifyClaimCertainty(lower),
			HasCanonicalAnchor: claimHasCanonicalAnchor(lower),
			Keywords:           verificationKeywords(sentence),
		}
		claims = append(claims, claim)
	}
	return claims
}

// splitIntoSentences splits on sentence-ending punctuation and newlines while
// keeping short fragments attached to neighbors when punctuation was missing.
func splitIntoSentences(response string) []string {
	normalized := strings.ReplaceAll(response, "\r\n", "\n")
	// Split on newlines first so bullet lists become candidate sentences too.
	var candidates []string
	for _, line := range strings.Split(normalized, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Trim list bullets so "- foo" becomes "foo".
		line = strings.TrimLeft(line, "-*•·– ")
		candidates = append(candidates, line)
	}

	var sentences []string
	for _, candidate := range candidates {
		// Split on `. `, `! `, `? ` while preserving terminators so certainty
		// markers can still be detected (e.g., trailing exclamations amplify).
		for _, chunk := range splitKeepingDelimiters(candidate, []string{". ", "! ", "? "}) {
			chunk = strings.TrimSpace(chunk)
			if chunk != "" {
				sentences = append(sentences, chunk)
			}
		}
	}
	return sentences
}

// splitKeepingDelimiters splits a string on any of the given delimiters and
// keeps each delimiter attached to the preceding chunk so punctuation stays
// with its sentence.
func splitKeepingDelimiters(s string, delims []string) []string {
	if s == "" {
		return nil
	}
	out := []string{s}
	for _, d := range delims {
		var next []string
		for _, segment := range out {
			parts := strings.SplitAfter(segment, d)
			for _, p := range parts {
				if p != "" {
					next = append(next, p)
				}
			}
		}
		out = next
	}
	return out
}

func classifyClaimCertainty(lower string) ClaimCertainty {
	for _, m := range absoluteMarkers {
		if strings.Contains(lower, m) {
			return CertaintyAbsolute
		}
	}
	for _, m := range hedgeMarkers {
		if strings.Contains(lower, m) {
			return CertaintyHedged
		}
	}
	for _, m := range highCertaintyMarkers {
		if strings.Contains(lower, m) {
			return CertaintyHigh
		}
	}
	return CertaintyModerate
}

func claimHasCanonicalAnchor(lower string) bool {
	for _, m := range canonicalAnchorMarkers {
		if strings.Contains(lower, m) {
			return true
		}
	}
	return false
}

// claimEvidenceSupport counts how many meaningful keywords of a claim appear
// in any evidence item. Two or more matches, or one strong entity match, are
// treated as evidence support.
func claimEvidenceSupport(claim VerifiedClaim, evidenceItems []string) int {
	if len(claim.Keywords) == 0 || len(evidenceItems) == 0 {
		return 0
	}
	return verificationKeywordEvidenceMatches(claim.Keywords, evidenceItems)
}

// ClaimSupportedByEvidence returns true when a claim can be reasonably backed
// by the retrieved evidence via keyword overlap. Short-keyword claims only
// need a single strong entity match; richer claims require at least two.
func ClaimSupportedByEvidence(claim VerifiedClaim, evidenceItems []string) bool {
	matches := claimEvidenceSupport(claim, evidenceItems)
	if matches == 0 {
		return false
	}
	threshold := 1
	if len(claim.Keywords) >= 4 {
		threshold = 2
	}
	return matches >= threshold
}

// ClaimAcknowledgesConflict returns true when the claim sentence itself hedges
// or cites the contradiction, e.g. "however there is conflicting evidence".
func ClaimAcknowledgesConflict(claim VerifiedClaim) bool {
	return containsAny(claim.Lower,
		"however", "but ", "although", "on the other hand", "conflict",
		"contradict", "inconsistent", "mixed evidence", "unclear",
		"sources disagree", "depending on",
	)
}

// isHighRiskQuery returns true when the verification context contains at least
// one high-risk signal that demands evidence-anchored claims.
func isHighRiskQuery(ctx VerificationContext) bool {
	if ctx.PolicySensitive {
		return true
	}
	if ctx.RequiresFreshness && ctx.HasFreshnessRisk {
		return true
	}
	if ctx.HasContradictions {
		return true
	}
	if ctx.RequiresActionPlan && len(ctx.Subgoals) >= 2 {
		return true
	}
	for _, intent := range ctx.Intents {
		switch strings.ToLower(intent) {
		case "financial_verification", "compliance", "security", "policy":
			return true
		}
	}
	return false
}

// claimEchoesContestedEvidence returns true when a claim repeats content that
// the evidence itself flags as conflicting (multiple evidence items share
// keywords with the claim but also differ in their specifics).
func claimEchoesContestedEvidence(claim VerifiedClaim, evidenceItems []string) bool {
	if len(claim.Keywords) == 0 || len(evidenceItems) < 2 {
		return false
	}
	overlap := 0
	for _, item := range evidenceItems {
		lower := strings.ToLower(item)
		matches := 0
		for _, kw := range claim.Keywords {
			if strings.Contains(lower, kw) {
				matches++
			}
		}
		if matches >= 1 {
			overlap++
		}
	}
	return overlap >= 2
}

// verifyClaimLevel runs the claim-oriented checks and appends issues to result.
// The base VerifyResponse function remains authoritative — this function is a
// pure additive pass so the existing tests keep passing. Every claim observed
// is also written to result.ClaimAudits so traces can surface the full map.
func verifyClaimLevel(content string, ctx VerificationContext, claims []VerifiedClaim, result *VerificationResult) {
	if len(claims) == 0 {
		return
	}

	// Build a baseline audit for every claim so traces carry the full picture,
	// not just the flagged subset. Issue codes are overlaid below when the
	// relevant checks fire.
	auditIndex := make(map[string]int, len(claims))
	for _, claim := range claims {
		hits := claimEvidenceSupport(claim, ctx.EvidenceItems)
		audit := ClaimAudit{
			Sentence:    truncate(claim.Sentence, 200),
			Certainty:   claim.Certainty.String(),
			Supported:   ClaimSupportedByEvidence(claim, ctx.EvidenceItems),
			Anchored:    claim.HasCanonicalAnchor,
			Reconciled:  ClaimAcknowledgesConflict(claim),
			KeywordHits: hits,
		}
		result.ClaimAudits = append(result.ClaimAudits, audit)
		auditIndex[claim.Sentence] = len(result.ClaimAudits) - 1
	}

	highRisk := isHighRiskQuery(ctx)

	// (1) Claim-level contradiction reconciliation.
	if ctx.HasContradictions && len(ctx.EvidenceItems) > 0 {
		var unresolved []string
		responseAcknowledgesConflict := containsAny(strings.ToLower(content),
			"conflict", "contradict", "inconsistent", "mixed evidence",
			"sources disagree", "however", "depending on",
		)
		for _, claim := range claims {
			if claim.Certainty != CertaintyAbsolute {
				continue
			}
			if claim.HasCanonicalAnchor {
				continue
			}
			if ClaimAcknowledgesConflict(claim) {
				continue
			}
			if !claimEchoesContestedEvidence(claim, ctx.EvidenceItems) {
				continue
			}
			if responseAcknowledgesConflict {
				// The block-level check already handled this case.
				continue
			}
			unresolved = append(unresolved, truncate(claim.Sentence, 120))
			if idx, ok := auditIndex[claim.Sentence]; ok {
				result.ClaimAudits[idx].IssueCode = "unresolved_contradicted_claim"
			}
		}
		if len(unresolved) > 0 {
			result.Passed = false
			result.Issues = append(result.Issues, VerificationIssue{
				Code:   "unresolved_contradicted_claim",
				Detail: "the response states absolute claims on topics the evidence treats as contested without reconciling the conflict: " + strings.Join(unresolved, " || "),
			})
		}
	}

	// (2) Provenance coverage accounting for high-risk queries.
	if highRisk && len(ctx.EvidenceItems) > 0 {
		var absolute []VerifiedClaim
		for _, claim := range claims {
			if claim.Certainty == CertaintyAbsolute {
				absolute = append(absolute, claim)
			}
		}
		if len(absolute) >= 2 {
			supported := 0
			for _, claim := range absolute {
				if claim.HasCanonicalAnchor {
					supported++
					continue
				}
				if ClaimSupportedByEvidence(claim, ctx.EvidenceItems) {
					supported++
				}
			}
			coverage := float64(supported) / float64(len(absolute))
			if coverage < 0.5 {
				result.Passed = false
				result.Issues = append(result.Issues, VerificationIssue{
					Code: "weak_provenance_coverage",
					Detail: fmt.Sprintf(
						"only %d/%d absolute claims trace back to retrieved evidence or a canonical anchor on a high-risk query",
						supported, len(absolute),
					),
				})
			}
		}
	}

	// (3) Single absolute claim on a high-risk query with zero support at all.
	if highRisk && len(ctx.EvidenceItems) > 0 {
		var orphaned []string
		for _, claim := range claims {
			if claim.Certainty != CertaintyAbsolute {
				continue
			}
			if claim.HasCanonicalAnchor {
				continue
			}
			if ClaimAcknowledgesConflict(claim) {
				// A claim that itself names the disagreement is not an orphaned
				// assertion — it is a reconciliation.
				continue
			}
			if ClaimSupportedByEvidence(claim, ctx.EvidenceItems) {
				continue
			}
			// Only flag if the claim uses enough substance to be checkable.
			if len(claim.Keywords) < 2 {
				continue
			}
			orphaned = append(orphaned, truncate(claim.Sentence, 120))
			if idx, ok := auditIndex[claim.Sentence]; ok && result.ClaimAudits[idx].IssueCode == "" {
				result.ClaimAudits[idx].IssueCode = "unsupported_absolute_claim"
			}
		}
		if len(orphaned) > 0 {
			// Only add this issue if the broader coverage failure did not already
			// surface the same problem — otherwise we double-report.
			alreadyReported := false
			for _, issue := range result.Issues {
				if issue.Code == "weak_provenance_coverage" {
					alreadyReported = true
					break
				}
			}
			if !alreadyReported {
				result.Passed = false
				result.Issues = append(result.Issues, VerificationIssue{
					Code:   "unsupported_absolute_claim",
					Detail: "absolute claims on a high-risk query have no evidence support and no canonical anchor: " + strings.Join(orphaned, " || "),
				})
			}
		}
	}
}

// VerifierSummary distills a VerificationResult into compact counters suitable
// for trace annotations. It is the backbone of AnnotateVerifierTrace.
type VerifierSummary struct {
	Passed           bool     `json:"passed"`
	IssueCodes       []string `json:"issue_codes,omitempty"`
	ClaimCount       int      `json:"claim_count"`
	AbsoluteCount    int      `json:"absolute_count"`
	SupportedCount   int      `json:"supported_count"`
	AnchoredCount    int      `json:"anchored_count"`
	UnsupportedAbs   int      `json:"unsupported_absolute_count"`
	CoverageRatio    float64  `json:"coverage_ratio"`
	FlaggedClaims    int      `json:"flagged_claims"`
}

// SummarizeVerification computes a compact summary from a VerificationResult.
func SummarizeVerification(result VerificationResult) VerifierSummary {
	summary := VerifierSummary{Passed: result.Passed}
	for _, issue := range result.Issues {
		summary.IssueCodes = append(summary.IssueCodes, issue.Code)
	}
	summary.ClaimCount = len(result.ClaimAudits)
	for _, audit := range result.ClaimAudits {
		if audit.Certainty == CertaintyAbsolute.String() {
			summary.AbsoluteCount++
			if audit.Supported || audit.Anchored {
				summary.SupportedCount++
			} else {
				summary.UnsupportedAbs++
			}
		}
		if audit.Anchored {
			summary.AnchoredCount++
		}
		if audit.IssueCode != "" {
			summary.FlaggedClaims++
		}
	}
	if summary.AbsoluteCount > 0 {
		summary.CoverageRatio = float64(summary.SupportedCount) / float64(summary.AbsoluteCount)
	}
	return summary
}
