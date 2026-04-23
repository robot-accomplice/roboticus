// context_assembly.go implements structured context assembly (Layer 12).
//
// Transforms raw retrieval results into structured evidence that the
// reasoning engine can work with. Separates working state (active,
// not searched) from retrieved evidence (searched, ranked, filtered).
//
// Output structure:
//   [Working State]    ← direct injection, not searched
//   [Retrieved Evidence] ← ranked by relevance with source/score
//   [Freshness Risks]  ← stale evidence / recency caveats
//   [Gaps]             ← what's missing, prevents confabulation
//   [Contradictions]   ← conflicting evidence, surfaces uncertainty

package memory

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/rs/zerolog/log"

	"roboticus/internal/db"
	"roboticus/internal/session"
)

// AssembledContext is the structured output of the context assembler.
//
// The text fields are the rendered markdown-ish sections that get
// formatted into the prompt (see Format()). The EvidenceArtifact
// field carries a typed, format-independent view of the same data
// for downstream pipeline stages (verifier, policy guards) that want
// to reason over structured data instead of `strings.Contains`'ing
// their way through the rendered output. See v1.0.6 audit finding
// P2-C and internal/session/verification_evidence.go for the
// rationale.
type AssembledContext struct {
	WorkingState   string // direct-injected active state
	Evidence       string // ranked retrieval results with provenance
	FreshnessRisks string // stale evidence or recency caveats
	Gaps           string // detected information gaps
	Contradictions string // conflicting evidence

	// EvidenceArtifact is the typed sibling of the rendered text
	// sections above. Populated alongside them by AssembleContext.
	// Nil means "no assembly happened yet" — callers can still
	// format the text fields but there's nothing for the verifier
	// to consume.
	EvidenceArtifact *session.VerificationEvidence
}

// Format produces the final text block for prompt injection.
func (ac *AssembledContext) Format() string {
	var sections []string

	if ac.WorkingState != "" {
		sections = append(sections, "[Working State]\n"+ac.WorkingState)
	}
	if ac.Evidence != "" {
		sections = append(sections, "[Retrieved Evidence]\n"+ac.Evidence)
	}
	if ac.FreshnessRisks != "" {
		sections = append(sections, "[Freshness Risks]\n"+ac.FreshnessRisks)
	}
	if ac.Gaps != "" {
		sections = append(sections, "[Gaps]\n"+ac.Gaps)
	}
	if ac.Contradictions != "" {
		sections = append(sections, "[Contradictions]\n"+ac.Contradictions)
	}

	if len(sections) == 0 {
		return ""
	}
	return "[Active Memory]\n\n" + strings.Join(sections, "\n\n")
}

