// decomposer.go implements query decomposition (Layer 2 of the agentic architecture).
//
// Complex queries often overload a single embedding — "What did we decide about
// the auth refactor and how does it affect the deployment timeline?" needs two
// separate retrieval passes. The decomposer splits compound queries into
// independent subgoals, each routed to the most appropriate memory tier.
//
// v1.0.5: heuristic decomposition (conjunction splitting + tier classification).
// v1.1.0+: LLM-based decomposition for semantic understanding.

package memory

import (
	"strings"

	"github.com/rs/zerolog/log"
)

// Subgoal represents a single information need extracted from a compound query.
type Subgoal struct {
	Question   string     // the specific information need
	TargetTier MemoryTier // best store for this question
}

// Decompose splits a compound query into independent subgoals.
// Simple queries return a single subgoal unchanged.
// Compound queries (containing conjunctions) are split and each part
// is classified to a target memory tier.
func Decompose(query string) []Subgoal {
	if query == "" {
		return nil
	}

	// Only decompose if the query shows compound structure.
	parts := splitCompound(query)
	if len(parts) <= 1 {
		return []Subgoal{{Question: query, TargetTier: TierSemantic}}
	}

	subgoals := make([]Subgoal, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		tier := classifySubgoalTier(part)
		subgoals = append(subgoals, Subgoal{Question: part, TargetTier: tier})
	}

	if len(query) <= 80 {
		log.Debug().Int("subgoals", len(subgoals)).Str("query", query).
			Msg("decomposer: compound query split into subgoals")
	} else {
		log.Debug().Int("subgoals", len(subgoals)).Str("query", query[:80]+"...").
			Msg("decomposer: compound query split into subgoals")
	}

	if len(subgoals) == 0 {
		return []Subgoal{{Question: query, TargetTier: TierSemantic}}
	}
	return subgoals
}

// splitCompound breaks a query at conjunction points.
func splitCompound(query string) []string {
	// First try splitting on sentence-final question marks (multiple questions).
	if strings.Count(query, "?") >= 2 {
		questions := strings.Split(query, "?")
		var nonEmpty []string
		for _, q := range questions {
			q = strings.TrimSpace(q)
			if len(q) > 10 {
				nonEmpty = append(nonEmpty, q+"?")
			}
		}
		if len(nonEmpty) >= 2 {
			return nonEmpty
		}
	}

	// Try splitting on semicolons.
	if strings.Contains(query, ";") {
		parts := strings.Split(query, ";")
		if len(parts) >= 2 {
			var nonEmpty []string
			for _, p := range parts {
				p = strings.TrimSpace(p)
				if len(p) > 10 {
					nonEmpty = append(nonEmpty, p)
				}
			}
			if len(nonEmpty) >= 2 {
				return nonEmpty
			}
		}
	}

	// Try splitting on conjunctions — but only if the query is long enough.
	if len(query) < 60 {
		return []string{query}
	}

	lower := strings.ToLower(query)
	conjunctions := []string{" and also ", " and how ", " and what ", " and why ",
		" and when ", " and who ", " as well as ", " plus "}
	for _, conj := range conjunctions {
		if idx := strings.Index(lower, conj); idx > 10 {
			return []string{
				strings.TrimSpace(query[:idx]),
				strings.TrimSpace(query[idx+len(conj):]),
			}
		}
	}

	return []string{query}
}

// classifySubgoalTier determines the best memory tier for a subgoal.
func classifySubgoalTier(question string) MemoryTier {
	lower := strings.ToLower(question)

	if containsAny(lower, "when did", "what happened", "last time", "previously",
		"history", "before", "remember", "decide") {
		return TierEpisodic
	}
	if containsAny(lower, "how to", "how do", "steps", "process", "procedure",
		"workflow", "playbook") {
		return TierProcedural
	}
	if containsAny(lower, "who is", "who are", "who ", "relationship", "trust",
		"responsible", "owner") {
		return TierRelationship
	}

	return TierSemantic
}
