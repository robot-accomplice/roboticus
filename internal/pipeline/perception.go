// perception.go builds the unified perception artifact described in
// Milestone 2 of the agentic memory architecture roadmap.
//
// Before this file, perception was spread across three lightly-related
// outputs: TaskSynthesis (intent + complexity + planned action), intent
// classification, and ad-hoc keyword matching for policy/freshness. This
// file consolidates those signals into a single PerceptionArtifact that
// downstream layers (retrieval, verifier, routing) can consume as a single
// decision artifact.
//
// The artifact carries:
//   - intent: the user's goal classification.
//   - risk: how costly a wrong answer is (low / medium / high).
//   - source_of_truth: which memory tier should be treated as authoritative
//     for this query — semantic (facts/policy), procedural (workflows),
//     relationship (graph), episodic (past experience), external (must
//     fetch), or none.
//   - required_memory_tiers: which tiers retrieval MUST consult before
//     answering. Empty when a conversational turn can skip retrieval.
//   - decomposition_needed: whether the query warrants subtask breakdown.
//   - freshness_required: whether the answer depends on current state.
//   - confidence: classifier confidence 0-1.

package pipeline

import (
	"strings"
)

// RiskLevel classifies how consequential a wrong answer is. It matches the
// Appendix A event schema values (low / medium / high).
type RiskLevel string

const (
	RiskLow    RiskLevel = "low"
	RiskMedium RiskLevel = "medium"
	RiskHigh   RiskLevel = "high"
)

// SourceOfTruth identifies which memory tier (or external fetch) should be
// authoritative for this query. Retrieval uses this to weight tier outputs
// during assembly.
type SourceOfTruth string

const (
	SourceSemantic     SourceOfTruth = "semantic"
	SourceProcedural   SourceOfTruth = "procedural"
	SourceRelationship SourceOfTruth = "relationship"
	SourceEpisodic     SourceOfTruth = "episodic"
	SourceExternal     SourceOfTruth = "external"
	SourceNone         SourceOfTruth = "none"
)

// PerceptionArtifact is the unified perception output.
type PerceptionArtifact struct {
	Intent              string
	Risk                RiskLevel
	SourceOfTruth       SourceOfTruth
	RequiredMemoryTiers []string
	DecompositionNeeded bool
	FreshnessRequired   bool
	Confidence          float64
}

// BuildPerception produces a PerceptionArtifact from the raw user content
// plus the already-computed task synthesis. The function is deterministic:
// given identical inputs it returns an identical artifact, so traces and
// tests stay reproducible.
func BuildPerception(content string, synthesis TaskSynthesis) PerceptionArtifact {
	lower := strings.ToLower(content)

	art := PerceptionArtifact{
		Intent:              synthesis.Intent,
		DecompositionNeeded: synthesis.Complexity == "complex" || synthesis.Complexity == "specialist",
		Confidence:          synthesis.Confidence,
	}

	art.Risk = classifyRisk(lower, synthesis)
	art.SourceOfTruth = classifySourceOfTruth(lower, synthesis)
	art.FreshnessRequired = classifyFreshness(lower, synthesis)
	art.RequiredMemoryTiers = classifyRequiredTiers(art, lower)

	return art
}

func classifyRisk(lower string, synthesis TaskSynthesis) RiskLevel {
	// High-risk markers: financial, compliance, security, irreversible actions.
	highMarkers := []string{
		"financial", "payment", "payout", "refund", "chargeback",
		"compliance", "kyc", "aml", "gdpr", "hipaa", "sox",
		"security", "credential", "secret", "password",
		"production", "prod ", "deploy to prod", "rollback",
		"delete", "drop table", "irrevers", "destroy",
	}
	for _, marker := range highMarkers {
		if strings.Contains(lower, marker) {
			return RiskHigh
		}
	}

	// Medium-risk markers: changes to state, operations on real data.
	mediumMarkers := []string{
		"migrate", "update", "modify", "change", "alter",
		"deploy", "release", "rollout", "ship",
		"scheduling", "send", "email", "message",
		"policy", "rule", "procedure",
	}
	for _, marker := range mediumMarkers {
		if strings.Contains(lower, marker) {
			return RiskMedium
		}
	}

	// Planned-action kind signals medium risk when planner is delegating.
	if synthesis.PlannedAction == "delegate_to_specialist" || synthesis.PlannedAction == "compose_subagent" {
		return RiskMedium
	}

	return RiskLow
}