// AssembleContext builds structured context from working memory + ranked
// evidence. Executive state (plan, assumptions, unresolved questions, verified
// conclusions, decision checkpoints, stopping criteria) is fetched from the
// working_memory store and surfaced at the top of the [Working State] section.
func AssembleContext(
	ctx context.Context,
	store *db.Store,
	sessionID string,
	evidence []Evidence,
	workingMemory string,
	ambientRecent string,
) *AssembledContext {
	ac := &AssembledContext{}

	// Load executive state ONCE and reuse it for both the rendered
	// working-state block AND the typed verification artifact.
	// Pre-v1.0.6-self-audit, these two surfaces each called
	// LoadExecutiveState independently — same DB query, run twice
	// per retrieval. The single load here is the P2-I dedup fix.
	execState := loadExecutiveState(ctx, store, sessionID)

	// Working state: direct injection (plan, assumptions, recent activity).
	var workingParts []string
	if executive := renderExecutiveStateBlock(execState); executive != "" {
		workingParts = append(workingParts, executive)
	}
	if workingMemory != "" {
		workingParts = append(workingParts, workingMemory)
	}
	if ambientRecent != "" {
		workingParts = append(workingParts, ambientRecent)
	}
	ac.WorkingState = strings.Join(workingParts, "\n")

	// Evidence: ranked with provenance labels.
	if len(evidence) > 0 {
		var b strings.Builder
		for i, e := range evidence {
			tier := e.SourceTier.String()
			var qualifiers []string
			if e.IsCanonical {
				qualifiers = append(qualifiers, "canonical")
			}
			if e.AuthorityScore > 0 {
				qualifiers = append(qualifiers, fmt.Sprintf("authority=%.2f", e.AuthorityScore))
			}
			if e.SourceLabel != "" {
				qualifiers = append(qualifiers, "source="+e.SourceLabel)
			}
			if e.RetrievalMode != "" {
				qualifiers = append(qualifiers, "via="+e.RetrievalMode)
			}
			if e.AgeDays >= 1 {
				qualifiers = append(qualifiers, fmt.Sprintf("age=%.0fd", e.AgeDays))
			}

			meta := fmt.Sprintf("%s, %.2f", tier, e.Score)
			if len(qualifiers) > 0 {
				meta += ", " + strings.Join(qualifiers, ", ")
			}
			fmt.Fprintf(&b, "%d. [%s] %s\n", i+1, meta, e.Content)
		}
		ac.Evidence = b.String()
	}

	// Gaps: detect which tiers returned no results.
	ac.Gaps = detectGaps(evidence)

	// Freshness: explicitly call out stale supporting evidence.
	ac.FreshnessRisks = detectFreshnessRisks(evidence)

	// Contradictions: detect conflicting evidence.
	contradictionArtifact := detectStructuredContradictions(evidence)
	ac.Contradictions = renderContradictions(contradictionArtifact)

	gapCount := strings.Count(ac.Gaps, "\n")
	freshnessCount := strings.Count(ac.FreshnessRisks, "\n")
	contradictionCount := strings.Count(ac.Contradictions, "\n")
	log.Debug().
		Int("evidence", len(evidence)).
		Int("gaps", gapCount).
		Int("freshness_risks", freshnessCount).
		Int("contradictions", contradictionCount).
		Msg("context assembly: structured context built")

	// Typed evidence artifact: derived from the same assembly state as
	// the rendered text, so the verifier can read structured fields
	// instead of parsing the rendered output (see v1.0.6 P2-C).
	// execState was already loaded above — pass it through instead of
	// re-querying (v1.0.6 self-audit P2-I).
	ac.EvidenceArtifact = buildVerificationEvidenceFromAssembly(ac, evidence, contradictionArtifact, execState)

	return ac
}

// buildVerificationEvidenceFromAssembly extracts the typed verification
// artifact from an already-assembled context plus a pre-loaded executive
// state. Callers are expected to have loaded executive state once via
// loadExecutiveState and shared it — this function does zero DB I/O.
//
// Pre-P2-I dedup: this function used to re-query LoadExecutiveState
// itself, duplicating the call that loadExecutiveStateBlock already
// made. Every retrieval turn paid for the same query twice.
func buildVerificationEvidenceFromAssembly(
	ac *AssembledContext,
	evidence []Evidence,
	contradictions []session.ContradictionEvidence,
	execState *ExecutiveState,
) *session.VerificationEvidence {
	ve := &session.VerificationEvidence{
		HasEvidence:       strings.TrimSpace(ac.Evidence) != "",
		HasGaps:           strings.TrimSpace(ac.Gaps) != "",
		HasFreshnessRisks: strings.TrimSpace(ac.FreshnessRisks) != "",
		HasContradictions: strings.TrimSpace(ac.Contradictions) != "",
		Contradictions:    append([]session.ContradictionEvidence(nil), contradictions...),
	}

	// Canonical evidence: any single evidence row with the canonical
	// qualifier is enough. This replaces pre-v1.0.6
	// `strings.Contains(mem, "canonical")` in the verifier which
	// would false-positive on anyone saying the word "canonical".
	for _, e := range evidence {
		if e.IsCanonical {
			ve.HasCanonicalEvidence = true
			break
		}
	}

	// Evidence items: flatten each evidence row into a short bullet
	// string with its provenance tag intact. The verifier uses this to
	// count distinct sources and detect thin-evidence answers.
	for _, e := range evidence {
		tier := e.SourceTier.String()
		ve.EvidenceItems = append(ve.EvidenceItems,
			fmt.Sprintf("[%s, %.2f] %s", tier, e.Score, truncateForArtifact(e.Content, 200)))
	}

	// Executive state: passed in from AssembleContext's single load.
	if execState != nil {
		for _, e := range execState.UnresolvedQuestions {
			ve.UnresolvedQuestions = append(ve.UnresolvedQuestions, e.Content)
		}
		for _, e := range execState.VerifiedConclusions {
			ve.VerifiedConclusions = append(ve.VerifiedConclusions, e.Content)
		}
		for _, e := range execState.StoppingCriteria {
			ve.StoppingCriteria = append(ve.StoppingCriteria, e.Content)
		}
	}

	return ve
}

