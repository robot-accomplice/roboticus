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
	"strings"

	"github.com/rs/zerolog/log"

	"roboticus/internal/db"
)

// AssembledContext is the structured output of the context assembler.
type AssembledContext struct {
	WorkingState   string // direct-injected active state
	Evidence       string // ranked retrieval results with provenance
	FreshnessRisks string // stale evidence or recency caveats
	Gaps           string // detected information gaps
	Contradictions string // conflicting evidence
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

// AssembleContext builds structured context from working memory + ranked evidence.
func AssembleContext(
	ctx context.Context,
	store *db.Store,
	sessionID string,
	evidence []Evidence,
	workingMemory string,
	ambientRecent string,
) *AssembledContext {
	ac := &AssembledContext{}

	// Working state: direct injection (goals, assumptions, recent activity).
	var workingParts []string
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
	ac.Contradictions = detectContradictions(evidence)

	gapCount := strings.Count(ac.Gaps, "\n")
	freshnessCount := strings.Count(ac.FreshnessRisks, "\n")
	contradictionCount := strings.Count(ac.Contradictions, "\n")
	log.Debug().
		Int("evidence", len(evidence)).
		Int("gaps", gapCount).
		Int("freshness_risks", freshnessCount).
		Int("contradictions", contradictionCount).
		Msg("context assembly: structured context built")

	return ac
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

// detectContradictions finds evidence pairs that might conflict.
// v1.0.5: simple heuristic — flags entries from the same tier with
// very different scores (one highly relevant, one barely relevant).
// v1.1.0+: LLM-based semantic contradiction detection.
func detectContradictions(evidence []Evidence) string {
	if len(evidence) < 2 {
		return ""
	}

	// Group by tier.
	byTier := make(map[MemoryTier][]Evidence)
	for _, e := range evidence {
		byTier[e.SourceTier] = append(byTier[e.SourceTier], e)
	}

	var contradictions []string
	for tier, entries := range byTier {
		if len(entries) < 2 {
			continue
		}
		// If the score spread within a tier is very high, the entries
		// might be in tension (one strongly matches, one barely matches).
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
		if spread > 0.5 && len(entries) >= 3 {
			contradictions = append(contradictions,
				fmt.Sprintf("- %s tier: high score spread (%.2f) — evidence may be inconsistent", tier, spread))
		}
	}

	if len(contradictions) == 0 {
		return ""
	}
	return strings.Join(contradictions, "\n")
}
