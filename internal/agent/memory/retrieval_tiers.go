package memory

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"roboticus/internal/db"
)

func semanticAuthority(category string, confidence float64) (bool, float64) {
	lower := strings.ToLower(category)
	canonical := strings.Contains(lower, "policy") ||
		strings.Contains(lower, "architecture") ||
		strings.Contains(lower, "procedure") ||
		strings.Contains(lower, "canonical")

	score := confidence
	if score < 0 {
		score = 0
	}
	if score > 1 {
		score = 1
	}
	if canonical && score < 0.85 {
		score = 0.85
	}
	return canonical, score
}

func semanticSourceLabel(category, key string) string {
	if category == "" {
		return key
	}
	if key == "" {
		return category
	}
	return category + "/" + key
}

func relationshipSourceLabel(entityName, entityID string) string {
	if entityName == "" {
		return entityID
	}
	if entityID == "" {
		return entityName
	}
	return entityName + "/" + entityID
}

// retrieveSemanticEvidence fetches semantic memory with richer provenance preserved.
func (mr *Retriever) retrieveSemanticEvidence(ctx context.Context, query string, queryEmbed []float32, mode RetrievalMode, budgetTokens int) []Evidence {
	maxChars := budgetTokens * mr.charsPerToken
	used := 0
	appendEvidence := func(dst []Evidence, ev Evidence) []Evidence {
		if ev.Content == "" {
			return dst
		}
		if used+len(ev.Content) > maxChars {
			return dst
		}
		used += len(ev.Content)
		return append(dst, ev)
	}

	var evidence []Evidence
	seen := make(map[string]struct{})

	switch mode {
	case RetrievalHybrid, RetrievalSemantic, RetrievalANN:
		if query != "" {
			weight := mr.config.HybridWeight
			if weight <= 0 {
				weight = AdaptiveHybridWeight(mr.estimateCorpusSize(ctx))
			}
			if mode == RetrievalSemantic || mode == RetrievalANN {
				weight = 1.0
			}
			results := db.HybridSearch(ctx, mr.store, query, queryEmbed, 20, weight, mr.vectorIndex)
			for _, hr := range results {
				if hr.SourceTable != "semantic_memory" {
					continue
				}
				var (
					id         string
					category   string
					key        string
					value      string
					confidence float64
					ageDays    float64
				)
				err := mr.store.QueryRowContext(ctx,
					`SELECT id, category, key, value, confidence,
					        julianday('now') - julianday(updated_at)
					   FROM semantic_memory
					  WHERE id = ? AND memory_state = 'active'`,
					hr.SourceID).Scan(&id, &category, &key, &value, &confidence, &ageDays)
				if err != nil {
					continue
				}
				seen[id] = struct{}{}
				isCanonical, authority := semanticAuthority(category, confidence)
				evidence = appendEvidence(evidence, Evidence{
					Content:        fmt.Sprintf("[%s] %s: %s", category, key, value),
					SourceTier:     TierSemantic,
					SourceID:       id,
					SourceTable:    "semantic_memory",
					SourceLabel:    semanticSourceLabel(category, key),
					SourceCategory: category,
					Score:          hr.Similarity,
					FTSScore:       hr.FTSScore,
					VecScore:       hr.VectorScore,
					AgeDays:        ageDays,
					IsCanonical:    isCanonical,
					AuthorityScore: authority,
					RetrievalMode:  mode.String(),
				})
			}
			if len(evidence) > 0 {
				return evidence
			}
		}
	}

	var rows *sql.Rows
	var err error
	if query != "" {
		rows, err = mr.store.QueryContext(ctx,
			`SELECT id, category, key, value, confidence,
			        julianday('now') - julianday(updated_at) AS age_days
			   FROM semantic_memory
			  WHERE memory_state = 'active' AND (value LIKE ? OR key LIKE ?)
			  ORDER BY confidence DESC, updated_at DESC LIMIT 20`,
			"%"+query+"%", "%"+query+"%")
	}
	if err != nil || rows == nil {
		rows, err = mr.store.QueryContext(ctx,
			`SELECT id, category, key, value, confidence,
			        julianday('now') - julianday(updated_at) AS age_days
			   FROM semantic_memory
			  WHERE memory_state = 'active'
			  ORDER BY confidence DESC, updated_at DESC LIMIT 20`)
	}
	if err != nil {
		return nil
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var (
			id         string
			category   string
			key        string
			value      string
			confidence float64
			ageDays    float64
		)
		if rows.Scan(&id, &category, &key, &value, &confidence, &ageDays) != nil {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		isCanonical, authority := semanticAuthority(category, confidence)
		evidence = appendEvidence(evidence, Evidence{
			Content:        fmt.Sprintf("[%s] %s: %s", category, key, value),
			SourceTier:     TierSemantic,
			SourceID:       id,
			SourceTable:    "semantic_memory",
			SourceLabel:    semanticSourceLabel(category, key),
			SourceCategory: category,
			Score:          confidence,
			AgeDays:        ageDays,
			IsCanonical:    isCanonical,
			AuthorityScore: authority,
			RetrievalMode:  mode.String(),
		})
	}
	return evidence
}

// retrieveSemanticMemory fetches from the semantic_memory table.
func (mr *Retriever) retrieveSemanticMemory(ctx context.Context, query string, queryEmbed []float32, mode RetrievalMode, budgetTokens int) string {
	var b strings.Builder
	for _, ev := range mr.retrieveSemanticEvidence(ctx, query, queryEmbed, mode, budgetTokens) {
		b.WriteString("- ")
		b.WriteString(ev.Content)
		b.WriteString("\n")
	}
	return b.String()
}

// retrieveProceduralMemory formats tool statistics from procedural_memory
// and learned procedures from learned_skills.
func (mr *Retriever) retrieveProceduralMemory(ctx context.Context, query string, mode RetrievalMode, budgetTokens int) string {
	var b strings.Builder

	filtered := query != "" && mode != RetrievalRecency

	// Part 1: Tool success/failure stats from procedural_memory.
	var rows *sql.Rows
	var err error
	if filtered {
		like := "%" + query + "%"
		rows, err = mr.store.QueryContext(ctx,
			`SELECT name, success_count, failure_count FROM procedural_memory
			 WHERE name LIKE ? OR steps LIKE ? OR preconditions LIKE ? OR error_modes LIKE ?
			 ORDER BY (success_count + failure_count) DESC LIMIT 15`,
			like, like, like, like)
	} else {
		rows, err = mr.store.QueryContext(ctx,
			`SELECT name, success_count, failure_count FROM procedural_memory
			 ORDER BY (success_count + failure_count) DESC LIMIT 15`)
	}
	if err == nil {
		emitted := 0
		for rows.Next() {
			var name string
			var successCount, failureCount int
			if rows.Scan(&name, &successCount, &failureCount) != nil {
				continue
			}
			total := successCount + failureCount
			if total == 0 {
				continue
			}
			pct := float64(successCount) / float64(total) * 100
			fmt.Fprintf(&b, "- %s: %d/%d (%.0f%% success)\n", name, successCount, total, pct)
			emitted++
		}
		_ = rows.Close()
		if filtered && emitted == 0 {
			rows, err = mr.store.QueryContext(ctx,
				`SELECT name, success_count, failure_count FROM procedural_memory
				 ORDER BY (success_count + failure_count) DESC LIMIT 15`)
			if err == nil {
				for rows.Next() {
					var name string
					var successCount, failureCount int
					if rows.Scan(&name, &successCount, &failureCount) != nil {
						continue
					}
					total := successCount + failureCount
					if total == 0 {
						continue
					}
					pct := float64(successCount) / float64(total) * 100
					fmt.Fprintf(&b, "- %s: %d/%d (%.0f%% success)\n", name, successCount, total, pct)
				}
				_ = rows.Close()
			}
		}
	}

	// Part 2: Learned procedures from learned_skills (auto-detected tool sequences).
	var skillRows *sql.Rows
	if filtered {
		like := "%" + query + "%"
		skillRows, err = mr.store.QueryContext(ctx,
			`SELECT name, success_count, priority FROM learned_skills
			 WHERE memory_state = 'active' AND success_count >= 2
			   AND (name LIKE ? OR description LIKE ? OR steps_json LIKE ?)
			 ORDER BY priority DESC, success_count DESC LIMIT 5`,
			like, like, like)
	} else {
		skillRows, err = mr.store.QueryContext(ctx,
			`SELECT name, success_count, priority FROM learned_skills
			 WHERE memory_state = 'active' AND success_count >= 2
			 ORDER BY priority DESC, success_count DESC LIMIT 5`)
	}
	if err == nil {
		emitted := 0
		for skillRows.Next() {
			var name string
			var successCount, priority int
			if skillRows.Scan(&name, &successCount, &priority) != nil {
				continue
			}
			fmt.Fprintf(&b, "- [learned] %s: %d runs, priority=%d\n", name, successCount, priority)
			emitted++
		}
		_ = skillRows.Close()
		if filtered && emitted == 0 {
			skillRows, err = mr.store.QueryContext(ctx,
				`SELECT name, success_count, priority FROM learned_skills
				 WHERE memory_state = 'active' AND success_count >= 2
				 ORDER BY priority DESC, success_count DESC LIMIT 5`)
			if err == nil {
				for skillRows.Next() {
					var name string
					var successCount, priority int
					if skillRows.Scan(&name, &successCount, &priority) != nil {
						continue
					}
					fmt.Fprintf(&b, "- [learned] %s: %d runs, priority=%d\n", name, successCount, priority)
				}
				_ = skillRows.Close()
			}
		}
	}

	return b.String()
}

// retrieveRelationshipMemory formats relationship data.
func (mr *Retriever) retrieveRelationshipMemory(ctx context.Context, query string, mode RetrievalMode, budgetTokens int) string {
	var b strings.Builder
	for _, ev := range mr.retrieveRelationshipEvidence(ctx, query, mode, budgetTokens) {
		b.WriteString("- ")
		b.WriteString(ev.Content)
		b.WriteString("\n")
	}
	return b.String()
}

func (mr *Retriever) retrieveRelationshipEvidence(ctx context.Context, query string, mode RetrievalMode, budgetTokens int) []Evidence {
	maxChars := budgetTokens * mr.charsPerToken
	used := 0
	appendEvidence := func(dst []Evidence, ev Evidence) []Evidence {
		if ev.Content == "" {
			return dst
		}
		if used+len(ev.Content) > maxChars {
			return dst
		}
		used += len(ev.Content)
		return append(dst, ev)
	}

	var rows *sql.Rows
	var err error
	if query != "" && mode != RetrievalRecency {
		like := "%" + query + "%"
		rows, err = mr.store.QueryContext(ctx,
			`SELECT id, entity_id, entity_name, trust_score, interaction_summary, interaction_count, last_interaction,
			        julianday('now') - julianday(COALESCE(updated_at, created_at)) AS age_days
			 FROM relationship_memory
			 WHERE entity_name LIKE ? OR interaction_summary LIKE ?
			 ORDER BY interaction_count DESC, trust_score DESC, COALESCE(updated_at, created_at) DESC LIMIT 20`,
			like, like)
	} else {
		rows, err = mr.store.QueryContext(ctx,
			`SELECT id, entity_id, entity_name, trust_score, interaction_summary, interaction_count, last_interaction,
			        julianday('now') - julianday(COALESCE(updated_at, created_at)) AS age_days
			 FROM relationship_memory
			 ORDER BY interaction_count DESC, COALESCE(updated_at, created_at) DESC LIMIT 20`)
	}
	if err != nil {
		return nil
	}
	defer func() { _ = rows.Close() }()

	var evidence []Evidence
	emitted := 0
	for rows.Next() {
		var id, entityID, entityName string
		var trustScore float64
		var interactionSummary sql.NullString
		var interactionCount int
		var lastInteraction *string
		var ageDays float64
		if rows.Scan(&id, &entityID, &entityName, &trustScore, &interactionSummary, &interactionCount, &lastInteraction, &ageDays) != nil {
			continue
		}
		line := fmt.Sprintf("%s: trust=%.1f, interactions=%d", entityName, trustScore, interactionCount)
		if interactionSummary.Valid && interactionSummary.String != "" {
			line += ", relation=" + interactionSummary.String
		}
		if lastInteraction != nil {
			line += ", last=" + *lastInteraction
		}
		evidence = appendEvidence(evidence, Evidence{
			Content:        line,
			SourceTier:     TierRelationship,
			SourceID:       id,
			SourceTable:    "relationship_memory",
			SourceLabel:    relationshipSourceLabel(entityName, entityID),
			SourceCategory: "relationship",
			Score:          trustScore,
			AgeDays:        ageDays,
			AuthorityScore: trustScore,
			RetrievalMode:  mode.String(),
		})
		emitted++
	}
	if query != "" && mode != RetrievalRecency && emitted == 0 {
		rows, err = mr.store.QueryContext(ctx,
			`SELECT id, entity_id, entity_name, trust_score, interaction_summary, interaction_count, last_interaction,
			        julianday('now') - julianday(COALESCE(updated_at, created_at)) AS age_days
			 FROM relationship_memory
			 ORDER BY interaction_count DESC, COALESCE(updated_at, created_at) DESC LIMIT 20`)
		if err != nil {
			return evidence
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var id, entityID, entityName string
			var trustScore float64
			var interactionSummary sql.NullString
			var interactionCount int
			var lastInteraction *string
			var ageDays float64
			if rows.Scan(&id, &entityID, &entityName, &trustScore, &interactionSummary, &interactionCount, &lastInteraction, &ageDays) != nil {
				continue
			}
			line := fmt.Sprintf("%s: trust=%.1f, interactions=%d", entityName, trustScore, interactionCount)
			if interactionSummary.Valid && interactionSummary.String != "" {
				line += ", relation=" + interactionSummary.String
			}
			if lastInteraction != nil {
				line += ", last=" + *lastInteraction
			}
			evidence = appendEvidence(evidence, Evidence{
				Content:        line,
				SourceTier:     TierRelationship,
				SourceID:       id,
				SourceTable:    "relationship_memory",
				SourceLabel:    relationshipSourceLabel(entityName, entityID),
				SourceCategory: "relationship",
				Score:          trustScore,
				AgeDays:        ageDays,
				AuthorityScore: trustScore,
				RetrievalMode:  mode.String(),
			})
		}
	}
	return evidence
}