// truncateForArtifact trims a content string for inclusion in the
// typed evidence artifact. Full content lives in the evidence-source
// rows already; we just need enough for downstream pattern matching.
func truncateForArtifact(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}

// loadExecutiveState reads the latest executive state for a session. Split
// from rendering (renderExecutiveStateBlock) so AssembleContext can share
// a single load between the rendered working-state block AND the typed
// verification artifact (v1.0.6 P2-I dedup). Returns nil when there's
// nothing to render — callers must nil-check before accessing fields.
func loadExecutiveState(ctx context.Context, store *db.Store, sessionID string) *ExecutiveState {
	if store == nil || sessionID == "" {
		return nil
	}
	// Use a minimal manager shim so executive loading does not require a fully
	// configured Manager instance. This keeps AssembleContext usable from the
	// tests and from code paths that construct contexts without a full agent.
	shim := &Manager{store: store}
	state, err := shim.LoadExecutiveState(ctx, sessionID, "")
	if err != nil {
		log.Debug().Err(err).Msg("context assembly: executive state load failed")
		return nil
	}
	if state == nil || state.IsEmpty() {
		return nil
	}
	return state
}

// renderExecutiveStateBlock formats an already-loaded executive state as
// the "Executive State:\n..." prefix block for the working-state
// section. Pure rendering — zero I/O — so it can be called after
// loadExecutiveState without paying for another DB query.
func renderExecutiveStateBlock(state *ExecutiveState) string {
	if state == nil {
		return ""
	}
	block := state.FormatForContext()
	if block == "" {
		return ""
	}
	return "Executive State:\n" + block
}

// detectGaps identifies which memory tiers were queried but returned no results.
func detectGaps(evidence []Evidence) string {
	if len(evidence) == 0 {
		return "- No evidence retrieved from any tier"
	}

	tiersPresent := make(map[MemoryTier]bool)
	for _, e := range evidence {
		tiersPresent[e.SourceTier] = true
	}

	var gaps []string
	expectedTiers := []struct {
		tier MemoryTier
		desc string
	}{
		{TierEpisodic, "No past experiences found for this query"},
		{TierSemantic, "No factual/policy knowledge found for this query"},
		{TierProcedural, "No relevant procedures or workflows found"},
		{TierRelationship, "No relationship/entity data found"},
	}

	for _, et := range expectedTiers {
		if !tiersPresent[et.tier] {
			gaps = append(gaps, "- "+et.desc)
		}
	}

	if len(gaps) == 0 {
		return ""
	}
	return strings.Join(gaps, "\n")
}

func detectFreshnessRisks(evidence []Evidence) string {
	if len(evidence) == 0 {
		return ""
	}

	staleByTier := make(map[MemoryTier]float64)
	for _, e := range evidence {
		if e.AgeDays < 30 {
			continue
		}
		if current, ok := staleByTier[e.SourceTier]; !ok || e.AgeDays > current {
			staleByTier[e.SourceTier] = e.AgeDays
		}
	}

	if len(staleByTier) == 0 {
		return ""
	}

	var risks []string
	for tier, ageDays := range staleByTier {
		risks = append(risks, fmt.Sprintf("- %s evidence may be stale (oldest supporting item is %.0f days old)", tier, ageDays))
	}
	return strings.Join(risks, "\n")
}

