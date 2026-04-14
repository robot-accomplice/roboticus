package memory

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// retrieveSemanticMemory fetches from the semantic_memory table.
func (mr *Retriever) retrieveSemanticMemory(ctx context.Context, query string, budgetTokens int) string {
	maxChars := budgetTokens * mr.charsPerToken

	var rows *sql.Rows
	var err error
	if query != "" {
		rows, err = mr.store.QueryContext(ctx,
			`SELECT category, key, value FROM semantic_memory
			 WHERE memory_state = 'active' AND (value LIKE ? OR key LIKE ?)
			 ORDER BY confidence DESC, updated_at DESC LIMIT 20`,
			"%"+query+"%", "%"+query+"%")
	}
	if err != nil || rows == nil {
		rows, err = mr.store.QueryContext(ctx,
			`SELECT category, key, value FROM semantic_memory
			 WHERE memory_state = 'active'
			 ORDER BY confidence DESC, updated_at DESC LIMIT 20`)
	}
	if err != nil {
		return ""
	}
	defer func() { _ = rows.Close() }()

	var b strings.Builder
	used := 0
	for rows.Next() {
		var category, key, value string
		if rows.Scan(&category, &key, &value) != nil {
			continue
		}
		line := fmt.Sprintf("[%s] %s: %s", category, key, value)
		if used+len(line) > maxChars {
			break
		}
		b.WriteString("- ")
		b.WriteString(line)
		b.WriteString("\n")
		used += len(line)
	}
	return b.String()
}

// retrieveProceduralMemory formats tool statistics from procedural_memory
// and learned procedures from learned_skills.
func (mr *Retriever) retrieveProceduralMemory(ctx context.Context, budgetTokens int) string {
	var b strings.Builder

	// Part 1: Tool success/failure stats from procedural_memory.
	rows, err := mr.store.QueryContext(ctx,
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

	// Part 2: Learned procedures from learned_skills (auto-detected tool sequences).
	skillRows, err := mr.store.QueryContext(ctx,
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

	return b.String()
}

// retrieveRelationshipMemory formats relationship data.
func (mr *Retriever) retrieveRelationshipMemory(ctx context.Context, budgetTokens int) string {
	maxChars := budgetTokens * mr.charsPerToken

	rows, err := mr.store.QueryContext(ctx,
		`SELECT entity_name, trust_score, interaction_count, last_interaction
		 FROM relationship_memory
		 ORDER BY interaction_count DESC LIMIT 20`)
	if err != nil {
		return ""
	}
	defer func() { _ = rows.Close() }()

	var b strings.Builder
	used := 0
	for rows.Next() {
		var entityName string
		var trustScore float64
		var interactionCount int
		var lastInteraction *string
		if rows.Scan(&entityName, &trustScore, &interactionCount, &lastInteraction) != nil {
			continue
		}
		line := fmt.Sprintf("%s: trust=%.1f, interactions=%d", entityName, trustScore, interactionCount)
		if lastInteraction != nil {
			line += ", last=" + *lastInteraction
		}
		if used+len(line) > maxChars {
			break
		}
		b.WriteString("- ")
		b.WriteString(line)
		b.WriteString("\n")
		used += len(line)
	}
	return b.String()
}