func classifySourceOfTruth(lower string, synthesis TaskSynthesis) SourceOfTruth {
	// External / current-state queries first: these carry strong freshness
	// cues ("current", "price of", "real-time") that outrank semantic
	// lookups even when the sentence also contains semantic markers like
	// "what is".
	externalMarkers := []string{
		"current ", "latest ", "real-time", "right now",
		"what's happening", "price of", "status of",
	}
	for _, marker := range externalMarkers {
		if strings.Contains(lower, marker) {
			return SourceExternal
		}
	}

	// Workflow / procedure / runbook queries: procedural memory.
	proceduralMarkers := []string{
		"how do i", "how to", "steps to", "procedure", "runbook",
		"playbook", "workflow", "process for",
	}
	for _, marker := range proceduralMarkers {
		if strings.Contains(lower, marker) {
			return SourceProcedural
		}
	}

	// Dependency / impact / graph queries: relationship memory.
	relationshipMarkers := []string{
		"depends on", "dependency", "depends", "impact",
		"impacted by", "blast radius", "affected by",
		"owner of", "owned by", "who owns",
	}
	for _, marker := range relationshipMarkers {
		if strings.Contains(lower, marker) {
			return SourceRelationship
		}
	}

	// Policy / compliance / spec queries: semantic memory owns the truth.
	semanticMarkers := []string{
		"policy", "policies", "compliance", "spec ", "specification",
		"documentation", "docs", "rule", "rules", "regulation",
		"definition", "what is", "what does", "explain",
	}
	for _, marker := range semanticMarkers {
		if strings.Contains(lower, marker) {
			return SourceSemantic
		}
	}

	// Past experience queries: episodic memory.
	episodicMarkers := []string{
		"did we", "have we", "last time", "previously",
		"in the past", "we tried", "before",
	}
	for _, marker := range episodicMarkers {
		if strings.Contains(lower, marker) {
			return SourceEpisodic
		}
	}

	// No retrieval needed for short conversational turns.
	if !synthesis.RetrievalNeeded {
		return SourceNone
	}
	return SourceEpisodic
}

func classifyFreshness(lower string, synthesis TaskSynthesis) bool {
	if verificationRequiresFreshness(lower, synthesis.Intent) {
		return true
	}
	return false
}

// classifyRequiredTiers decides which memory tiers retrieval MUST consult
// based on the source-of-truth and risk. Callers can still consult
// additional tiers opportunistically — this is the minimum required set.
func classifyRequiredTiers(art PerceptionArtifact, lower string) []string {
	if art.SourceOfTruth == SourceNone {
		return nil
	}
	seen := make(map[string]struct{})
	var tiers []string
	add := func(tier string) {
		if _, ok := seen[tier]; ok {
			return
		}
		seen[tier] = struct{}{}
		tiers = append(tiers, tier)
	}

	switch art.SourceOfTruth {
	case SourceSemantic:
		add("semantic")
	case SourceProcedural:
		add("procedural")
	case SourceRelationship:
		add("relationship")
	case SourceEpisodic:
		add("episodic")
	case SourceExternal:
		// External queries still benefit from semantic / episodic context.
		add("semantic")
		add("episodic")
	}

	// High-risk queries always consult semantic (for canonical policy) and
	// relationship (for dependency awareness).
	if art.Risk == RiskHigh {
		add("semantic")
		add("relationship")
	}

	// Decomposition work benefits from procedural retrieval so the agent can
	// reuse an existing workflow.
	if art.DecompositionNeeded {
		add("procedural")
	}

	return tiers
}