// detectStructuredContradictions finds contradiction signals that the verifier
// can reason about later. It prefers explicit pair conflicts (same topic,
// incompatible discriminator values) and falls back to the older score-spread
// heuristic only when there is no stronger semantic signal for that tier.
func detectStructuredContradictions(evidence []Evidence) []session.ContradictionEvidence {
	if len(evidence) < 2 {
		return nil
	}

	var contradictions []session.ContradictionEvidence
	seen := make(map[string]struct{})

	for i := 0; i < len(evidence); i++ {
		for j := i + 1; j < len(evidence); j++ {
			shared := contradictionSharedKeywords(evidence[i].Content, evidence[j].Content)
			if len(shared) < 2 {
				continue
			}
			if !contradictionHasConflictingDiscriminator(evidence[i].Content, evidence[j].Content) {
				continue
			}
			topic := strings.Join(shared[:min(3, len(shared))], ", ")
			key := "value_conflict|" + topic
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			contradictions = append(contradictions, session.ContradictionEvidence{
				Kind:           "value_conflict",
				Topic:          topic,
				Summary:        fmt.Sprintf("%s evidence disagrees across retrieved items", topic),
				SharedKeywords: append([]string(nil), shared...),
				EvidenceItems: []string{
					truncateForArtifact(evidence[i].Content, 160),
					truncateForArtifact(evidence[j].Content, 160),
				},
			})
		}
	}

	byTier := make(map[MemoryTier][]Evidence)
	for _, e := range evidence {
		byTier[e.SourceTier] = append(byTier[e.SourceTier], e)
	}
	for tier, entries := range byTier {
		if len(entries) < 3 {
			continue
		}
		maxScore := entries[0].Score
		minScore := entries[0].Score
		for _, e := range entries[1:] {
			if e.Score > maxScore {
				maxScore = e.Score
			}
			if e.Score < minScore {
				minScore = e.Score
			}
		}
		spread := maxScore - minScore
		if spread <= 0.5 {
			continue
		}
		key := "score_spread|" + tier.String()
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		items := make([]string, 0, min(3, len(entries)))
		for _, e := range entries[:min(3, len(entries))] {
			items = append(items, truncateForArtifact(e.Content, 160))
		}
		contradictions = append(contradictions, session.ContradictionEvidence{
			Kind:          "score_spread",
			Topic:         tier.String(),
			Summary:       fmt.Sprintf("%s tier: high score spread (%.2f) — evidence may be inconsistent", tier, spread),
			EvidenceItems: items,
		})
	}

	return contradictions
}

func renderContradictions(contradictions []session.ContradictionEvidence) string {
	if len(contradictions) == 0 {
		return ""
	}
	lines := make([]string, 0, len(contradictions))
	for _, contradiction := range contradictions {
		summary := strings.TrimSpace(contradiction.Summary)
		if summary == "" {
			continue
		}
		lines = append(lines, "- "+summary)
	}
	return strings.Join(lines, "\n")
}

func contradictionSharedKeywords(left, right string) []string {
	leftSet := contradictionKeywordSet(left)
	rightSet := contradictionKeywordSet(right)
	var shared []string
	for kw := range leftSet {
		if _, ok := rightSet[kw]; ok {
			shared = append(shared, kw)
		}
	}
	sort.Strings(shared)
	return shared
}

func contradictionKeywordSet(text string) map[string]struct{} {
	stop := map[string]struct{}{
		"the": {}, "and": {}, "for": {}, "with": {}, "that": {}, "this": {},
		"from": {}, "into": {}, "what": {}, "when": {}, "where": {}, "which": {},
		"why": {}, "how": {}, "version": {}, "specified": {}, "current": {},
		"policy": {}, "source": {}, "evidence": {},
	}
	out := make(map[string]struct{})
	for _, token := range strings.Fields(strings.ToLower(text)) {
		token = strings.Trim(token, ".,:;!?()[]{}\"'")
		if len(token) < 4 {
			continue
		}
		if _, skip := stop[token]; skip {
			continue
		}
		out[token] = struct{}{}
	}
	return out
}

func contradictionHasConflictingDiscriminator(left, right string) bool {
	leftTokens := contradictionDiscriminatorTokens(left)
	rightTokens := contradictionDiscriminatorTokens(right)
	if len(leftTokens) == 0 || len(rightTokens) == 0 {
		return false
	}
	if strings.Join(leftTokens, "|") == strings.Join(rightTokens, "|") {
		return false
	}
	return true
}

func contradictionDiscriminatorTokens(text string) []string {
	var out []string
	for _, token := range strings.Fields(strings.ToLower(text)) {
		token = strings.Trim(token, ".,:;!?()[]{}\"'")
		if token == "" {
			continue
		}
		if contradictionLooksNumeric(token) || contradictionLooksVersion(token) {
			out = append(out, token)
		}
	}
	return out
}

func contradictionLooksNumeric(token string) bool {
	if token == "" {
		return false
	}
	for _, r := range token {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func contradictionLooksVersion(token string) bool {
	if strings.HasPrefix(token, "v") && len(token) > 1 && contradictionLooksNumeric(token[1:]) {
		return true
	}
	return strings.Contains(token, "version")
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
