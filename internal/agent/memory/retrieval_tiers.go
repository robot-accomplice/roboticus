package memory

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
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

func knowledgeFactSourceLabel(subject, relation, object string) string {
	return subject + " " + relation + " " + object
}

type graphFactRow struct {
	ID         string
	Subject    string
	Relation   string
	Object     string
	Confidence float64
	AgeDays    float64
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

	var evidence []Evidence
	for _, ev := range mr.retrieveKnowledgeFactEvidence(ctx, query, mode, budgetTokens/2) {
		evidence = appendEvidence(evidence, ev)
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

func (mr *Retriever) retrieveKnowledgeFactEvidence(ctx context.Context, query string, mode RetrievalMode, budgetTokens int) []Evidence {
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

	rows, err := mr.store.QueryContext(ctx,
		`SELECT id, subject, relation, object, confidence,
		        julianday('now') - julianday(updated_at) AS age_days
		 FROM knowledge_facts
		 ORDER BY updated_at DESC, confidence DESC LIMIT 200`)
	if err != nil {
		return nil
	}
	defer func() { _ = rows.Close() }()

	var facts []graphFactRow
	for rows.Next() {
		var fact graphFactRow
		if rows.Scan(&fact.ID, &fact.Subject, &fact.Relation, &fact.Object, &fact.Confidence, &fact.AgeDays) != nil {
			continue
		}
		facts = append(facts, fact)
	}

	ordered := rankKnowledgeFactsForQuery(facts, query, mode)
	var evidence []Evidence
	for _, fact := range ordered {
		evidence = appendEvidence(evidence, Evidence{
			Content:        fmt.Sprintf("%s %s %s", fact.Subject, fact.Relation, fact.Object),
			SourceTier:     TierRelationship,
			SourceID:       fact.ID,
			SourceTable:    "knowledge_facts",
			SourceLabel:    knowledgeFactSourceLabel(fact.Subject, fact.Relation, fact.Object),
			SourceCategory: "graph",
			Score:          fact.Confidence,
			AgeDays:        fact.AgeDays,
			AuthorityScore: fact.Confidence,
			RetrievalMode:  mode.String(),
		})
	}
	return evidence
}

func rankKnowledgeFactsForQuery(facts []graphFactRow, query string, mode RetrievalMode) []graphFactRow {
	if len(facts) == 0 {
		return nil
	}

	if query == "" || mode == RetrievalRecency {
		ordered := append([]graphFactRow(nil), facts...)
		sort.SliceStable(ordered, func(i, j int) bool {
			if ordered[i].Confidence == ordered[j].Confidence {
				return ordered[i].AgeDays < ordered[j].AgeDays
			}
			return ordered[i].Confidence > ordered[j].Confidence
		})
		if len(ordered) > 20 {
			ordered = ordered[:20]
		}
		return ordered
	}

	tokens := graphQueryTokens(query)
	if len(tokens) == 0 {
		return rankKnowledgeFactsForQuery(facts, "", mode)
	}

	type scoredFact struct {
		fact         graphFactRow
		seedScore    float64
		connected    bool
		connectScore float64
	}

	scored := make([]scoredFact, 0, len(facts))
	seedEntities := make(map[string]struct{})
	for _, fact := range facts {
		subject := strings.ToLower(fact.Subject)
		relation := strings.ToLower(fact.Relation)
		object := strings.ToLower(fact.Object)

		score := 0.0
		for _, token := range tokens {
			if strings.Contains(subject, token) || strings.Contains(object, token) {
				score += 2.0
			}
			if strings.Contains(relation, token) {
				score += 1.0
			}
		}
		if score > 0 {
			scored = append(scored, scoredFact{fact: fact, seedScore: score})
			seedEntities[strings.ToLower(fact.Subject)] = struct{}{}
			seedEntities[strings.ToLower(fact.Object)] = struct{}{}
		}
	}

	if len(scored) == 0 {
		return rankKnowledgeFactsForQuery(facts, "", mode)
	}

	if mode == RetrievalGraph {
		for _, fact := range facts {
			subject := strings.ToLower(fact.Subject)
			object := strings.ToLower(fact.Object)
			_, subjectHit := seedEntities[subject]
			_, objectHit := seedEntities[object]
			if !subjectHit && !objectHit {
				continue
			}

			alreadySeed := false
			for _, candidate := range scored {
				if candidate.fact.ID == fact.ID {
					alreadySeed = true
					break
				}
			}
			if alreadySeed {
				continue
			}

			connectScore := 1.0
			if subjectHit && objectHit {
				connectScore = 2.0
			}
			scored = append(scored, scoredFact{
				fact:         fact,
				connected:    true,
				connectScore: connectScore,
			})
		}
	}

	sort.SliceStable(scored, func(i, j int) bool {
		left := scored[i]
		right := scored[j]

		leftScore := left.seedScore*10 + left.connectScore*5 + left.fact.Confidence - left.fact.AgeDays/365
		rightScore := right.seedScore*10 + right.connectScore*5 + right.fact.Confidence - right.fact.AgeDays/365
		if leftScore == rightScore {
			return left.fact.ID < right.fact.ID
		}
		return leftScore > rightScore
	})

	ordered := make([]graphFactRow, 0, minInt(len(scored), 20))
	for _, item := range scored {
		ordered = append(ordered, item.fact)
		if len(ordered) == 20 {
			break
		}
	}
	return ordered
}

func graphQueryTokens(query string) []string {
	stopwords := map[string]struct{}{
		"the": {}, "and": {}, "for": {}, "with": {}, "that": {}, "this": {},
		"what": {}, "which": {}, "who": {}, "does": {}, "did": {}, "from": {},
		"have": {}, "has": {}, "into": {}, "than": {}, "when": {}, "where": {},
		"why": {}, "how": {}, "again": {}, "keep": {}, "give": {}, "latest": {},
		"current": {}, "plan": {}, "debug": {}, "issue": {}, "error": {},
	}

	normalized := strings.ToLower(query)
	replacer := strings.NewReplacer("?", " ", ".", " ", ",", " ", ":", " ", ";", " ", "/", " ", "-", " ")
	normalized = replacer.Replace(normalized)
	fields := strings.Fields(normalized)

	seen := make(map[string]struct{}, len(fields))
	tokens := make([]string, 0, len(fields))
	for _, field := range fields {
		if len(field) < 3 {
			continue
		}
		if _, stop := stopwords[field]; stop {
			continue
		}
		if _, exists := seen[field]; exists {
			continue
		}
		seen[field] = struct{}{}
		tokens = append(tokens, field)
	}
	return tokens
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
